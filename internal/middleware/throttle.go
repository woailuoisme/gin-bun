package middleware

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"bgin/internal/platform/authentication"
	"bgin/internal/platform/cache"
	"bgin/internal/platform/errs"
	"bgin/internal/platform/logger"
)

// Tier 定义传统全局限流预设档位。
type Tier string

const (
	// TierAnonymous 面向未认证客户端，按客户端 IP 计数。
	TierAnonymous Tier = "anonymous"
	// TierAuthenticated 面向已认证用户，按用户 ID 计数。
	TierAuthenticated Tier = "authenticated"
	// TierSensitive 面向敏感认证端点，按客户端 IP 计数且配额最严。
	TierSensitive Tier = "sensitive"

	rateLimitWindow = time.Minute
)

// Limit 定义限流配额规则与自定义维度（对应 Laravel 的 Limit 对象）。
type Limit struct {
	MaxAttempts int
	Decay       time.Duration
	Key         string
	response    func(c *gin.Context)
}

// PerMinute 构造按每分钟为周期的限流配额（对应 Laravel 的 Limit::perMinute(N)）。
func PerMinute(maxAttempts int) Limit {
	return Limit{
		MaxAttempts: maxAttempts,
		Decay:       time.Minute,
	}
}

// PerMinutes 构造按指定分钟数为周期的限流配额（对应 Laravel 的 Limit::perMinutes(M, N)）。
func PerMinutes(minutes int, maxAttempts int) Limit {
	return Limit{
		MaxAttempts: maxAttempts,
		Decay:       time.Duration(minutes) * time.Minute,
	}
}

// PerHour 构造按每小时为周期的限流配额（对应 Laravel 的 Limit::perHour(N)）。
func PerHour(maxAttempts int) Limit {
	return Limit{
		MaxAttempts: maxAttempts,
		Decay:       time.Hour,
	}
}

// PerDay 构造按每日为周期的限流配额（对应 Laravel 的 Limit::perDay(N)）。
func PerDay(maxAttempts int) Limit {
	return Limit{
		MaxAttempts: maxAttempts,
		Decay:       24 * time.Hour,
	}
}

// None 表示不作任何限流限制（对应 Laravel 的 Limit::none()）。
func None() Limit {
	return Limit{MaxAttempts: 0}
}

// By 指定自定义限流分区维度（例如 IP、用户 ID 或组合标识）。
func (l Limit) By(key string) Limit {
	l.Key = key
	return l
}

// Response 指定自定义超限响应处理函数。
func (l Limit) Response(fn func(c *gin.Context)) Limit {
	l.response = fn
	return l
}

// LimiterResolver 定义命名限流器的动态解析函数。
type LimiterResolver func(c *gin.Context) Limit

// RegisterLimiter 注册命名限流器（对应 Laravel 的 RateLimiter::for(...)）。
func (m *Middleware) RegisterLimiter(name string, resolver LimiterResolver) {
	if m == nil || resolver == nil {
		return
	}
	m.limitersMu.Lock()
	defer m.limitersMu.Unlock()
	if m.limiters == nil {
		m.limiters = make(map[string]LimiterResolver)
	}
	m.limiters[name] = resolver
}

func (m *Middleware) getLimiter(name string) (LimiterResolver, bool) {
	if m == nil {
		return nil, false
	}
	m.limitersMu.RLock()
	defer m.limitersMu.RUnlock()
	if m.limiters == nil {
		return nil, false
	}
	resolver, ok := m.limiters[name]
	return resolver, ok
}

// ThrottleOption 定义自定义限流选项。
type ThrottleOption func(*throttleOptions)

type throttleOptions struct {
	keyFunc  func(c *gin.Context) string
	tier     Tier
	response func(c *gin.Context)
}

// WithThrottleKey 自定义限流区分维度（对应 Laravel 的 ->by(...) 机制）。
func WithThrottleKey(fn func(c *gin.Context) string) ThrottleOption {
	return func(o *throttleOptions) {
		o.keyFunc = fn
	}
}

// WithThrottleTier 指定所属业务档位（用于监控与日志分类）。
func WithThrottleTier(tier Tier) ThrottleOption {
	return func(o *throttleOptions) {
		o.tier = tier
	}
}

// WithThrottleResponse 指定自定义限流触发时的响应拦截回调。
func WithThrottleResponse(fn func(c *gin.Context)) ThrottleOption {
	return func(o *throttleOptions) {
		o.response = fn
	}
}

// RateLimiterStore 定义限流中间件所需的缓存原语接口。
// platform/cache.Client 结构体天然完整实现该接口。
type RateLimiterStore interface {
	Increment(ctx context.Context, key string, ttl time.Duration) (int64, error)
	Get(ctx context.Context, key string) (string, error)
}

// Throttle 【路由/路由组中间件】仿照 Laravel throttle 中间件设计，执行平滑滑动窗口限流（Sliding Window Counter）。
//
// 支持多种灵活调用形态：
// 1. 命名限流器：mw.Throttle("login") 或 mw.Throttle("api")（需通过 mw.RegisterLimiter 注册）
// 2. 字符串表达式：mw.Throttle("60,1")（每 1 分钟 60 次）或 mw.Throttle("100,5")（每 5 分钟 100 次）
// 3. 预设业务档位：mw.Throttle(middleware.TierSensitive) 或 mw.Throttle(middleware.TierAuthenticated)
// 4. 显式数值窗口：mw.Throttle(60, time.Minute, opts...)
func (m *Middleware) Throttle(args ...any) gin.HandlerFunc {
	if len(args) == 0 {
		return m.ThrottleLimit(m.limitFor(TierAnonymous), rateLimitWindow, WithThrottleTier(TierAnonymous))
	}

	opts := extractThrottleOptions(args[1:])

	switch first := args[0].(type) {
	case string:
		// 1. 优先判断是否为形如 "60,1" 的表达式
		if limit, window, ok := parseThrottleExpr(first); ok {
			return m.ThrottleLimit(limit, window, opts...)
		}

		// 2. 检查是否为已注册的命名限流器 (RateLimiter::for)
		if resolver, ok := m.getLimiter(first); ok {
			return m.throttleNamed(first, resolver)
		}

		// 3. 检查是否为 Tier 预设档位字符串
		tier := Tier(first)
		switch tier {
		case TierAnonymous, TierAuthenticated, TierSensitive:
			return m.ThrottleLimit(m.limitFor(tier), rateLimitWindow, append([]ThrottleOption{WithThrottleTier(tier)}, opts...)...)
		}

		// 未知名称兜底使用默认配置并记录告警
		if m.logger != nil {
			m.logger.Warn("unknown throttle limiter name, falling back to anonymous tier", zap.String("name", first))
		}
		return m.ThrottleLimit(m.limitFor(TierAnonymous), rateLimitWindow, WithThrottleTier(TierAnonymous))

	case Tier:
		return m.ThrottleLimit(m.limitFor(first), rateLimitWindow, append([]ThrottleOption{WithThrottleTier(first)}, opts...)...)

	case int:
		limit := first
		window := rateLimitWindow
		if len(args) > 1 {
			if w, ok := args[1].(time.Duration); ok {
				window = w
			}
		}
		return m.ThrottleLimit(limit, window, opts...)
	}

	return m.ThrottleLimit(m.limitFor(TierAnonymous), rateLimitWindow, WithThrottleTier(TierAnonymous))
}

// extractThrottleOptions 从变长参数中提取 ThrottleOption 选项切片。
func extractThrottleOptions(args []any) []ThrottleOption {
	var opts []ThrottleOption
	for _, arg := range args {
		if opt, ok := arg.(ThrottleOption); ok {
			opts = append(opts, opt)
		}
	}
	return opts
}

func (m *Middleware) resolveLimiterStore() RateLimiterStore {
	if m == nil {
		return nil
	}
	if m.limiterStore != nil {
		return m.limiterStore
	}
	if m.cache != nil {
		return m.cache
	}
	return nil
}

// ThrottleLimit 显式强类型方法，执行指定配额与时间周期的滑动窗口限流。
func (m *Middleware) ThrottleLimit(limit int, window time.Duration, opts ...ThrottleOption) gin.HandlerFunc {
	return ThrottleWithStore(m.resolveLimiterStore(), limit, window, opts...)
}

func (m *Middleware) throttleNamed(name string, resolver LimiterResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		limitObj := resolver(c)
		if limitObj.MaxAttempts <= 0 {
			// Limit::none() bypass
			c.Next()
			return
		}

		opts := []ThrottleOption{
			WithThrottleTier(Tier(name)),
		}
		if limitObj.Key != "" {
			opts = append(opts, WithThrottleKey(func(_ *gin.Context) string {
				return limitObj.Key
			}))
		}
		if limitObj.response != nil {
			opts = append(opts, WithThrottleResponse(limitObj.response))
		}

		handler := ThrottleWithStore(m.resolveLimiterStore(), limitObj.MaxAttempts, limitObj.Decay, opts...)
		handler(c)
	}
}

// ThrottleWithStore 构造独立的平滑滑动窗口限流中间件实例，方便测试与自定义注入。
func ThrottleWithStore(store RateLimiterStore, limit int, window time.Duration, opts ...ThrottleOption) gin.HandlerFunc {
	var options throttleOptions
	for _, opt := range opts {
		opt(&options)
	}
	if window <= 0 {
		window = rateLimitWindow
	}

	return func(c *gin.Context) {
		if limit <= 0 || store == nil {
			c.Next()
			return
		}

		now := time.Now().UTC()
		windowSeconds := max(int64(window.Seconds()), 1)
		bucket := now.Unix() / windowSeconds
		offset := now.Unix() % windowSeconds
		resetAt := (bucket + 1) * windowSeconds

		var scope string
		switch {
		case options.keyFunc != nil:
			scope = options.keyFunc(c)
		case options.tier != "":
			scope = scopeKey(options.tier, c)
		default:
			scope = defaultScopeKey(c)
		}

		tierName := string(options.tier)
		if tierName == "" {
			tierName = "custom"
		}

		key := cache.Key("ratelimit", tierName, scope, strconv.FormatInt(windowSeconds, 10), strconv.FormatInt(bucket, 10))
		currCount, err := store.Increment(c.Request.Context(), key, 2*window)
		if err != nil {
			// 计数器故障时降级放行并记录错误。
			logger.FromContext(c.Request.Context()).Error("throttle counter failed, request allowed", zap.Error(err))
			c.Next()
			return
		}

		// 平滑滑动窗口算法 (Sliding Window Counter)：结合前一窗口的加权余量，平滑消除窗口临界双倍突发流量。
		prevKey := cache.Key("ratelimit", tierName, scope, strconv.FormatInt(windowSeconds, 10), strconv.FormatInt(bucket-1, 10))
		var prevCount int64
		if val, getErr := store.Get(c.Request.Context(), prevKey); getErr == nil {
			prevCount, _ = strconv.ParseInt(val, 10, 64)
		}

		weight := float64(windowSeconds-offset) / float64(windowSeconds)
		estimatedCount := currCount + int64(float64(prevCount)*weight)

		if estimatedCount > int64(limit) {
			c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))
			c.Header("Retry-After", strconv.FormatInt(max(resetAt-now.Unix(), 1), 10))

			logger.FromContext(c.Request.Context()).Warn(
				"rate limit exceeded",
				zap.String("tier", tierName),
				zap.String("scope", scope),
				zap.Int("limit", limit),
				zap.Int64("window_seconds", windowSeconds),
				zap.Int64("current_count", currCount),
				zap.Int64("estimated_count", estimatedCount),
				zap.String("path", c.Request.URL.Path),
				zap.String("method", c.Request.Method),
				zap.String("ip", c.ClientIP()),
			)

			if options.response != nil {
				options.response(c)
				c.Abort()
				return
			}

			abortWithError(c, errs.RateLimited("Too many requests"))
			return
		}

		c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
		c.Header("X-RateLimit-Remaining", strconv.FormatInt(max(int64(limit)-estimatedCount, 0), 10))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))

		c.Next()
	}
}

// parseThrottleExpr 解析类似 Laravel 的 "60,1" 表达式为 limit 和 decay duration。
func parseThrottleExpr(expr string) (int, time.Duration, bool) {
	parts := strings.Split(expr, ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	limit, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || limit <= 0 {
		return 0, 0, false
	}

	decayStr := strings.TrimSpace(parts[1])
	// 尝试优先按照标准 time.Duration 解析 (如 "1m", "10s", "2h")
	if d, dErr := time.ParseDuration(decayStr); dErr == nil && d > 0 {
		return limit, d, true
	}

	// 否则若为纯数字，按照 Laravel 规范默认视为分钟数
	minutes, mErr := strconv.Atoi(decayStr)
	if mErr != nil || minutes <= 0 {
		return 0, 0, false
	}
	return limit, time.Duration(minutes) * time.Minute, true
}

// limitFor 按档位读取配额，未知档位回退至未认证配额。
func (m *Middleware) limitFor(tier Tier) int {
	switch tier {
	case TierAuthenticated:
		return m.limiter.Authenticated
	case TierSensitive:
		return m.limiter.Sensitive
	default:
		return m.limiter.Anonymous
	}
}

// defaultScopeKey 默认限流维度：优先使用已认证用户 ID，未认证时回退为客户端 IP。
func defaultScopeKey(c *gin.Context) string {
	if id, ok := authentication.FromContext(c.Request.Context()); ok && id.UserID != "" {
		return "user:" + id.UserID
	}
	return "ip:" + c.ClientIP()
}

// scopeKey 确定特定档位的限流维度：TierAuthenticated 优先使用 UserID，其余均使用客户端 IP。
func scopeKey(tier Tier, c *gin.Context) string {
	if tier == TierAuthenticated {
		return defaultScopeKey(c)
	}
	return "ip:" + c.ClientIP()
}
