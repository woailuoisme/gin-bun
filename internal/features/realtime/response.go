package realtime

import (
	"time"

	platformRealtime "bgin/internal/platform/realtime"
)

// TokenResponse 描述客户端连接 Centrifugo 所需的 JWT 凭据与连接元信息。
type TokenResponse struct {
	// Centrifugo 客户端连接 Token (JWT)
	Token string `json:"token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
	// Token 过期时间戳 (RFC3339)
	ExpiresAt time.Time `json:"expires_at" example:"2026-09-27T19:30:00Z"`
	// Centrifugo WebSocket/SSE 服务端地址
	WSURL string `json:"ws_url" example:"ws://127.0.0.1:8000/connection/websocket"`
}

// SubscribeResponse 为 Centrifugo HTTP Proxy 订阅鉴权响应别名（供 Swagger 注解直接引用）。
type SubscribeResponse = platformRealtime.SubscribeResponse

// SubscribeResult 为 Centrifugo HTTP Proxy 订阅结果别名。
type SubscribeResult = platformRealtime.SubscribeResult

// ProxyError 为 Centrifugo HTTP Proxy 错误结构别名。
type ProxyError = platformRealtime.ProxyError
