package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"bgin/internal/platform/errs"
	"bgin/internal/platform/identity"
	"bgin/internal/platform/token"
)

// RequireAuth 校验 Bearer 访问令牌与黑名单状态，并将身份载荷注入请求上下文。
func (m *Middleware) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := bearerToken(c)
		if err != nil {
			abortWithError(c, err)
			return
		}

		claims, err := m.tokens.Parse(raw, token.TypeAccess)
		if err != nil {
			abortWithError(c, err)
			return
		}

		revoked, err := m.tokens.IsRevoked(c.Request.Context(), claims.ID)
		if err != nil {
			abortWithError(c, errs.Internal(err))
			return
		}
		if revoked {
			abortWithError(c, errs.Unauthorized(errs.CodeAuthTokenRevoked, "The token has been revoked"))
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

// RequirePermission 基于令牌角色执行 Casbin RBAC 权限校验。
func (m *Middleware) RequirePermission(resource, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := identity.FromContext(c.Request.Context())
		if !ok {
			abortWithError(c, errs.Unauthorized(errs.CodeAuthTokenInvalid, "Authentication is required"))
			return
		}

		allowed, err := m.authz.Can(id.Role, resource, action)
		if err != nil {
			abortWithError(c, errs.Internal(err))
			return
		}
		if !allowed {
			abortWithError(c, errs.Forbidden("You are not allowed to perform this action"))
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
