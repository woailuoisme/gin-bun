// Package log 提供用于开发调试环境的日志型短信驱动实现。
package log

import (
	"context"

	"github.com/rs/zerolog"

	"bgin/internal/platform/logger"
	"bgin/internal/platform/notification/channel/sms"
)

// Driver 结构化日志短信驱动。
type Driver struct {
	logger zerolog.Logger
}

// NewDriver 创建日志短信驱动实例。
func NewDriver(l ...zerolog.Logger) *Driver {
	var lg zerolog.Logger
	if len(l) > 0 {
		lg = l[0]
	} else {
		lg = *logger.FromContext(context.Background())
	}
	return &Driver{
		logger: lg.With().Str("component", "sms_log_driver").Logger(),
	}
}

// Name 返回驱动标识名。
func (d *Driver) Name() string {
	return "log"
}

// Send 记录短信发送的结构化日志。
func (d *Driver) Send(_ context.Context, msg *sms.Message) error {
	evt := d.logger.Info().
		Str("phone", msg.PhoneNumber).
		Str("sign_name", msg.SignName).
		Str("template_code", msg.TemplateCode)

	if len(msg.TemplateParam) > 0 {
		evt.Interface("params", msg.TemplateParam)
	}
	if msg.Content != "" {
		evt.Str("content", msg.Content)
	}

	evt.Msg("sms notification logged via log driver")
	return nil
}
