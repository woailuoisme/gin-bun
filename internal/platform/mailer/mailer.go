// Package mailer 通过 SMTP 投递邮件。
// 本地默认指向 Mailpit（无需认证与 TLS）；带 userinfo 的地址会启用 PLAIN 认证与强制 STARTTLS。
package mailer

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/wneessen/go-mail"

	"bgin/internal/config"
)

// defaultTimeout 避免 SMTP 服务器无响应时把请求挂死。
const defaultTimeout = 10 * time.Second

// Message 是一封待投递的纯文本邮件。
type Message struct {
	To      string
	Subject string
	Body    string
}

// Mailer 负责投递邮件。
type Mailer struct {
	client      *mail.Client
	fromAddress string
	fromName    string
}

// New 按 MAIL_URL 构造 Mailer。目前只支持 smtp:// 方案，smtps:// 需显式改造以免误配。
func New(cfg config.MailConfig) (*Mailer, error) {
	parsed, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("mailer: parse MAIL_URL failed: %w", err)
	}
	if parsed.Scheme != "smtp" {
		return nil, fmt.Errorf("mailer: scheme must be smtp, got %q", parsed.Scheme)
	}

	port := 25
	if parsed.Port() != "" {
		port, err = strconv.Atoi(parsed.Port())
		if err != nil {
			return nil, fmt.Errorf("mailer: parse port failed: %w", err)
		}
	}

	options := []mail.Option{
		mail.WithPort(port),
		mail.WithTimeout(defaultTimeout),
	}
	if parsed.User != nil {
		password, _ := parsed.User.Password()
		options = append(options,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(parsed.User.Username()),
			mail.WithPassword(password),
			mail.WithTLSPolicy(mail.TLSMandatory),
		)
	} else {
		options = append(options,
			mail.WithSMTPAuth(mail.SMTPAuthNoAuth),
			mail.WithTLSPolicy(mail.NoTLS),
		)
	}

	client, err := mail.NewClient(parsed.Hostname(), options...)
	if err != nil {
		return nil, fmt.Errorf("mailer: new client failed: %w", err)
	}

	return &Mailer{
		client:      client,
		fromAddress: cfg.FromAddress,
		fromName:    cfg.FromName,
	}, nil
}

// Send 投递一封纯文本邮件。
func (m *Mailer) Send(ctx context.Context, msg Message) error {
	message := mail.NewMsg()
	if err := message.FromFormat(m.fromName, m.fromAddress); err != nil {
		return fmt.Errorf("mailer: set sender failed: %w", err)
	}
	if err := message.To(msg.To); err != nil {
		return fmt.Errorf("mailer: set recipient %s failed: %w", msg.To, err)
	}

	message.Subject(msg.Subject)
	message.SetBodyString(mail.TypeTextPlain, msg.Body)

	if err := m.client.DialAndSendWithContext(ctx, message); err != nil {
		return fmt.Errorf("mailer: send email failed: %w", err)
	}
	return nil
}

// Close 释放 SMTP 连接。
func (m *Mailer) Close() error {
	if err := m.client.Close(); err != nil {
		return fmt.Errorf("mailer: close connection failed: %w", err)
	}
	return nil
}
