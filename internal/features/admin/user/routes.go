// Package admin 实现后台用户管理，对外路径为 /v1/admin/users。
//
// 本包刻意不参与 API 文档生成：管理端接口仅供内部使用，不是对外契约的一部分。
// 前端用户的本人档案在 internal/features/client/profile，
// 两个交付面共用的用户域内核在 internal/shared/user，见 ADR-0005。
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
