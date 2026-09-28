package config

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// CORSConfig 描述跨域资源共享策略（对齐 Laravel config/cors.php）。
//
// 刻意不提供默认值：未配置即不放行任何跨域来源。
// 提供 "*" 默认值等于让每个部署默认把响应开放给任意站点，而这类"安静放行"很难被发现。
type CORSConfig struct {
	AllowedOrigins []string `env:"CORS_ALLOWED_ORIGINS"`
}

// validateCORS 校验跨域来源白名单：仅接受通配符 * 或带 scheme 的绝对来源，
// 并在生产环境拒绝通配符。
//
// 该钩子此前为空实现，于是 "https://a.com," 这类多写一个逗号的笔误、
// 或漏写 scheme 的 "a.com" 都会静默地不让任何来源通过，排障时极难定位。
func (c *Config) validateCORS() error {
	if c.IsProduction() && slices.Contains(c.CORS.AllowedOrigins, "*") {
		return errors.New("config: CORS_ALLOWED_ORIGINS cannot be \"*\" in production, list explicit origins instead")
	}

	for _, origin := range c.CORS.AllowedOrigins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			return errors.New("config: CORS_ALLOWED_ORIGINS contains an empty entry, remove the redundant comma")
		}
		if trimmed == "*" {
			continue
		}

		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("config: CORS_ALLOWED_ORIGINS contains an invalid origin %q, expected \"*\" or \"scheme://host[:port]\"", origin)
		}
	}
	return nil
}
