package user

import (
	"time"

	"bgin/internal/database/model"
)

// Profile 是用户档案的响应视图，面向前端自助与后台管理两个交付面共用。
//
// 这里刻意只共用"输出"：两个域描述的是同一条记录的同几个列，各写一份必然漂移。
// 差异主要在输入侧——谁能改哪些字段由各自的请求体约束，而不在视图上体现。
// 这与 ADR-0002 早期"两份视图各自演进"的设想相反，理由见 ADR-0005。
type Profile struct {
	// 用户唯一标识
	ID string `json:"id" example:"usr_01J9X4N8Y0ABCD"`
	// 用户展示昵称
	Name string `json:"name" example:"Alex Mercer"`
	// 用户电子邮箱
	Email string `json:"email" example:"alex@example.com"`
	// 邮箱是否已验证
	EmailVerified bool `json:"emailVerified" example:"true"`
	// 用户头像 URL
	Image *string `json:"image" example:"https://example.com/avatar.png"`
	// 用户权限角色 (user: 普通用户, admin: 系统管理员)
	Role string `json:"role" example:"user" enums:"user,admin"`
	// 账号状态 (active: 正常活跃, suspended: 封禁停用)
	Status string `json:"status" example:"active" enums:"active,suspended"`
	// 唯一登录用户名
	Username *string `json:"username" example:"alexmercer"`
	// 用户显示名称
	DisplayUsername *string `json:"displayUsername" example:"Alex Mercer"`
	// E.164 格式手机号
	PhoneNumber *string `json:"phoneNumber" example:"+8613800138000"`
	// 账号创建时间 (ISO 8601 UTC)
	CreatedAt time.Time `json:"createdAt" example:"2026-09-27T10:00:00Z"`
	// 账号更新时间 (ISO 8601 UTC)
	UpdatedAt time.Time `json:"updatedAt" example:"2026-09-27T10:00:00Z"`
}

// ProjectProfile 把用户模型投影为档案视图。
func ProjectProfile(entity *model.User) Profile {
	return Profile{
		ID:              entity.ID,
		Name:            entity.Name,
		Email:           entity.Email,
		EmailVerified:   entity.EmailVerified,
		Image:           entity.Image,
		Role:            entity.Role,
		Status:          entity.Status,
		Username:        entity.Username,
		DisplayUsername: entity.DisplayUsername,
		PhoneNumber:     entity.PhoneNumber,
		// 归一到 UTC：pgx 解出的 time.Local 会让响应体带出服务器本地偏移。
		CreatedAt: entity.CreatedAt.UTC(),
		UpdatedAt: entity.UpdatedAt.UTC(),
	}
}
