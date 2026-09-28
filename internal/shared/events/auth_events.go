// Package events 声明跨业务领域切片的强类型事件载荷契约。
// 所有事件定义必须为纯 DTO，严禁携带外部 I/O 驱动或重量级业务逻辑，避免 Feature 间横向依赖。
package events

import "time"

// UserRegistered 在新用户完成基本注册流程后向全系统广播。
type UserRegistered struct {
	UserID       string    `json:"user_id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	RegisteredAt time.Time `json:"registered_at"`
}

// PasswordResetRequested 在用户请求邮箱密码重置验证码时向全系统广播。
type PasswordResetRequested struct {
	Email     string        `json:"email"`
	Code      string        `json:"code"`
	ExpiresIn time.Duration `json:"expires_in"`
}

// PasswordResetRequestedByPhone 在用户通过手机短信请求密码重置验证码时向全系统广播。
type PasswordResetRequestedByPhone struct {
	PhoneNumber string        `json:"phone_number"`
	Code        string        `json:"code"`
	ExpiresIn   time.Duration `json:"expires_in"`
}

// PasswordChanged 在用户登录口令修改或重置成功后广播，用于撤销旧凭据与安全审计。
type PasswordChanged struct {
	UserID    string    `json:"user_id"`
	ChangedAt time.Time `json:"changed_at"`
}
