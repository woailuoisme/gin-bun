// Package enums 定义系统跨领域通用枚举与常量。
package enums

// CtxKey 代表 HTTP 传输层请求上下文 (Fiber Locals) 键枚举类型。
type CtxKey string

// 请求上下文与 Locals 键常量定义。
const (
	// CtxKeyAuthUser 认证用户信息对象键。
	CtxKeyAuthUser CtxKey = "auth_user"
	// CtxKeyUser 历史兼容用户键。
	CtxKeyUser CtxKey = "user"
	// CtxKeyUserClaims JWT Claims 载荷键。
	CtxKeyUserClaims CtxKey = "user_claims"
	// CtxKeySession 用户会话凭据键。
	CtxKeySession CtxKey = "session"
	// CtxKeyAuthContext 聚合认证上下文键。
	CtxKeyAuthContext CtxKey = "auth_context"
	// CtxKeyUserID 当前登录用户 ID 键。
	CtxKeyUserID CtxKey = "user_id"
	// CtxKeyGuard 当前路由守卫名称键。
	CtxKeyGuard CtxKey = "guard"
	// CtxKeyUserGuard 历史兼容用户守卫键。
	CtxKeyUserGuard CtxKey = "user_guard"
	// CtxKeyStartTime 请求开始时间戳键。
	CtxKeyStartTime CtxKey = "start_time"
	// CtxKeyOopsError 捕获的结构化 Oops 错误键。
	CtxKeyOopsError CtxKey = "oops_error"
)

// String 返回上下文键字符串表现形式。
func (k CtxKey) String() string {
	return string(k)
}
