package middleware

import (
	"encoding/hex"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/samber/lo"

	"bgin/internal/platform/errs"
	"bgin/internal/platform/logger"
)

// headerRequestID 是全链路追踪请求头。
const headerRequestID = "x-request-id"

// requestIDPrefix 是追踪标识前缀。
const requestIDPrefix = "req_"

// probePaths 探针路径集合。
var probePaths = map[string]struct{}{
	"/livez":  {},
	"/readyz": {},
}

// RequestID 生成或透传全链路追踪请求标识。
func (m *Middleware) RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader(headerRequestID))
		if requestID == "" {
			requestID = newRequestID()
		}

		c.Writer.Header().Set(headerRequestID, requestID)
		c.Request = c.Request.WithContext(logger.WithContext(c.Request.Context(), m.logger, requestID))
		c.Next()
	}
}

// newRequestID 生成形如 req_<uuidv7> 的追踪标识。
func newRequestID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return requestIDPrefix + strings.ReplaceAll(uuid.NewString(), "-", "")
	}

	buf := make([]byte, 0, len(requestIDPrefix)+hex.EncodedLen(len(id)))
	buf = append(buf, requestIDPrefix...)
	return string(hex.AppendEncode(buf, id[:]))
}

// AccessLog 记录每个请求的方法、路径、状态码与耗时。
func (m *Middleware) AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		log := logger.FromContext(c.Request.Context())
		event := log.Info()
		switch {
		case c.Writer.Status() >= 500:
			event = log.Error()
		case c.Writer.Status() >= 400:
			event = log.Warn()
		case isProbePath(c.FullPath()):
			event = log.Debug()
		}

		if errMsg := formatErrors(c.Errors); errMsg != "" {
			event = event.Str("error", errMsg)
		}
		if code, ok := c.Get("error_code"); ok {
			if codeStr, ok := code.(string); ok && codeStr != "" {
				event = event.Str("code", codeStr)
			}
		}

		event.
			Str("method", c.Request.Method).
			Str("path", c.Request.URL.Path).
			Int("status", c.Writer.Status()).
			Str("latency", time.Since(start).String()).
			Str("ip", c.ClientIP()).
			Msg("access")
	}
}

// formatErrors 提取并拼接上下文错误文本，去除内部格式前缀与换行。
func formatErrors(errs []*gin.Error) string {
	if len(errs) == 0 {
		return ""
	}
	if len(errs) == 1 {
		return errs[0].Err.Error()
	}
	msgs := lo.Map(errs, func(e *gin.Error, _ int) string {
		return e.Err.Error()
	})
	return strings.Join(msgs, "; ")
}

// isProbePath 判断请求是否为探针路由。
func isProbePath(fullPath string) bool {
	_, ok := probePaths[fullPath]
	return ok
}

// Recovery 捕获 panic 转为统一 500 响应，堆栈仅进入日志。
func (m *Middleware) Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}

			logger.FromContext(c.Request.Context()).Error().
				Any("panic", recovered).
				Str("stack", string(debug.Stack())).
				Msg("request panic recovered")

			if c.Writer.Written() {
				c.Abort()
				return
			}
			_ = c.Error(errs.Internal(nil))
			c.Abort()
		}()

		c.Next()
	}
}
