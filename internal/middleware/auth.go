package middleware

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"bgin/internal/platform/authentication"
	"bgin/internal/platform/errs"
)

// RequireAuth 校验 Bearer 访问令牌、黑名单与指定守卫策略，并将认证身份载荷注入请求上下文。
// 可选指定 guardName 参数（例如 "admin"、"client"）；未指定时自动回退至默认守卫。
func (m *Middleware) RequireAuth(guardName ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := bearerToken(c)
		if err != nil {
			abortWithError(c, err)
			return
		}

		if m.authManager != nil {
			guard, err := m.authManager.Resolve(guardName...)
			if err != nil {
				abortWithError(c, errs.Internal(err))
				return
			}

			ctx, _, err := guard.AuthenticateToken(c.Request.Context(), raw)
			if err != nil {
				abortWithError(c, err)
				return
			}

			if m.authz != nil {
				ctx = authentication.WithGate(ctx, m.authz)
			}
			c.Request = c.Request.WithContext(ctx)
			c.Next()
			return
		}

		if len(guardName) > 0 {
			abortWithError(c, errs.Internal(errors.New("middleware: auth manager required for named guard")))
			return
		}

		if m.tokens == nil {
			abortWithError(c, errs.Internal(errors.New("middleware: token issuer not configured")))
			return
		}

		// 兜底降级：若未注入 AuthManager，则仅允许回退至默认基础令牌校验
		ctx, _, err := m.tokens.AuthenticateToken(c.Request.Context(), raw)
		if err != nil {
			abortWithError(c, err)
			return
		}

		if m.authz != nil {
			ctx = authentication.WithGate(ctx, m.authz)
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// RequirePermission 基于令牌角色执行 Casbin RBAC 权限校验。
func (m *Middleware) RequirePermission(resource, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := authentication.FromContext(c.Request.Context()); !ok {
			abortWithError(c, errs.Unauthorized(errs.CodeAuthTokenInvalid, "Authentication is required"))
			return
		}

		if m.authz == nil {
			abortWithError(c, errs.Internal(errors.New("middleware: authz enforcer not configured")))
			return
		}

		if !m.authz.Can(c.Request.Context(), resource, action) {
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
