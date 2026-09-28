package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"bgin/internal/platform/errs"
	"bgin/internal/platform/identity"
	"bgin/internal/platform/token"
)

// RequireAuth 校验 Bearer 访问令牌与撤销名单，并把身份写入请求 context。
// 刻意不查库：身份完全来自令牌载荷，这是纯 JWT 方案的直接结果（ADR-0001）。
func (m *Middleware) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := bearerToken(c)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}

		claims, err := m.tokens.Parse(raw, token.TypeAccess)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}

		revoked, err := m.tokens.IsRevoked(c.Request.Context(), claims.ID)
		if err != nil {
			_ = c.Error(errs.Internal(err))
			c.Abort()
			return
		}
		if revoked {
			_ = c.Error(errs.Unauthorized(errs.CodeAuthTokenRevoked, "The token has been revoked"))
			c.Abort()
			return
		}

		var tokenExpiresAt time.Time
		if claims.ExpiresAt != nil {
			tokenExpiresAt = claims.ExpiresAt.Time
		}

		ctx := identity.With(c.Request.Context(), identity.Identity{
			UserID:         claims.Subject,
			Role:           claims.Role,
			TokenID:        claims.ID,
			TokenExpiresAt: tokenExpiresAt,
		})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// RequirePermission 依据令牌中的角色判定权限码，未授权返回 403。
// 角色不由数据库实时查询决定，因此改角色必须重新登录才生效（ADR-0001、ADR-0003）。
func (m *Middleware) RequirePermission(resource, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := identity.FromContext(c.Request.Context())
		if !ok {
			_ = c.Error(errs.Unauthorized(errs.CodeAuthTokenInvalid, "Authentication is required"))
			c.Abort()
			return
		}

		allowed, err := m.authz.Can(id.Role, resource, action)
		if err != nil {
			_ = c.Error(errs.Internal(err))
			c.Abort()
			return
		}
		if !allowed {
			_ = c.Error(errs.Forbidden("You are not allowed to perform this action"))
			c.Abort()
			return
		}
		c.Next()
	}
}

// bearerToken 从 Authorization 头提取 Bearer 令牌。
func bearerToken(c *gin.Context) (string, error) {
	header := strings.TrimSpace(c.GetHeader("Authorization"))
	if header == "" {
		return "", errs.Unauthorized(errs.CodeAuthTokenInvalid, "Authentication is required")
	}

	scheme, raw, found := strings.Cut(header, " ")
	raw = strings.TrimSpace(raw)
	if !found || !strings.EqualFold(scheme, "Bearer") || raw == "" {
		return "", errs.Unauthorized(
			errs.CodeAuthTokenInvalid,
			"Authorization header must use the Bearer scheme",
		)
	}
	return raw, nil
}
