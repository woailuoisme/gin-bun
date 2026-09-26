package admin

import (
	"time"

	"bgin/internal/platform/optional"
	"bgin/internal/shared/user"
)

// 分页默认值与上限，对应 api.md 6.1。
const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100
)

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
	Role string `json:"role" example:"user" enums:"user,admin" validate:"required,oneof=user admin"`
	// 邮箱是否已完成验证
	EmailVerified bool `json:"emailVerified" example:"true"`
}

// UpdateUserRequest 是 PATCH /v1/admin/users/:id 的请求体。
// 在自助可改字段之外追加角色、状态与封禁信息；邮箱与口令一律不在此处修改（Q27）。
//
// 本包不参与 API 文档生成，因此这里不写 swaggertype 之类的文档标签；
// 三态字段在文档上的修正只由客户端模块 internal/features/client/profile 承担。
type UpdateUserRequest struct {
	// 用户展示昵称
	Name optional.Optional[string] `json:"name" example:"Alex Mercer"`
	// 用户头像 URL
	Image optional.Optional[string] `json:"image" example:"https://example.com/avatar.png"`
	// 唯一登录用户名 (3-30位)
	Username optional.Optional[string] `json:"username" example:"alexmercer"`
	// 用户显示名称
	DisplayUsername optional.Optional[string] `json:"displayUsername" example:"Alex Mercer"`
	// E.164 格式手机号
	PhoneNumber optional.Optional[string] `json:"phoneNumber" example:"+8613800138000"`
	// 邮箱是否已验证
	EmailVerified optional.Optional[bool] `json:"emailVerified" example:"true"`
	// 用户权限角色 (user: 普通用户, admin: 系统管理员)
	Role optional.Optional[string] `json:"role" example:"admin" enums:"user,admin"`
	// 账号状态 (active: 正常活跃, suspended: 封禁停用)
	Status optional.Optional[string] `json:"status" example:"active" enums:"active,suspended"`
	// 是否封禁
	Banned optional.Optional[bool] `json:"banned" example:"false"`
	// 封禁原因
	BanReason optional.Optional[string] `json:"banReason" example:"违规发布广告"`
	// 封禁到期时间 (ISO 8601 UTC)
	BanExpires optional.Optional[time.Time] `json:"banExpires" example:"2026-10-01T00:00:00Z"`
}

// fieldSet 把请求中的档案字段投影成共有的字段集。
// 管理端请求比档案字段集多出角色、状态与封禁字段，因此必须显式构造，无法像自助请求那样直接转换。
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
	Role       *string    `json:"role" validate:"omitempty,oneof=user admin"`
	Status     *string    `json:"status" validate:"omitempty,oneof=active inactive banned"`
	BanReason  *string    `json:"banReason" validate:"omitempty,max=500"`
	BanExpires *time.Time `json:"banExpires"`
}

// ListUsersQuery 是 GET /v1/admin/users 的查询参数。
type ListUsersQuery struct {
	Page     int    `form:"page" json:"page" validate:"omitempty,min=1"`
	PageSize int    `form:"pageSize" json:"pageSize" validate:"omitempty,min=1,max=100"`
	Sort     string `form:"sort" json:"sort" validate:"omitempty,usersort,max=120"`
	Search   string `form:"search" json:"search" validate:"omitempty,max=120"`
	Status   string `form:"status" json:"status" validate:"omitempty,oneof=active inactive banned"`
	Role     string `form:"role" json:"role" validate:"omitempty,oneof=user admin"`
}

// User 是管理视角的用户视图。
//
// 它在共有的档案视图之上追加封禁与登录审计字段：管理端要看的比前端多，
// 但两者描述的是同一条记录的同几个列，因此档案部分共用而非各写一份，见 ADR-0005。
type User struct {
	user.Profile

	Banned          bool       `json:"banned"`
	BanReason       *string    `json:"banReason"`
	BanExpires      *time.Time `json:"banExpires"`
	LastLoginAt     *time.Time `json:"lastLoginAt"`
	LastLoginMethod *string    `json:"lastLoginMethod"`
}
