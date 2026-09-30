package models

import (
	"time"

	"github.com/uptrace/bun"

	"bgin/internal/shared/enums"
)

// 用户角色枚举别名，取值统一定义于 internal/shared/enums。
const (
	RoleUser  = string(enums.RoleUser)
	RoleAdmin = string(enums.RoleAdmin)
)

// 用户账号状态枚举别名，取值统一定义于 internal/shared/enums。
const (
	StatusActive   = string(enums.StatusActive)
	StatusInactive = string(enums.StatusInactive)
	StatusBanned   = string(enums.StatusBanned)
)

// User 映射系统用户表 ("user")。
type User struct {
	bun.BaseModel `bun:"table:user,alias:u"`

	Base

	Name                string     `bun:"name"`
	Email               string     `bun:"email"`
	EmailVerified       bool       `bun:"email_verified"`
	Image               *string    `bun:"image"`
	Role                string     `bun:"role"`
	Status              string     `bun:"status"`
	Banned              bool       `bun:"banned"`
	TwoFactorEnabled    bool       `bun:"two_factor_enabled"`
	Username            *string    `bun:"username"`
	DisplayUsername     *string    `bun:"display_username"`
	PhoneNumber         *string    `bun:"phone_number"`
	PhoneNumberVerified bool       `bun:"phone_number_verified"`
	IsAnonymous         bool       `bun:"is_anonymous"`
	LastLoginMethod     *string    `bun:"last_login_method"`
	BanReason           *string    `bun:"ban_reason"`
	BanExpires          *time.Time `bun:"ban_expires"`
	LastLoginAt         *time.Time `bun:"last_login_at"`
	DeletedAt           *time.Time `bun:"deleted_at"`
}

// IsBanned 判定账号是否处于封禁状态，已过期的封禁视为解除。
func (u *User) IsBanned(now time.Time) bool {
	if !u.Banned {
		return false
	}
	return u.BanExpires == nil || u.BanExpires.After(now)
}

// SearchableAs 返回 User 实体对应的搜索索引基础名称。
func (u *User) SearchableAs() string {
	return "users"
}

// SearchableKey 返回 User 实体的全局唯一搜索键。
func (u *User) SearchableKey() string {
	return u.ID
}

// ToSearchableArray 将用户模型映射为全文搜索文档字典。
func (u *User) ToSearchableArray() map[string]any {
	doc := map[string]any{
		"id":             u.ID,
		"name":           u.Name,
		"email":          u.Email,
		"role":           u.Role,
		"status":         u.Status,
		"email_verified": u.EmailVerified,
		"created_at":     u.CreatedAt.Unix(),
		"updated_at":     u.UpdatedAt.Unix(),
	}
	if u.Username != nil {
		doc["username"] = *u.Username
	}
	if u.DisplayUsername != nil {
		doc["display_username"] = *u.DisplayUsername
	}
	if u.PhoneNumber != nil {
		doc["phone_number"] = *u.PhoneNumber
	}
	return doc
}

// GetModelType 返回多态媒体附件的模型类型标识。
func (u *User) GetModelType() string {
	return "user"
}

// GetModelID 返回多态媒体附件的宿主主键 ID。
func (u *User) GetModelID() string {
	return u.ID
}

// GetAuthIdentifier 返回认证主体唯一标识符（用户 ID）。
func (u *User) GetAuthIdentifier() string {
	if u == nil {
		return ""
	}
	return u.ID
}

// GetAuthRole 返回认证主体的权限角色。
func (u *User) GetAuthRole() string {
	if u == nil {
		return ""
	}
	return u.Role
}

// HasVerifiedEmail 判断用户邮箱是否已通过验证。
func (u *User) HasVerifiedEmail() bool {
	if u == nil {
		return false
	}
	return u.EmailVerified
}

// GetJWTCustomClaims 返回需要嵌入 JWT 载荷的自定义声明字典。
func (u *User) GetJWTCustomClaims() map[string]any {
	if u == nil {
		return nil
	}
	return map[string]any{
		"email_verified": u.EmailVerified,
	}
}
