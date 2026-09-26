package middleware

import (
	"bytes"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// maxBodyBytes 是请求体大小上限。API 的入参都是短 JSON（图片以 URL 字符串传递），
// 1 MiB 的余量已经很宽。没有这道限制时，解析器会照单把任意大的 body 读进内存，
// 几十个并发就能把容器打满。
const maxBodyBytes = 1 << 20

// CacheBody 预先读取并将请求体安全缓存至上下文（gin.BodyBytesKey），
// 使得后续安全中间件（如验签/审计）与下游处理器能进行多道并发/重复反序列化，规避流单次排空 EOF 问题。
func (m *Middleware) CacheBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body == nil {
			c.Next()
			return
		}

		if _, ok := c.Get(gin.BodyBytesKey); ok {
			c.Next()
			return
		}

		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}

		c.Set(gin.BodyBytesKey, bodyBytes)
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		c.Next()
	}
}

// BodyLimit 给请求体套上硬上限；超限由绑定层翻译成 413（见 validator.decodeError）。
//
// 这里用 http.MaxBytesReader 而不是在解析前读长度：它拦在读的路上，
// 不必先把整个 body 收进内存才能发现它太大。
func (m *Middleware) BodyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
		}
		c.Next()
	}
}

// SecurityHeaders 补齐 API 响应缺失的安全响应头。
//
// 刻意不下发 Content-Security-Policy：/docs 上的 Scalar UI 需要内联脚本与样式，
// 一份 default-src 'none' 的策略会直接把它打坏，而 JSON 响应本身没有脚本执行面。
// 文档页将来若迁到独立源，再单独为它定策略。
func (m *Middleware) SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.Writer.Header()
		// 禁止浏览器按内容嗅探 MIME 类型，避免把 JSON 当脚本执行。
		header.Set("X-Content-Type-Options", "nosniff")
		// 禁止被任何来源嵌入框架，防点击劫持。
		header.Set("X-Frame-Options", "DENY")
		// 不把完整 URL（可能含查询参数）带给第三方。
		header.Set("Referrer-Policy", "no-referrer")
		// HSTS 只在生产下发：本地开发走 http，浏览器一旦记住它，后续 http 请求会被升级成
		// https 而连不上。它是唯一一个"下发错了就无法靠改服务端撤销"的响应头。
		if m.production {
			header.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}
