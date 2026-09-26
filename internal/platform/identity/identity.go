// Package identity 承载已认证请求的身份。
// 身份全部来自令牌载荷，不查库——这是纯 JWT 方案的直接结果（见 ADR-0001）。
package identity

import (
	"context"
	"time"
)

// Identity 是已认证请求的身份。
type Identity struct {
	UserID string
	Role   string
	// TokenID 与 TokenExpiresAt 用于登出时把访问令牌写入撤销名单。
	TokenID        string
	TokenExpiresAt time.Time
}

// ctxKey 是身份在 context 中的私有键。
type ctxKey struct{}

// With 把身份写入 context，由认证中间件调用。
func With(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext 取出身份；未认证请求返回 false。
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}
