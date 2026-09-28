// Package enums 定义系统跨领域通用枚举与常量。
package enums

// GuardName 代表认证守卫领域枚举类型。
type GuardName string

// 认证守卫常量定义。
const (
	// GuardWeb 面向 Web 浏览器的认证守卫。
	GuardWeb GuardName = "web"
	// GuardAPI 面向客户端 C 端用户的认证守卫。
	GuardAPI GuardName = "api"
	// GuardAdmin 面向管理后台 Admin 用户的认证守卫。
	GuardAdmin GuardName = "admin"
)

// String 返回认证守卫的字符串表现形式。
func (g GuardName) String() string {
	return string(g)
}

// IsValid 检查守卫名称是否合法。
func (g GuardName) IsValid() bool {
	return g == GuardAPI || g == GuardAdmin || g == GuardWeb
}

// GuardDriver 代表认证守卫的底层驱动枚举类型。
type GuardDriver string

// 认证守卫驱动常量定义。
const (
	// DriverJWT 基于 JWT 令牌的认证驱动。
	DriverJWT GuardDriver = "jwt"
	// DriverSession 基于持久化会话的认证驱动。
	DriverSession GuardDriver = "session"
)

// GuardProviderName 代表认证守卫所绑定的用户提供者名称枚举类型。
type GuardProviderName string

// 用户提供者名称常量定义。
const (
	// ProviderUsers 普通用户提供者。
	ProviderUsers GuardProviderName = "users"
	// ProviderAdminUsers 管理端管理员用户提供者。
	ProviderAdminUsers GuardProviderName = "admin_users"
)
