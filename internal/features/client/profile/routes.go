// Package profile 实现前端用户的本人档案模块，对外路径为 /v1/users/me。
//
// 它与管理端的 internal/features/admin/user 是同一用户记录的两个交付面：
// 本包只做前端自助可及的读写，档案列的翻译与落库规则共用 internal/shared/user，
// 见 ADR-0005。
package profile

import (
	"github.com/gin-gonic/gin"

	"bgin/internal/middleware"
)

// RegisterRoutes 注册自助档案端点（/v1/users）。
//
// 路径保持 /users/me 不变：模块目录的划分不改变已发布的公开契约（Q27）。
func RegisterRoutes(router *gin.RouterGroup, handler *Handler, mw *middleware.Middleware) {
	group := router.Group("/users", mw.RateLimit(middleware.TierAuthenticated), mw.RequireAuth())

	group.GET("/me", handler.GetProfile)
	group.PATCH("/me", handler.UpdateProfile)
}
