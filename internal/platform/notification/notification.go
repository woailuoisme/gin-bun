// Package notification 提供统一、模块化、高可扩展的多渠道通知基础设施。
package notification

import (
	"cmp"
	"context"
	"strings"
	"sync"

	"github.com/uptrace/bun"

	"bgin/internal/config"
	"bgin/internal/platform/mailer"
	chatChannel "bgin/internal/platform/notification/channel/chat"
	telegramDriver "bgin/internal/platform/notification/channel/chat/telegram"
	dbChannel "bgin/internal/platform/notification/channel/database"
	mailChannel "bgin/internal/platform/notification/channel/mail"
	rtChannel "bgin/internal/platform/notification/channel/realtime"
	smsChannel "bgin/internal/platform/notification/channel/sms"
	aliyunSMS "bgin/internal/platform/notification/channel/sms/aliyun"
	logSMS "bgin/internal/platform/notification/channel/sms/log"
	mockSMS "bgin/internal/platform/notification/channel/sms/mock"
	smsbaoSMS "bgin/internal/platform/notification/channel/sms/smsbao"
	tencentSMS "bgin/internal/platform/notification/channel/sms/tencent"
	"bgin/internal/platform/realtime"
	"bgin/internal/platform/txhook"
)

var (
	defaultNotifier *Notifier
	notifierMu      sync.RWMutex
)

// SetDefaultManager 设置全局默认通知管理器。
func SetDefaultManager(n *Notifier) {
	notifierMu.Lock()
	defer notifierMu.Unlock()
	defaultNotifier = n
}

// DefaultManager 返回全局默认通知管理器。
func DefaultManager() *Notifier {
	notifierMu.RLock()
	defer notifierMu.RUnlock()
	return defaultNotifier
}

// New 根据配置与基础依赖构建统一多渠道通知引擎实例。
func New(cfg config.NotificationConfig, db *bun.DB, m mailer.Mailer, rt realtime.Manager) (*Notifier, error) {
	poolSize := cmp.Or(cfg.WorkerPool, 10)
	queueSize := cmp.Or(cfg.QueueSize, 100)

	n := NewNotifier(poolSize, queueSize)

	// 1. 邮件渠道 (mail)
	if m != nil {
		n.Extend("mail", mailChannel.NewChannel(m))
	}

	// 2. 数据库渠道 (database)
	if db != nil {
		n.Extend("database", dbChannel.NewChannel(db))
	}

	// 3. 实时广播渠道 (realtime)
	if rt != nil {
		n.Extend("realtime", rtChannel.NewChannel(rt))
	}

	// 4. 短信渠道 (sms)
	smsDriver := cmp.Or(strings.ToLower(strings.TrimSpace(cfg.SMS.Driver)), "log")
	smsCh := smsChannel.NewChannel(smsDriver)
	smsCh.RegisterDriver("log", logSMS.NewDriver())
	smsCh.RegisterDriver("mock", mockSMS.NewDriver())

	if cfg.SMS.Aliyun.AccessKeyID != "" && cfg.SMS.Aliyun.AccessKeySecret != "" {
		aliDriver, err := aliyunSMS.NewDriver(aliyunSMS.Config{
			AccessKeyID:     cfg.SMS.Aliyun.AccessKeyID,
			AccessKeySecret: cfg.SMS.Aliyun.AccessKeySecret,
			SignName:        cmp.Or(cfg.SMS.Aliyun.SignName, cfg.SMS.DefaultSignName),
			TemplateCode:    cfg.SMS.Aliyun.TemplateCode,
		})
		if err == nil {
			smsCh.RegisterDriver("aliyun", aliDriver)
		}
	}

	if cfg.SMS.Tencent.SecretID != "" && cfg.SMS.Tencent.SecretKey != "" {
		tcDriver, err := tencentSMS.NewDriver(tencentSMS.Config{
			SecretID:   cfg.SMS.Tencent.SecretID,
			SecretKey:  cfg.SMS.Tencent.SecretKey,
			AppID:      cfg.SMS.Tencent.AppID,
			SignName:   cmp.Or(cfg.SMS.Tencent.SignName, cfg.SMS.DefaultSignName),
			TemplateID: cfg.SMS.Tencent.TemplateID,
			Region:     cfg.SMS.Tencent.Region,
		})
		if err == nil {
			smsCh.RegisterDriver("tencent", tcDriver)
		}
	}

	if cfg.SMS.SMSBao.Username != "" && cfg.SMS.SMSBao.APIKey != "" {
		smsbaoDriver, err := smsbaoSMS.NewDriver(smsbaoSMS.Config{
			Username: cfg.SMS.SMSBao.Username,
			APIKey:   cfg.SMS.SMSBao.APIKey,
		})
		if err == nil {
			smsCh.RegisterDriver("smsbao", smsbaoDriver)
		}
	}

	n.Extend("sms", smsCh)

	// 5. 即时通讯渠道 (chat - telegram)
	chatCh := chatChannel.NewChannel("telegram")
	if cfg.Telegram.BotToken != "" {
		tgDriver, err := telegramDriver.NewDriver(telegramDriver.Config{
			BotToken: cfg.Telegram.BotToken,
			ChatID:   cfg.Telegram.ChatID,
			BaseURL:  cfg.Telegram.APIURL,
		})
		if err == nil {
			chatCh.RegisterDriver("telegram", tgDriver)
		}
	}
	n.Extend("chat", chatCh)

	SetDefaultManager(n)
	return n, nil
}

// Send 使用默认管理器向指定接收者发送通知。
func Send(ctx context.Context, notifiables any, notif Notification, opts ...SendOption) error {
	if mgr := DefaultManager(); mgr != nil {
		return mgr.Send(ctx, notifiables, notif, opts...)
	}
	return nil
}

// SendAfterCommit 在当前数据库事务成功提交后投递通知。
func SendAfterCommit(ctx context.Context, notifiables any, notif Notification, opts ...SendOption) {
	txhook.AfterCommit(ctx, func() {
		_ = Send(ctx, notifiables, notif, opts...)
	})
}

// SendParallel 使用默认管理器并发发送通知。
func SendParallel(ctx context.Context, notifiables any, notif Notification, opts ...SendOption) error {
	if mgr := DefaultManager(); mgr != nil {
		return mgr.SendParallel(ctx, notifiables, notif, opts...)
	}
	return nil
}

// SendParallelAfterCommit 在当前数据库事务成功提交后并发投递通知。
func SendParallelAfterCommit(ctx context.Context, notifiables any, notif Notification, opts ...SendOption) {
	txhook.AfterCommit(ctx, func() {
		_ = SendParallel(ctx, notifiables, notif, opts...)
	})
}

// NewNotificationManager 为向后兼容提供构造函数。
func NewNotificationManager(m mailer.Mailer) *Notifier {
	n := NewNotifier(10, 100)
	if m != nil {
		n.Extend("mail", mailChannel.NewChannel(m))
	}
	return n
}

// NewMailChannel 创建邮件通知渠道（直接注入底层 Mailer）。
func NewMailChannel(m mailer.Mailer) *mailChannel.Channel {
	return mailChannel.NewChannel(m)
}

// NewRealtimeChannel 创建实时广播通知渠道。
func NewRealtimeChannel(rt realtime.Manager) *rtChannel.Channel {
	return rtChannel.NewChannel(rt)
}

// NewDatabaseChannel 创建数据库通知渠道。
func NewDatabaseChannel(db *bun.DB) *dbChannel.Channel {
	return dbChannel.NewChannel(db)
}

// NewSMSChannel 创建短信通知渠道。
func NewSMSChannel(defaultDriver string) *smsChannel.Channel {
	return smsChannel.NewChannel(defaultDriver)
}

// NewChatChannel 创建即时通讯通知渠道。
func NewChatChannel(defaultDriver string) *chatChannel.Channel {
	return chatChannel.NewChannel(defaultDriver)
}

// 类型别名以保持与现有业务领域的无缝兼容。
type (
	// RealtimeNotification 实时广播通知接口。
	RealtimeNotification = rtChannel.Notification
	// RealtimeMessage 实时广播消息载荷。
	RealtimeMessage = rtChannel.Message

	// DatabaseNotification 数据库持久化通知接口。
	DatabaseNotification = dbChannel.Notification
	// DatabaseMessage 数据库持久化消息载荷。
	DatabaseMessage = dbChannel.Message

	// MailNotification 邮件通知接口。
	MailNotification = mailChannel.Notification
	// MailMessage 邮件通知消息载荷。
	MailMessage = mailChannel.Message

	// SMSNotification 短信通知接口。
	SMSNotification = smsChannel.Notification
	// SMSMessage 短信通知消息载荷。
	SMSMessage = smsChannel.Message

	// ChatNotification 即时通讯通知接口。
	ChatNotification = chatChannel.Notification
	// ChatMessage 即时通讯通知消息载荷。
	ChatMessage = chatChannel.Message
)

// NewSMSMessage 创建短信载荷构建器。
func NewSMSMessage() *SMSMessage {
	return smsChannel.NewMessage()
}

// NewMailMessage 创建邮件载荷构建器。
func NewMailMessage() *MailMessage {
	return mailChannel.NewMessage()
}

// NewChatMessage 创建即时通讯载荷构建器。
func NewChatMessage() *ChatMessage {
	return chatChannel.NewMessage()
}
