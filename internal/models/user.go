package models

import (
	"time"

	"github.com/uptrace/bun"

	"bgin/internal/shared/enums"
)

// 用户角色。取值只有一处定义（internal/shared/enums），这里是指向它的别名：
// 同一枚举在两处各写一遍，改一处漏一处是必然的。库层有 CHECK 约束限定取值，
// 新增角色必须同时改 schema 与策略矩阵。
const (
	RoleUser  = string(enums.RoleUser)
	RoleAdmin = string(enums.RoleAdmin)
)

// 用户账号状态。同样取自 internal/shared/enums，并保持 string 类型，
// 以便直接写入实体字段而不必在每处调用点加转换。
const (
	StatusActive   = string(enums.StatusActive)
	StatusInactive = string(enums.StatusInactive)
	StatusBanned   = string(enums.StatusBanned)
)

// User 对应 "user" 表。表名是 PostgreSQL 保留字，必须显式声明。
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
