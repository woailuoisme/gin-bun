// Package models_test 验证数据模型上的领域判定，尤其是封禁到期的处理。
package models_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"bgin/internal/models"
)

// TestIsBanned 校验封禁判定把已过期的封禁视为解除。
func TestIsBanned(t *testing.T) {
	now := time.Now().UTC()
	expired := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	tests := []struct {
		name string
		user models.User
		want bool
	}{
		{"未封禁", models.User{}, false},
		{"永久封禁（无到期时间）", models.User{Banned: true}, true},
		{"封禁未到期", models.User{Banned: true, BanExpires: &future}, true},
		{"封禁已到期", models.User{Banned: true, BanExpires: &expired}, false},
		{"只有到期时间但未打封禁标记", models.User{BanExpires: &future}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.user.IsBanned(now))
		})
	}
}

// TestRoleAndStatusConstants 校验角色与状态的取值与 schema 的 CHECK 约束一致。
func TestRoleAndStatusConstants(t *testing.T) {
	assert.Equal(t, "user", models.RoleUser)
	assert.Equal(t, "admin", models.RoleAdmin)

	assert.Equal(t, "active", models.StatusActive)
	assert.Equal(t, "inactive", models.StatusInactive)
	assert.Equal(t, "banned", models.StatusBanned)

	assert.Equal(t, "credential", models.ProviderCredential)
}
