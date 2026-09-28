package config

import (
	"errors"
	"strings"
)

// WeChatConfig 定义微信开放平台（公众平台 / 小程序）的服务端能力配置。
type WeChatConfig struct {
	MiniProgram WeChatMiniProgramConfig
}

// WeChatMiniProgramConfig 定义单个微信小程序服务端凭据。
type WeChatMiniProgramConfig struct {
	// AppID 小程序 AppID。
	AppID string `env:"WECHAT_MINIPROGRAM_APP_ID"`
	// AppSecret 小程序 AppSecret。
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
