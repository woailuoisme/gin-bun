package adminauth

import (
	"bgin/internal/shared/user"
)

// Profile 是共享内核档案视图的类型别名，避免 swag 跨包解析失效。
type Profile = user.Profile

// SessionResponse 是签发令牌类端点的统一响应体。
type SessionResponse struct {
	// 访问令牌 (JWT Access Token)
	Token string `json:"token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
	// 刷新令牌 (Refresh Token)
	RefreshToken string `json:"refreshToken" example:"rt_01J9X4N8Y0EFGH"`
	// 访问令牌有效期（秒）
	ExpiresIn int `json:"expiresIn" example:"900"`
	// 管理员身份资料投影
	User Profile `json:"user"`
}

// GetSessionResponse 是 GET /v1/admin/auth/get-session 的响应体。
type GetSessionResponse struct {
	// 当前登录管理员身份资料投影
	User Profile `json:"user"`
}
