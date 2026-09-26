package middleware

import (
	"encoding/hex"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"bgin/internal/platform/errs"
	"bgin/internal/platform/logger"
)

// headerRequestID 是 api.md 3.2 约定的全链路追踪请求头。
const headerRequestID = "x-request-id"

// RequestID 生成或透传 x-request-id，把日志器与请求标识绑定后写入请求 context。
func (m *Middleware) RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader(headerRequestID))
		if requestID == "" {
			requestID = newRequestID()
		}

		// 响应头必须回写，便于调用方在报障时提供。
		c.Writer.Header().Set(headerRequestID, requestID)
		c.Request = c.Request.WithContext(logger.WithContext(c.Request.Context(), m.logger, requestID))
		c.Next()
	}
}

// requestIDPrefix 是追踪标识的前缀，与 api.md 3.2 的示例一致。
const requestIDPrefix = "req_"

// newRequestID 生成形如 req_<uuidv7> 的追踪标识。
func newRequestID() string {
	// 一次分配：uuid.NewString() 会先生成 36 字节带连字符的串、再去掉连字符、再拼接，
	// 三次分配只为得到一个标识。这里按最终长度开缓冲一次成型。
	id, err := uuid.NewV7()
	if err != nil {
		// V7 依赖时间源与随机源；退回与旧实现相同的形态即可，标识本身不需要有序。
		return requestIDPrefix + strings.ReplaceAll(uuid.NewString(), "-", "")
	}

	buf := make([]byte, 0, len(requestIDPrefix)+hex.EncodedLen(len(id)))
	buf = append(buf, requestIDPrefix...)
	return string(hex.AppendEncode(buf, id[:]))
}

// probePaths 是编排系统的探针路径：它们每几秒就会被调用一次。
var probePaths = map[string]struct{}{
	"/healthz": {},
	"/readyz":  {},
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
			// 探针降为 Debug：它们仍要留痕，但不该把真实请求淹没在 Info 里。
			event = log.Debug()
		}
		if len(c.Errors) > 0 {
			event = event.Str("error", c.Errors.String())
		}
		if code, ok := c.Get("error_code"); ok {
			if codeStr, ok := code.(string); ok {
				event = event.Str("code", codeStr)
			}
		}
		event.
			Str("method", c.Request.Method).
			Str("path", c.Request.URL.Path).
			Int("status", c.Writer.Status()).
			Str("latency", time.Since(start).String()).
			Str("ip", c.ClientIP()).
			Msg("HTTP 访问日志")
	}
}

// isProbePath 判断请求是否落在探针路由上。用路由模板而非原始路径，
// 这样带参数的路由不会被误判。
func isProbePath(fullPath string) bool {
	_, ok := probePaths[fullPath]
	return ok
}

// Recovery 捕获 panic 转为统一的 500 响应，堆栈只进日志不进响应体。
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
				Msg("请求处理发生 panic")

			// 已经写出响应的请求改不了状态码了，此时再处理会二次写响应头
			// （gin 报 superfluous WriteHeader），客户端拿到的是被截断的 body。
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
