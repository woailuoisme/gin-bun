// Package enums 定义系统跨领域通用枚举与常量。
package enums

// NotificationChannel 代表通知分发渠道枚举类型。
type NotificationChannel string

// 通知分发渠道常量定义。
const (
	// ChannelMail 邮件分发渠道。
	ChannelMail NotificationChannel = "mail"
	// ChannelDatabase 数据库/站内信通知渠道。
	ChannelDatabase NotificationChannel = "database"
	// ChannelRealtime 实时广播/WebSocket 通知渠道。
	ChannelRealtime NotificationChannel = "realtime"
	// ChannelSMS 短信通知渠道。
	ChannelSMS NotificationChannel = "sms"
	// ChannelChat 聊天/即时通讯通知渠道（如 Telegram 等）。
	ChannelChat NotificationChannel = "chat"
)

// String 返回通知分发渠道字符串。
func (c NotificationChannel) String() string {
	return string(c)
}

// IsValid 检查通知渠道是否合法。
func (c NotificationChannel) IsValid() bool {
	return c == ChannelMail || c == ChannelDatabase || c == ChannelRealtime || c == ChannelSMS || c == ChannelChat
}
