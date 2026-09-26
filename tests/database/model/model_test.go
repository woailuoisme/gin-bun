// Package model_test 验证数据模型上的领域判定，尤其是封禁到期的处理。
package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"bgin/internal/database/model"
)

// TestIsBanned 校验封禁判定把已过期的封禁视为解除。
func TestIsBanned(t *testing.T) {
	now := time.Now().UTC()
	expired := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	tests := []struct {
		name string
		user model.User
		want bool
	}{
		{"未封禁", model.User{}, false},
		{"永久封禁（无到期时间）", model.User{Banned: true}, true},
		{"封禁未到期", model.User{Banned: true, BanExpires: &future}, true},
		{"封禁已到期", model.User{Banned: true, BanExpires: &expired}, false},
		{"只有到期时间但未打封禁标记", model.User{BanExpires: &future}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.user.IsBanned(now))
		})
	}
}

// TestRoleAndStatusConstants 校验角色与状态的取值与 schema 的 CHECK 约束一致。
func TestRoleAndStatusConstants(t *testing.T) {
	assert.Equal(t, "user", model.RoleUser)
	assert.Equal(t, "admin", model.RoleAdmin)

	assert.Equal(t, "active", model.StatusActive)
	assert.Equal(t, "inactive", model.StatusInactive)
	assert.Equal(t, "banned", model.StatusBanned)

	assert.Equal(t, "credential", model.ProviderCredential)
}
