// Package identity 承载已认证请求的上下文身份信息。
package identity

import (
	"context"
	"time"
)

// Identity 描述已认证请求的身份凭据与会话元数据。
type Identity struct {
	UserID string
	Role   string
	// TokenID 与 TokenExpiresAt 用于登出时将访问令牌加入黑名单。
	TokenID        string
	TokenExpiresAt time.Time
}

// ctxKey 是身份在 context 中的私有键。
type ctxKey struct{}

// With 将身份凭据注入 context。
func With(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext 从上下文中提取身份凭据；未认证时返回 false。
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}
