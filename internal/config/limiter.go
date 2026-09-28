package config

import "fmt"

// LimiterConfig 描述请求速率限制配额（每分钟最大请求数，0 为不限制）。
type LimiterConfig struct {
	Anonymous     int `env:"RATELIMIT_ANONYMOUS" envDefault:"60"`
	Authenticated int `env:"RATELIMIT_AUTHENTICATED" envDefault:"300"`
	Sensitive     int `env:"RATELIMIT_SENSITIVE" envDefault:"10"`
}

// validateLimiter 校验速率限制配额：0 表示显式关闭，负值属于非法配置。
func (c *Config) validateLimiter() error {
	tiers := []struct {
		name  string
		value int
	}{
		{"RATELIMIT_ANONYMOUS", c.Limiter.Anonymous},
		{"RATELIMIT_AUTHENTICATED", c.Limiter.Authenticated},
		{"RATELIMIT_SENSITIVE", c.Limiter.Sensitive},
	}
	for _, tier := range tiers {
		if tier.value < 0 {
			return fmt.Errorf("config: %s cannot be negative, use 0 to disable the tier explicitly", tier.name)
		}
	}
	return nil
}
