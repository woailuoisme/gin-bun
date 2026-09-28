package mailer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/lo"
	"github.com/samber/oops"
	gomail "github.com/wneessen/go-mail"

	"bgin/internal/config"
	"bgin/internal/platform/errs"
)

// mailpitMailer 基于 wneessen/go-mail 实现的 Mailpit 本地测试发信器。
type mailpitMailer struct {
	cfg    config.MailConfig
	logger zerolog.Logger
	client *gomail.Client
}

// newMailpitMailer 创建 Mailpit 发信器实例。
func newMailpitMailer(cfg config.MailConfig, log zerolog.Logger) (*mailpitMailer, error) {
	host := cmp.Or(strings.TrimSpace(cfg.Mailpit.Host), "127.0.0.1")
	port := cmp.Or(cfg.Mailpit.Port, 1025)

	opts := []gomail.Option{
		gomail.WithPort(port),
		gomail.WithTimeout(10 * time.Second),
		gomail.WithSMTPAuth(gomail.SMTPAuthNoAuth),
		gomail.WithTLSPolicy(gomail.NoTLS),
	}

	client, err := gomail.NewClient(host, opts...)
	if err != nil {
		log.Error().Err(err).Str("host", host).Int("port", port).Msg("failed to initialize mailpit client")
		return nil, oops.Code(string(errs.CodeInternal)).Wrap(fmt.Errorf("mailpit: create client failed: %w", err))
	}

	return &mailpitMailer{
		cfg:    cfg,
		logger: log,
		client: client,
	}, nil
}

// Send 发送单封邮件消息。
func (m *mailpitMailer) Send(ctx context.Context, msg Message) error {
	if strings.TrimSpace(msg.To) == "" {
		return errs.BadRequest("email recipient 'To' cannot be empty")
	}

	if m.client == nil {
		return errs.Internal(errors.New("mailpit client is not initialized"))
	}

	emailMsg, err := m.buildMessage(msg)
	if err != nil {
		return err
	}

	if err := m.client.DialAndSendWithContext(ctx, emailMsg); err != nil {
		m.logger.Error().Err(err).Str("to", maskRecipient(msg.To)).Str("subject", msg.Subject).Msg("mailpit email delivery failed")
		return oops.Code(string(errs.CodeInternal)).Wrap(fmt.Errorf("mailpit: send email failed: %w", err))
	}

	m.logger.Debug().Str("to", maskRecipient(msg.To)).Str("subject", msg.Subject).Msg("mailpit email delivered successfully")
	return nil
}

// SendMailable 将 Mailable 实体渲染为邮件消息并发送。
func (m *mailpitMailer) SendMailable(ctx context.Context, to string, mailable Mailable) error {
	return sendMailable(ctx, m, to, mailable)
}

// Close 释放底层的 SMTP 连接池资源。
func (m *mailpitMailer) Close() error {
	if m.client != nil {
		return m.client.Close()
	}
	return nil
}

// buildMessage 将 Message 结构转换为 gomail.Msg 实例。
func (m *mailpitMailer) buildMessage(msg Message) (*gomail.Msg, error) {
	emailMsg := gomail.NewMsg()

	if err := emailMsg.FromFormat(m.cfg.FromName, m.cfg.FromAddress); err != nil {
		return nil, oops.Code(string(errs.CodeInternal)).Wrap(fmt.Errorf("mailpit: set from address failed: %w", err))
	}

	if err := emailMsg.To(msg.To); err != nil {
		return nil, oops.Code(string(errs.CodeBadRequest)).Wrap(fmt.Errorf("mailpit: set to address failed: %w", err))
	}

	if len(msg.Cc) > 0 {
		if err := emailMsg.Cc(msg.Cc...); err != nil {
			return nil, oops.Code(string(errs.CodeBadRequest)).Wrap(fmt.Errorf("mailpit: set cc address failed: %w", err))
		}
	}

	if len(msg.Bcc) > 0 {
		if err := emailMsg.Bcc(msg.Bcc...); err != nil {
			return nil, oops.Code(string(errs.CodeBadRequest)).Wrap(fmt.Errorf("mailpit: set bcc address failed: %w", err))
		}
	}

	emailMsg.Subject(msg.Subject)
	bodyType := lo.Ternary(msg.IsHTML, gomail.TypeTextHTML, gomail.TypeTextPlain)
	emailMsg.SetBodyString(bodyType, msg.Body)

	for _, file := range msg.Attachments {
		if strings.TrimSpace(file) != "" {
			emailMsg.AttachFile(file)
		}
	}

	return emailMsg, nil
}
