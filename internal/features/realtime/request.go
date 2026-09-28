// Package realtime 提供 Centrifugo 实时通信端点（客户端 Token 颁发与 Webhook 订阅鉴权代理）。
package realtime

import (
	platformRealtime "bgin/internal/platform/realtime"
)

// SubscribeRequest 为 Centrifugo HTTP Proxy 订阅鉴权请求别名（供 Swagger 注解直接引用）。
type SubscribeRequest = platformRealtime.SubscribeRequest
