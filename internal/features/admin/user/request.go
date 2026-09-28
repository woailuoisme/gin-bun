package admin

import (
	"time"

	"bgin/internal/platform/optional"
	"bgin/internal/shared/enums"
	"bgin/internal/shared/user"
)

// 分页默认值与上限，对应 api.md 6.1。
const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100
	// maxPage 是页码硬上限，防止深度翻页引发性能退化。
	maxPage = 1_000_000
)

// Role 是权限角色枚举的本地类型别名，规避 swag 跨包解析缺陷。
type Role = enums.Role

// Status 是账号状态枚举的本地类型别名，规避 swag 跨包解析缺陷。
type Status = enums.Status

// CreateUserRequest 是 POST /v1/admin/users 的请求体。
type CreateUserRequest struct {
	// 用户电子邮箱
	Email string `json:"email" example:"alex@example.com" validate:"required,email,max=254"`
	// 登录口令 (8-128位)
	Password string `json:"password" example:"Secret123!" validate:"required,min=8,max=128"`
	// 用户展示昵称
	Name string `json:"name" example:"Alex Mercer" validate:"required,min=1,max=120"`
	// 唯一登录用户名 (3-30位)
	Username *string `json:"username" example:"alexmercer" validate:"omitempty,min=3,max=30,usernameslug,notreserved"`
	// 用户头像 URL
	Image *string `json:"image" example:"https://example.com/avatar.png" validate:"omitempty,max=2048"`
	// 用户权限角色 (user: 普通用户, admin: 系统管理员)
	Role Role `json:"role" example:"user" enums:"user,admin" validate:"required,oneof=user admin"`
	// 邮箱是否已完成验证
	EmailVerified bool `json:"emailVerified" example:"true"`
}

// UpdateUserRequest 是 PATCH /v1/admin/users/:id 的请求体。
type UpdateUserRequest struct {
	// 用户展示昵称
	Name optional.Optional[string] `json:"name" swaggertype:"string" extensions:"x-nullable" example:"Alex Mercer"`
	// 用户头像 URL
	Image optional.Optional[string] `json:"image" swaggertype:"string" extensions:"x-nullable" example:"https://example.com/avatar.png"`
	// 唯一登录用户名 (3-30位)
	Username optional.Optional[string] `json:"username" swaggertype:"string" extensions:"x-nullable" example:"alexmercer"`
	// 用户显示名称
	DisplayUsername optional.Optional[string] `json:"displayUsername" swaggertype:"string" extensions:"x-nullable" example:"Alex Mercer"`
	// E.164 格式手机号
	PhoneNumber optional.Optional[string] `json:"phoneNumber" swaggertype:"string" extensions:"x-nullable" example:"+8613800138000"`
	// 邮箱是否已验证
	EmailVerified optional.Optional[bool] `json:"emailVerified" swaggertype:"boolean" extensions:"x-nullable" example:"true"`
	// 用户权限角色 (user: 普通用户, admin: 系统管理员)
	Role optional.Optional[Role] `json:"role" swaggertype:"string" extensions:"x-nullable" example:"admin" enums:"user,admin"`
	// 账号状态 (active: 正常活跃, inactive: 未激活, banned: 已封禁)
	Status optional.Optional[Status] `json:"status" swaggertype:"string" extensions:"x-nullable" example:"active" enums:"active,inactive,banned"`
	// 是否封禁
	Banned optional.Optional[bool] `json:"banned" swaggertype:"boolean" extensions:"x-nullable" example:"false"`
	// 封禁原因
	BanReason optional.Optional[string] `json:"banReason" swaggertype:"string" extensions:"x-nullable" example:"违规发布广告"`
	// 封禁到期时间 (ISO 8601 UTC)
	BanExpires optional.Optional[time.Time] `json:"banExpires" swaggertype:"string" extensions:"x-nullable" example:"2026-10-01T00:00:00Z"`
}

// fieldSet 将请求中的档案字段投影为共享内核字段集。
func (r UpdateUserRequest) fieldSet() user.FieldSet {
	return user.FieldSet{
		Name:            r.Name,
		Image:           r.Image,
		Username:        r.Username,
		DisplayUsername: r.DisplayUsername,
		PhoneNumber:     r.PhoneNumber,
	}
}

// managementPatch 是管理更新的校验投影。
type managementPatch struct {
	user.ProfilePatch
	Role       *Role      `json:"role" validate:"omitempty,oneof=user admin"`
	Status     *Status    `json:"status" validate:"omitempty,oneof=active inactive banned"`
	BanReason  *string    `json:"banReason" validate:"omitempty,max=500"`
	BanExpires *time.Time `json:"banExpires"`
}

// ListUsersQuery 是 GET /v1/admin/users 的查询参数。
type ListUsersQuery struct {
	// 当前页码 (从 1 开始，默认 1，上限 1000000，与 maxPage 常量保持一致)
	Page int `form:"page" json:"page" example:"1" validate:"omitempty,min=1,max=1000000"`
	// 每页条数 (1-100，默认 20)
	PageSize int `form:"pageSize" json:"pageSize" example:"20" validate:"omitempty,min=1,max=100"`
	// 排序字段 (字段名前加 - 表示倒序，如 -createdAt)
	Sort string `form:"sort" json:"sort" example:"-createdAt" validate:"omitempty,usersort,max=120"`
	// 关键字搜索 (按邮箱、昵称、用户名模糊检索)
	Search string `form:"search" json:"search" example:"alex" validate:"omitempty,max=120"`
	// 按账号状态过滤 (active: 正常活跃, inactive: 未激活, banned: 已封禁)
	Status Status `form:"status" json:"status" example:"active" enums:"active,inactive,banned" validate:"omitempty,oneof=active inactive banned"`
	// 按权限角色过滤 (user: 普通用户, admin: 系统管理员)
	Role Role `form:"role" json:"role" example:"user" enums:"user,admin" validate:"omitempty,oneof=user admin"`
}
