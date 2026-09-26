package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"bgin/internal/platform/cache"
	"bgin/internal/platform/errs"
	"bgin/internal/platform/identity"
	"bgin/internal/platform/logger"
)

// Tier 是限流档位，对应 api.md 7 描述的三级配额。
type Tier string

// 三个限流档位。
const (
	// TierAnonymous 面向未认证客户端，按 IP 计数。
	TierAnonymous Tier = "anonymous"
	// TierAuthenticated 面向已认证用户，按用户计数。
	TierAuthenticated Tier = "authenticated"
	// TierSensitive 面向敏感认证端点，按 IP 计数且配额最严。
	TierSensitive Tier = "sensitive"
)

// rateLimitWindow 是所有档位共用的固定窗口长度。
const rateLimitWindow = time.Minute

// RateLimit 执行固定窗口限流，超限返回 429 与 api.md 7 约定的配额响应头。
func (m *Middleware) RateLimit(tier Tier) gin.HandlerFunc {
	limit := m.limitFor(tier)

	return func(c *gin.Context) {
		if limit <= 0 {
			c.Next()
			return
		}

		now := time.Now().UTC()
		windowSeconds := int64(rateLimitWindow.Seconds())
		// 把窗口编号编进键名，使统计天然按分钟隔离，无需再读一次 TTL。
		bucket := now.Unix() / windowSeconds
		resetAt := (bucket + 1) * windowSeconds

		key := cache.Key("ratelimit", string(tier), m.scopeKey(tier, c), strconv.FormatInt(bucket, 10))

		count, err := m.cache.Increment(c.Request.Context(), key, 2*rateLimitWindow)
		if err != nil {
			// 计数器不可用时放行并告警：宁可少拦，也不能让全站不可用。
			logger.FromContext(c.Request.Context()).Error().Err(err).Msg("限流计数失败，本次请求放行")
			c.Next()
			return
		}

		c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
		c.Header("X-RateLimit-Remaining", strconv.FormatInt(max(int64(limit)-count, 0), 10))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))

		if count > int64(limit) {
			c.Header("Retry-After", strconv.FormatInt(max(resetAt-now.Unix(), 1), 10))
			_ = c.Error(errs.RateLimited("Too many requests"))
			c.Abort()
			return
		}

		c.Next()
	}
}

// limitFor 按档位取配额，未知档位退回未认证配额。
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

// scopeKey 决定计数维度：已认证按用户，其余按客户端 IP。
func (m *Middleware) scopeKey(tier Tier, c *gin.Context) string {
	if tier == TierAuthenticated {
		if id, ok := identity.FromContext(c.Request.Context()); ok {
			return "user:" + id.UserID
		}
	}
	return "ip:" + c.ClientIP()
}
