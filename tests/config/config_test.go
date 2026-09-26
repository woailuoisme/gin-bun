// Package config_test 以黑盒方式验证 gin 运行模式的推导规则。
//
// 规则的关键不在于取值本身，而在于方向：只有明确声明的本地环境才允许 debug，
// 未知的 APP_ENV 必须落到 release，否则会把完整路由表打进非本地环境的日志。
package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"bgin/internal/config"
)

// newConfig 构造只填了模式相关字段的配置，避免依赖真实环境变量。
func newConfig(env, override string) *config.Config {
	return &config.Config{App: config.AppConfig{Env: env, GinMode: override}}
}

// TestResolveGinMode 覆盖推导与显式覆盖两条路径。
func TestResolveGinMode(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		override string
		want     string
	}{
		{"本地用 debug", "local", "", config.ModeDebug},
		{"development 同样用 debug", "development", "", config.ModeDebug},
		{"生产用 release", "production", "", config.ModeRelease},
		{"预发用 release", "staging", "", config.ModeRelease},
		{"未知环境一律 release", "producton", "", config.ModeRelease},
		{"空环境一律 release", "", "", config.ModeRelease},
		{"显式覆盖优先于推导", "local", config.ModeRelease, config.ModeRelease},
		{"覆盖可用 test", "production", config.ModeTest, config.ModeTest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, newConfig(tt.env, tt.override).ResolveGinMode())
		})
	}
}

// TestModePredicates 锁定 IsLocal 与 IsProduction 的边界，二者对同一环境不会同时为真。
func TestModePredicates(t *testing.T) {
	tests := []struct {
		env        string
		wantLocal  bool
		wantIsProd bool
	}{
		{"local", true, false},
		{"development", true, false},
		{"production", false, true},
		{"staging", false, false},
		{"", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			cfg := newConfig(tt.env, "")
			assert.Equal(t, tt.wantLocal, cfg.IsLocal())
			assert.Equal(t, tt.wantIsProd, cfg.IsProduction())
			assert.False(t, cfg.IsLocal() && cfg.IsProduction(), "两个判定不应同时为真")
		})
	}
}
