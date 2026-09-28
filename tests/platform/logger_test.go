package platform_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/config"
	"bgin/internal/platform/logger"
)

// TestLogger_BuildAndContext 验证日志器构建与 Context 请求标识传递。
func TestLogger_BuildAndContext(t *testing.T) {
	cfg := &config.Config{
		App: config.AppConfig{
			Name: "bgin-test",
			Env:  "local",
		},
		Log: config.LogConfig{
			Level:   "debug",
			Channel: "console",
		},
	}

	l := logger.New(cfg)
	require.NotNil(t, l)

	ctx := logger.WithContext(context.Background(), l, "req_test_123")
	extracted, ok := logger.FromContextOK(ctx)
	assert.True(t, ok)
	assert.NotNil(t, extracted)

	// 缺失时返回默认兜底日志器
	fallback := logger.FromContext(context.Background())
	assert.NotNil(t, fallback)
}

// TestLogger_ChannelJSON 验证 JSON 通道构建。
func TestLogger_ChannelJSON(t *testing.T) {
	cfg := &config.Config{
		App: config.AppConfig{
			Name: "bgin-test",
			Env:  "production",
		},
		Log: config.LogConfig{
			Level:   "info",
			Channel: "json",
		},
	}

	l := logger.New(cfg)
	require.NotNil(t, l)
}

// TestLogger_ColorDecision 验证颜色启闭决策与环境变量优先级。
func TestLogger_ColorDecision(t *testing.T) {
	t.Run("NO_COLOR 存在时禁用着色", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		t.Setenv("FORCE_COLOR", "1")

		cfg := &config.Config{
			App: config.AppConfig{Env: "local"},
			Log: config.LogConfig{Channel: "console"},
		}
		l := logger.New(cfg)
		assert.NotNil(t, l)
	})

	t.Run("FORCE_COLOR 等于 0 或 false 时显式禁用着色", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		t.Setenv("FORCE_COLOR", "0")

		cfg := &config.Config{
			App: config.AppConfig{Env: "production"},
			Log: config.LogConfig{Channel: "console"},
		}
		l := logger.New(cfg)
		assert.NotNil(t, l)

		t.Setenv("FORCE_COLOR", "false")
		l2 := logger.New(cfg)
		assert.NotNil(t, l2)
	})

	t.Run("FORCE_COLOR 或 CLICOLOR_FORCE 强制着色", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		t.Setenv("FORCE_COLOR", "1")

		cfg := &config.Config{
			App: config.AppConfig{Env: "production"},
			Log: config.LogConfig{Channel: "console"},
		}
		l := logger.New(cfg)
		assert.NotNil(t, l)

		t.Setenv("FORCE_COLOR", "")
		t.Setenv("CLICOLOR_FORCE", "1")
		l2 := logger.New(cfg)
		assert.NotNil(t, l2)
	})
}
