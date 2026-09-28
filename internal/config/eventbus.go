package config

import (
	"strings"
)

// EventBusConfig 描述事件总线与 CQRS 路由参数。
type EventBusConfig struct {
	Driver      string `env:"EVENTBUS_DRIVER" envDefault:"go_channel"`
	TopicPrefix string `env:"EVENTBUS_TOPIC_PREFIX" envDefault:"bgin"`
	LogLevel    string `env:"EVENTBUS_LOG_LEVEL" envDefault:"warn"`
}

// validateEventBus 校验事件总线配置并设置合理默认值。
func (c *Config) validateEventBus() error {
	if strings.TrimSpace(c.EventBus.Driver) == "" {
		c.EventBus.Driver = "go_channel"
	}
	if strings.TrimSpace(c.EventBus.TopicPrefix) == "" {
		c.EventBus.TopicPrefix = "bgin"
	}
	if strings.TrimSpace(c.EventBus.LogLevel) == "" {
		c.EventBus.LogLevel = "warn"
	}
	return nil
}
