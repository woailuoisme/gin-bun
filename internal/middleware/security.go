package middleware

import (
	"bytes"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// maxBodyBytes 请求体大小上限（1 MiB）。
const maxBodyBytes = 1 << 20

// CacheBody 将请求体读取并缓存至上下文（gin.BodyBytesKey），避免下游多道读取流 EOF。
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
			abortWithError(c, err)
			return
		}

		c.Set(gin.BodyBytesKey, bodyBytes)
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		c.Next()
	}
}

// BodyLimit 限制请求体大小上限，防止超大请求耗尽内存。
func (m *Middleware) BodyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
		}
		c.Next()
	}
}

// SecurityHeaders 注入常用安全响应头（防 MIME 嗅探、点击劫持与隐私泄露）。
func (m *Middleware) SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.Writer.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		if m.production {
			header.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}
