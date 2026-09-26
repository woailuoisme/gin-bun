// Package profile_test 验证前端自助档案模块的对外契约（Q27）。
package profile_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/features/client/profile"
)

// TestUpdateProfileRequestShape 校验自助更新只暴露 Q27 允许的字段。
func TestUpdateProfileRequestShape(t *testing.T) {
	var request profile.UpdateProfileRequest
	require.NoError(t, json.Unmarshal([]byte(`{
		"name":"Alice",
		"image":null,
		"username":"alice_01",
		"displayUsername":"Alice",
		"phoneNumber":"+8613800138000",
		"email":"should-be-ignored@bgin.local",
		"role":"admin"
	}`), &request))

	// 邮箱与角色不在自助契约里：JSON 里出现也不会被解析进结构体。
	encoded, err := json.Marshal(request)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "email")
	assert.NotContains(t, string(encoded), "role")

	assert.True(t, request.Name.IsSet())
	assert.True(t, request.Image.IsClear())
	assert.True(t, request.Username.IsSet())
}
