package models

import (
	"time"

	"github.com/uptrace/bun"
)

// ProviderCredential 是本地密码账号的 provider_id。
// 一个 User 可以有多个 Account，但本地密码只能有一条。
const ProviderCredential = "credential"

// Account 对应 "account" 表：一个 User 的一条登录途径。
type Account struct {
	bun.BaseModel `bun:"table:account,alias:a"`

	Base

	UserID                string     `bun:"user_id"`
	AccountID             string     `bun:"account_id"`
	ProviderID            string     `bun:"provider_id"`
	Password              *string    `bun:"password"`
	AccessToken           *string    `bun:"access_token"`
	RefreshToken          *string    `bun:"refresh_token"`
	IDToken               *string    `bun:"id_token"`
	AccessTokenExpiresAt  *time.Time `bun:"access_token_expires_at"`
	RefreshTokenExpiresAt *time.Time `bun:"refresh_token_expires_at"`
	Scope                 *string    `bun:"scope"`
}
