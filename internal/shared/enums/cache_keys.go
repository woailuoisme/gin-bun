// Package enums 定义系统跨领域通用枚举与常量。
package enums

// CachePrefix 代表集中式缓存键前缀枚举类型。
type CachePrefix string

// 缓存键前缀常量定义。
const (
	// CachePrefixRefreshToken 刷新令牌缓存键前缀。
	CachePrefixRefreshToken CachePrefix = "refresh_token:"
	// CachePrefixBlacklist JWT 令牌黑名单缓存键前缀。
	CachePrefixBlacklist CachePrefix = "blacklist:"
	// CachePrefixRateLimit API 速率限制计数器前缀。
	CachePrefixRateLimit CachePrefix = "rate_limit:"
	// CachePrefixUserProfile 用户详情数据缓存前缀。
	CachePrefixUserProfile CachePrefix = "user_profile:"
	// CachePrefixTOTPReplay TOTP 动态码一次性消费（防重放）键前缀。
	CachePrefixTOTPReplay CachePrefix = "totp_replay:"
)

// String 返回缓存前缀字符串。
func (p CachePrefix) String() string {
	return string(p)
}

// Key 将前缀与具体业务唯一标识符组合拼接为完整的缓存键名。
func (p CachePrefix) Key(identifier string) string {
	return string(p) + identifier
}
