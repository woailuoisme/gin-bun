package middleware

import (
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

// allowedRequestHeaders 是预检时允许客户端携带的请求头。
const allowedRequestHeaders = "Authorization, Content-Type, Accept, x-request-id, Idempotency-Key"

// exposedResponseHeaders 是允许浏览器脚本读取的响应头。
const exposedResponseHeaders = "x-request-id, X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset, Retry-After"

// CORS 处理跨域预检与响应头。允许来源含 * 时放开全部来源，但按规范不得同时允许携带凭证。
func (m *Middleware) CORS() gin.HandlerFunc {
	allowAll := slices.Contains(m.cors.AllowedOrigins, "*")
	allowed := make(map[string]struct{}, len(m.cors.AllowedOrigins))
	for _, origin := range m.cors.AllowedOrigins {
		if origin != "*" {
			allowed[strings.ToLower(origin)] = struct{}{}
		}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}

		_, listed := allowed[strings.ToLower(origin)]
		switch {
		case listed:
			// 白名单命中：回显具体来源，这样可以同时允许携带凭证。
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			// 同一 URL 的响应随 Origin 而变，必须声明，否则会被共享缓存串用。
			// 用 Add 而非 Set：上游中间件可能已写入其他 Vary 维度，覆盖会丢掉它们。
			c.Writer.Header().Add("Vary", "Origin")
		case allowAll:
			// 通配放开：只能回 *，且规范禁止它与 Allow-Credentials 同时出现。
			// 回显任意来源 + 允许凭证，等于让任何站点都能带凭证跨域读响应（CWE-942）：
			// 当前鉴权是纯 Bearer 尚且收得住，一旦改用 Cookie 会话就是账号接管。
			c.Header("Access-Control-Allow-Origin", "*")
		}

		if listed || allowAll {
			c.Header("Access-Control-Allow-Headers", allowedRequestHeaders)
			c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Expose-Headers", exposedResponseHeaders)
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
