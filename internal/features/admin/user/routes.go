// Package admin 实现后台系统用户管理模块 (/v1/admin/users)。
package admin

import (
	"github.com/gin-gonic/gin"

	"bgin/internal/middleware"
	"bgin/internal/platform/authz"
)

// RegisterAdminRoutes 注册管理端点（/v1/admin/users）。
// 每个端点各自声明所需权限码，权限矩阵见 internal/platform/authz/rbac/policy.csv。
func RegisterAdminRoutes(router *gin.RouterGroup, handler *Handler, mw *middleware.Middleware) {
	group := router.Group("/admin/users", mw.RequireAuth(), mw.RateLimit(middleware.TierAuthenticated))

	group.GET("", mw.RequirePermission(authz.ResourceUsers, authz.ActionList), handler.ListUsers)
	group.POST("", mw.RequirePermission(authz.ResourceUsers, authz.ActionCreate), handler.CreateUser)
	group.GET("/:id", mw.RequirePermission(authz.ResourceUsers, authz.ActionRead), handler.GetUser)
	group.PATCH("/:id", mw.RequirePermission(authz.ResourceUsers, authz.ActionUpdate), handler.UpdateUser)
	group.DELETE("/:id", mw.RequirePermission(authz.ResourceUsers, authz.ActionDelete), handler.DeleteUser)
}
