package realtime

import (
	"github.com/gin-gonic/gin"

	"bgin/internal/middleware"
)

// RegisterRoutes 注册实时通信客户端端点与 Centrifugo 代理回调端点。
func RegisterRoutes(router *gin.RouterGroup, handler *Handler, mw *middleware.Middleware) {
	group := router.Group("/realtime")

	// 客户端连接凭据（需要身份认证，速率限制为 authenticated）
	group.GET("/token", mw.RequireAuth(), mw.RateLimit(middleware.TierAuthenticated), handler.GetToken)

	// Centrifugo 反向代理鉴权端点（内部基于 HMAC-SHA256 签名校验）。
	//
	// 刻意不叠加按 IP 计数的限流档：该端点的合法调用者只有 Centrifugo 本身，
	// 所有订阅鉴权都来自同一个上游主机 IP，套用户档配额会把正常订阅拦成 429；
	// 真正的访问控制由 HMAC 签名承担，异常流量交给入口层的全局限流与 WAF。
	//
	// 注意：未启用 realtime（CENTRIFUGO_ENABLED=false）时该端点一律返回 403。
	// 密钥为空即无从鉴别调用方，fail-closed 优先于协议形态的应答。
	group.POST("/proxy/subscribe", handler.ProxySubscribe)
}
