package adminauth

// SignInEmailRequest 是 POST /v1/admin/auth/sign-in/email 的请求体。
type SignInEmailRequest struct {
	// 管理员电子邮箱
	Email string `json:"email" example:"admin@example.com" validate:"required,email"`
	// 管理员登录口令
	Password string `json:"password" example:"Admin123!" validate:"required"`
}

// RefreshTokenRequest 是 POST /v1/admin/auth/token/refresh 的请求体。
type RefreshTokenRequest struct {
	// 刷新令牌凭据
	RefreshToken string `json:"refreshToken" example:"rt_01J9X4N8Y0EFGH" validate:"required"`
}

// SignOutRequest 是 POST /v1/admin/auth/sign-out 的请求体。
type SignOutRequest struct {
	// 待作废的刷新令牌 (可选，同时作废可真正阻断续签)
	RefreshToken *string `json:"refreshToken" example:"rt_01J9X4N8Y0EFGH" validate:"omitempty"`
}

// ChangePasswordRequest 是 POST /v1/admin/auth/change-password 的请求体。
type ChangePasswordRequest struct {
	// 当前登录口令
	CurrentPassword string `json:"currentPassword" example:"Admin123!" validate:"required"`
	// 新登录口令 (8-128位，须含字母与数字)
	NewPassword string `json:"newPassword" example:"NewAdmin456!" validate:"required,min=8,max=128"`
}
