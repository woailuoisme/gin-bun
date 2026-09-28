package wechat_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/config"
	"bgin/internal/platform/wechat"
)

func TestWeChatClientCreationAndAccessors(t *testing.T) {
	cfg := config.WeChatMiniProgramConfig{
		AppID:     "wx1234567890abcdef",
		AppSecret: "secret1234567890abcdef1234567890",
	}

	client := wechat.NewClient(cfg, nil)
	require.NotNil(t, client)
	assert.Equal(t, cfg.AppID, client.AppID())
}

func TestWeChatClientUninitializedOperations(t *testing.T) {
	// 空凭据构造客户端应返回 nil
	client := wechat.NewClient(config.WeChatMiniProgramConfig{}, nil)
	assert.Nil(t, client)

	ctx := t.Context()

	// nil 客户端调用各方法应优雅返回错误
	_, err := client.Code2Session(ctx, "mock_code")
	require.Error(t, err)

	_, err = client.GetPhoneNumber(ctx, "mock_code")
	require.Error(t, err)

	_, err = client.GetUnlimitedCode(ctx, wechat.UnlimitedCodeParams{
		Scene: "id=123",
	})
	require.Error(t, err)
}
