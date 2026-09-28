// Package config 声明 Centrifugo 实时通信服务端连接配置。
package config

import (
	"errors"
	"net/url"
	"strings"
	"time"
)

// CentrifugoConfig 描述 Centrifugo 实时通信服务连接配置（极简工业实践）。
type CentrifugoConfig struct {
	// Enabled 控制是否启用实时通信服务。
	Enabled bool `env:"CENTRIFUGO_ENABLED" envDefault:"false"`
	// URL Centrifugo 服务端 HTTP API 完整地址（例如 http://127.0.0.1:8000/api）。
	URL string `env:"CENTRIFUGO_URL"`
	// APIKey 服务端调用 Centrifugo HTTP API 凭据。
	APIKey string `env:"CENTRIFUGO_API_KEY"`
	// Secret 用于 HMAC-SHA256 签发客户端连接 Token 及校验 Proxy 请求签名的密钥。
	Secret string `env:"CENTRIFUGO_SECRET"`
	// TokenTTL 是签发给客户端的连接凭据有效期。连接凭据应短于会话寿命，过期后由客户端重新申请；
	// 显式配 0 视为未配置，回退到 2 小时。
	TokenTTL time.Duration `env:"CENTRIFUGO_TOKEN_TTL" envDefault:"2h"`
}

// RealtimeConfig 为 CentrifugoConfig 别名。
type RealtimeConfig = CentrifugoConfig

// WebSocketConfig 兼容别名。
type WebSocketConfig = CentrifugoConfig

// validateRealtime 校验 Centrifugo 配置。
func (c *Config) validateRealtime() error {
	if !c.Realtime.Enabled {
		return nil
	}
	if strings.TrimSpace(c.Realtime.URL) == "" {
		return errors.New("config: CENTRIFUGO_URL cannot be empty when realtime is enabled")
	}
	if _, err := url.ParseRequestURI(c.Realtime.URL); err != nil {
		return errors.New("config: invalid CENTRIFUGO_URL format")
	}
	// 密钥缺失会让代理签名校验无据可依（见 platform/realtime.VerifySignature 的 fail-closed 约定），
	// 等于把订阅鉴权端点完全敞开，故在启用时强制要求非空，杜绝带病启动。
	if strings.TrimSpace(c.Realtime.Secret) == "" {
		return errors.New("config: CENTRIFUGO_SECRET cannot be empty when realtime is enabled")
	}
	if strings.TrimSpace(c.Realtime.APIKey) == "" {
		return errors.New("config: CENTRIFUGO_API_KEY cannot be empty when realtime is enabled")
	}
	return nil
}
