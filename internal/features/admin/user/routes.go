// Package admin 实现后台系统用户管理模块 (/v1/admin/users)。
package admin

import (
	"github.com/gin-gonic/gin"

	"bgin/internal/middleware"
	"bgin/internal/platform/authorization"
)

// RegisterAdminRoutes 注册管理端点（/v1/admin/users）。
// 每个端点各自声明所需权限码，权限矩阵见 internal/platform/authorization/rbac/policy.csv。
func RegisterAdminRoutes(router *gin.RouterGroup, handler *Handler, mw *middleware.Middleware) {
	group := router.Group("/admin/users", mw.RequireAuth("admin"), mw.RateLimit(middleware.TierAuthenticated))

	group.GET("", mw.RequirePermission(authorization.ResourceUsers, authorization.ActionList), handler.ListUsers)
	group.GET("/export", mw.RequirePermission(authorization.ResourceUsers, authorization.ActionList), handler.ExportUsers)
	group.POST("", mw.RequirePermission(authorization.ResourceUsers, authorization.ActionCreate), handler.CreateUser)
	group.GET("/:id", mw.RequirePermission(authorization.ResourceUsers, authorization.ActionRead), handler.GetUser)
	group.PATCH("/:id", mw.RequirePermission(authorization.ResourceUsers, authorization.ActionUpdate), handler.UpdateUser)
	group.DELETE("/:id", mw.RequirePermission(authorization.ResourceUsers, authorization.ActionDelete), handler.DeleteUser)
}
