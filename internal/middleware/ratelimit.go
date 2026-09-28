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

// Tier 定义限流档位。
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

// RateLimit 执行固定窗口限流，超限返回 429 及限流响应头。
func (m *Middleware) RateLimit(tier Tier) gin.HandlerFunc {
	limit := m.limitFor(tier)

	return func(c *gin.Context) {
		if limit <= 0 {
			c.Next()
			return
		}

		now := time.Now().UTC()
		windowSeconds := int64(rateLimitWindow.Seconds())
		// 按分钟计算窗口分桶及重置时间戳。
		bucket := now.Unix() / windowSeconds
		resetAt := (bucket + 1) * windowSeconds

		key := cache.Key("ratelimit", string(tier), m.scopeKey(tier, c), strconv.FormatInt(bucket, 10))

		count, err := m.cache.Increment(c.Request.Context(), key, 2*rateLimitWindow)
		if err != nil {
			// 计数器故障时降级放行并记录错误。
			logger.FromContext(c.Request.Context()).Error().Err(err).Msg("rate limit counter failed, request allowed")
			c.Next()
			return
		}

		c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
		c.Header("X-RateLimit-Remaining", strconv.FormatInt(max(int64(limit)-count, 0), 10))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))

		if count > int64(limit) {
			c.Header("Retry-After", strconv.FormatInt(max(resetAt-now.Unix(), 1), 10))
			abortWithError(c, errs.RateLimited("Too many requests"))
			return
		}

		c.Next()
	}
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

// scopeKey 确定限流维度：认证用户按 UserID，其余按客户端 IP。
func (m *Middleware) scopeKey(tier Tier, c *gin.Context) string {
	if tier == TierAuthenticated {
		if id, ok := identity.FromContext(c.Request.Context()); ok {
			return "user:" + id.UserID
		}
	}
	return "ip:" + c.ClientIP()
}
