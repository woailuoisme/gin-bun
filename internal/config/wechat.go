package config

import (
	"errors"
	"strings"
)

// WeChatConfig 定义微信开放平台（公众平台 / 小程序）的服务端能力配置。
//
// 与 PaymentConfig 下的微信支付凭据刻意分离：本配置面向 api.weixin.qq.com，
// 承载小程序登录、手机号获取与小程序码等身份与入口能力；微信支付面向
// api.mch.weixin.qq.com，使用商户证书签名，是另一套完全独立的凭据与错误语义。
// 两者不可混用，也不共用字段。
type WeChatConfig struct {
	MiniProgram WeChatMiniProgramConfig
}

// WeChatMiniProgramConfig 定义单个小程序的凭据。
//
// 这里只放凭据：接口调用凭据（access_token）的获取、缓存与提前失效量由
// internal/platform/wechat 的客户端统一管理（稳定版接口 + 进程内/Redis 缓存），
// 不需要也不应该在这里配置——它没有任何真正需要按其环境调整的取值。
type WeChatMiniProgramConfig struct {
	// AppID 小程序 AppID。
	AppID string `env:"WECHAT_MINIPROGRAM_APP_ID"`
	// AppSecret 小程序 AppSecret，仅服务端持有，绝不可下发到客户端。
	AppSecret string `env:"WECHAT_MINIPROGRAM_APP_SECRET"`
}

// Declared 判断小程序凭据是否已被声明。
// 沿用「配置即启用」的原则：填写任一字段即视为已声明。
func (c WeChatMiniProgramConfig) Declared() bool {
	return strings.TrimSpace(c.AppID) != "" || strings.TrimSpace(c.AppSecret) != ""
}

// WeChatMiniProgramConfigured 判断小程序能力是否已配置。
func WeChatMiniProgramConfigured(cfg *Config) bool {
	if cfg == nil {
		return false
	}

	return cfg.WeChat.MiniProgram.Declared()
}

// validateWeChat 校验微信配置项的有效性。
func (c *Config) validateWeChat() error {
	if c.WeChat.MiniProgram.Declared() {
		if strings.TrimSpace(c.WeChat.MiniProgram.AppID) == "" {
			return errors.New("config: WECHAT_MINIPROGRAM_APP_ID cannot be empty when app secret is declared")
		}
		if strings.TrimSpace(c.WeChat.MiniProgram.AppSecret) == "" {
			return errors.New("config: WECHAT_MINIPROGRAM_APP_SECRET cannot be empty when app id is declared")
		}
	}
	return nil
}
