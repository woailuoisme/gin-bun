package admin

import (
	"time"

	"bgin/internal/models"
	"bgin/internal/platform/response"
	"bgin/internal/shared/user"
)

// Profile 是共享内核档案视图的类型别名，供本模块的响应体与 Swagger 注解引用。
type Profile = user.Profile

// User 是管理端视角的系统用户视图模型。
type User struct {
	Profile

	// 账号是否已被封禁
	Banned bool `json:"banned" example:"false"`
	// 封禁原因说明
	BanReason *string `json:"banReason" example:"违规发布广告"`
	// 封禁到期时间 (ISO 8601 UTC)
	BanExpires *time.Time `json:"banExpires" example:"2026-10-01T00:00:00Z"`
	// 最后登录时间 (ISO 8601 UTC)
	LastLoginAt *time.Time `json:"lastLoginAt" example:"2026-09-27T10:00:00Z"`
	// 最后登录认证方式
	LastLoginMethod *string `json:"lastLoginMethod" example:"email"`
}

// UserPage 是用户分页列表的响应结构。
type UserPage struct {
	// 用户列表数据
	Items []User `json:"items"`
	// 分页元信息
	Meta response.Meta `json:"meta"`
}

// projectUser 把模型投影为管理视角的响应体。
func projectUser(entity *models.User) User {
	return User{
		Profile:         user.ProjectProfile(entity),
		Banned:          entity.Banned,
		BanReason:       entity.BanReason,
		BanExpires:      models.UTCPtr(entity.BanExpires),
		LastLoginAt:     models.UTCPtr(entity.LastLoginAt),
		LastLoginMethod: entity.LastLoginMethod,
	}
}
