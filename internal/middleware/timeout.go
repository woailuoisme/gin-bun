package middleware

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"

	"bgin/internal/platform/errs"
)

// Timeout 给每个请求注入基于 Context 的截止期。
//
// 区别于第三方通过独立 goroutine 与计时器竞争的浅层并发包装（极易引发对未加锁 gin.Context 的数据竞态），
// 本中间件直接向 c.Request.Context() 注入 WithTimeout。下游所有涉 I/O 操作（Bun/pgx、Valkey、S3）
// 均遵从 Context 并在超时时主动取消退出，由后置 ErrorHandler 捕获 context.DeadlineExceeded
// 并统一渲染为 RFC 9110 规定的 504 Gateway Timeout 统一结构化错误响应。
func (m *Middleware) Timeout() gin.HandlerFunc {
	return func(c *gin.Context) {
		if m.requestTimeout <= 0 {
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), m.requestTimeout)
		defer cancel()

		c.Request = c.Request.WithContext(ctx)
		c.Next()

		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return
		}
		if !c.Writer.Written() {
			_ = c.Error(errs.GatewayTimeout(context.DeadlineExceeded))
			return
		}
		if m.logger != nil {
			m.logger.Debug().
				Str("path", c.Request.URL.Path).
				Str("method", c.Request.Method).
				Msg("request timed out after partial response")
		}
	}
}
