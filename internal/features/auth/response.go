package auth

import (
	"time"

	"bgin/internal/models"
)

// UserProjection 是认证端点返回的用户档案投影。
type UserProjection struct {
	// 用户唯一标识
	ID string `json:"id" example:"usr_01J9X4N8Y0ABCD"`
	// 用户展示昵称
	Name string `json:"name" example:"Alex Mercer"`
	// 用户电子邮箱
	Email string `json:"email" example:"alex@example.com"`
	// 邮箱是否已完成验证
	EmailVerified bool `json:"emailVerified" example:"true"`
	// 用户头像 URL
	Image *string `json:"image" example:"https://example.com/avatar.png"`
	// 用户权限角色 (user: 普通用户, admin: 系统管理员)
	Role string `json:"role" example:"user" enums:"user,admin"`
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

// SessionResponse 是签发令牌类端点的统一响应体。
type SessionResponse struct {
	// 访问令牌 (JWT Access Token)
	Token string `json:"token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
	// 刷新令牌 (Refresh Token)
	RefreshToken string `json:"refreshToken" example:"rt_01J9X4N8Y0EFGH"`
	// 访问令牌有效期（秒）
	ExpiresIn int `json:"expiresIn" example:"900"`
	// 用户身份资料投影
	User UserProjection `json:"user"`
}

// GetSessionResponse 是 GET /v1/auth/get-session 的响应体。
type GetSessionResponse struct {
	// 当前登录用户身份资料投影
	User UserProjection `json:"user"`
}

// StatusResponse 是通用布尔状态响应体。
type StatusResponse struct {
	// 请求是否已成功受理并处理完毕
	Status bool `json:"status" example:"true"`
}

// projectUser 把用户模型投影为认证端点的响应体。
func projectUser(user *models.User) UserProjection {
	return UserProjection{
		ID:              user.ID,
		Name:            user.Name,
		Email:           user.Email,
		EmailVerified:   user.EmailVerified,
		Image:           user.Image,
		Role:            user.Role,
		Username:        user.Username,
		DisplayUsername: user.DisplayUsername,
		PhoneNumber:     user.PhoneNumber,
		// 归一到 UTC：pgx 解出的 time.Local 会让响应体带出服务器本地偏移。
		CreatedAt: user.CreatedAt.UTC(),
		UpdatedAt: user.UpdatedAt.UTC(),
	}
}
