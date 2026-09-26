package model

import (
	"time"

	"github.com/uptrace/bun"
)

// 用户角色。库层有 CHECK 约束限定取值，新增角色必须同时改 schema 与策略矩阵。
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// 用户账号状态。
const (
	StatusActive   = "active"
	StatusInactive = "inactive"
	StatusBanned   = "banned"
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
