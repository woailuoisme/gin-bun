package models

import (
	"time"

	"github.com/uptrace/bun"
)

// Verification 对应 "verification" 表：一次性凭据记录。
// 邮箱验证码与密码重置令牌共用此表，靠 identifier 前缀区分用途。
type Verification struct {
	bun.BaseModel `bun:"table:verification,alias:v"`

	Base

	Identifier string    `bun:"identifier"`
	Value      string    `bun:"value"`
	ExpiresAt  time.Time `bun:"expires_at"`
}
