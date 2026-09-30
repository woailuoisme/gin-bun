package middleware

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"bgin/internal/platform/errs"
)

// Timeout 向请求上下文注入超时截止期，超时由下层 I/O 响应并经 ErrorHandler 统一渲染为 504 错误。
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
			m.logger.Debug("request timed out after partial response",
				zap.String("path", c.Request.URL.Path),
				zap.String("method", c.Request.Method),
			)
		}
	}
}
