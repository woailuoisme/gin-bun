// Package token 负责访问令牌与刷新令牌的签发、校验与撤销。
// 签名固定 HS256，撤销名单落 Redis（见 ADR-0001）。
package token

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"bgin/internal/config"
	"bgin/internal/platform/cache"
	"bgin/internal/platform/errs"
)

// 令牌类型。两类令牌互不通用，避免刷新令牌被当作访问令牌使用。
const (
	TypeAccess  = "access"
	TypeRefresh = "refresh"
)

// 撤销名单的键由 cache.Key 构造（"jwt" / "revoked" / jti），命名空间在 cache 包里定义一处。

// Claims 是令牌载荷。api.md 3.1 约定携带 role 声明，不再有 guard（见 Q21）。
type Claims struct {
	jwt.RegisteredClaims

	Role string `json:"role"`
	Type string `json:"tokenType"`
}

// Pair 是一次签发的双令牌结果。
type Pair struct {
	AccessToken      string
	RefreshToken     string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
}

// Issuer 签发与校验令牌，并通过 Redis 承载撤销名单。
type Issuer struct {
	secret     []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
	blacklist  *cache.Client
}

// New 构造 Issuer。
func New(cfg config.JWTConfig, blacklist *cache.Client) *Issuer {
	return &Issuer{
		secret:     []byte(cfg.Secret),
		issuer:     cfg.Issuer,
		accessTTL:  cfg.AccessTTL,
		refreshTTL: cfg.RefreshTTL,
		blacklist:  blacklist,
	}
}

// Issue 同时签发访问令牌与刷新令牌，两枚令牌各自持有唯一 jti 以便独立撤销。
func (i *Issuer) Issue(userID, role string) (Pair, error) {
	now := time.Now().UTC()
	accessExpiresAt := now.Add(i.accessTTL)
	refreshExpiresAt := now.Add(i.refreshTTL)

	accessToken, err := i.sign(userID, role, TypeAccess, now, accessExpiresAt)
	if err != nil {
		return Pair{}, err
	}
	refreshToken, err := i.sign(userID, role, TypeRefresh, now, refreshExpiresAt)
	if err != nil {
		return Pair{}, err
	}

	return Pair{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		AccessExpiresAt:  accessExpiresAt,
		RefreshExpiresAt: refreshExpiresAt,
	}, nil
}

// Parse 校验签名、签发方、有效期与令牌类型，失败时返回可直接响应的 401 错误。
func (i *Issuer) Parse(raw, wantType string) (*Claims, error) {
	claims := &Claims{}

	parsed, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errs.Unauthorized(errs.CodeAuthTokenInvalid, "The token is invalid")
		}
		return i.secret, nil
	},
		jwt.WithIssuer(i.issuer),
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, errs.Unauthorized(errs.CodeAuthTokenExpired, "The token has expired")
		}
		return nil, errs.Wrap(errs.CodeAuthTokenInvalid, "The token is invalid", err)
	}
	if !parsed.Valid {
		return nil, errs.Unauthorized(errs.CodeAuthTokenInvalid, "The token is invalid")
	}
	if claims.Type != wantType {
		return nil, errs.Unauthorized(errs.CodeAuthTokenInvalid, "The token is invalid")
	}
	return claims, nil
}

// Revoke 把令牌的 jti 写入撤销名单，TTL 取其剩余寿命，过期后自动清理。
func (i *Issuer) Revoke(ctx context.Context, claims *Claims) error {
	if claims == nil {
		return nil
	}
	return i.RevokeByID(ctx, claims.ID, i.expiryOf(claims))
}

// RevokeByID 在只有 jti 与到期时间的场景下作废令牌（例如登出时只拿到中间件传入的身份）。
func (i *Issuer) RevokeByID(ctx context.Context, jti string, expiresAt time.Time) error {
	if jti == "" {
		return nil
	}

	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return nil
	}
	if err := i.blacklist.Set(ctx, cache.Key("jwt", "revoked", jti), "1", ttl); err != nil {
		return fmt.Errorf("token: write blacklist failed: %w", err)
	}
	return nil
}

// expiryOf 取出令牌的到期时间，缺失时返回零值。
func (i *Issuer) expiryOf(claims *Claims) time.Time {
	if claims.ExpiresAt == nil {
		return time.Time{}
	}
	return claims.ExpiresAt.Time
}

// IsRevoked 判断令牌是否已被撤销。
func (i *Issuer) IsRevoked(ctx context.Context, jti string) (bool, error) {
	if jti == "" {
		return false, nil
	}

	revoked, err := i.blacklist.Exists(ctx, cache.Key("jwt", "revoked", jti))
	if err != nil {
		return false, fmt.Errorf("token: check blacklist failed: %w", err)
	}
	return revoked, nil
}

// sign 用 HS256 签发一枚带唯一 jti 的令牌。
func (i *Issuer) sign(userID, role, tokenType string, now, expiresAt time.Time) (string, error) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Issuer:    i.issuer,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		Role: role,
		Type: tokenType,
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.secret)
	if err != nil {
		return "", fmt.Errorf("token: sign %s token failed: %w", tokenType, err)
	}
	return signed, nil
}
