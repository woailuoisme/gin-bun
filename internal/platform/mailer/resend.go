package mailer

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/rs/zerolog"
	"github.com/samber/oops"

	"bgin/internal/config"
	"bgin/internal/platform/errs"
)

// resendMailer 基于 Resend HTTP REST API 实现的现代化邮件发送器。
type resendMailer struct {
	cfg     config.MailConfig
	logger  zerolog.Logger
	client  *resty.Client
	baseURL string
}

// resendEmailRequest 定义 Resend 发送邮件请求体。
type resendEmailRequest struct {
	From        string             `json:"from"`
	To          []string           `json:"to"`
	Subject     string             `json:"subject"`
	HTML        string             `json:"html,omitempty"`
	Text        string             `json:"text,omitempty"`
	Cc          []string           `json:"cc,omitempty"`
	Bcc         []string           `json:"bcc,omitempty"`
	Attachments []resendAttachment `json:"attachments,omitempty"`
}

// resendAttachment 定义 Resend 附件结构。
type resendAttachment struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

// resendErrorResponse 定义 Resend 错误响应体。
type resendErrorResponse struct {
	StatusCode int    `json:"statusCode"`
	Name       string `json:"name"`
	Message    string `json:"message"`
}

// newResendMailer 创建基于 Resend API 的邮件发送器实例。
func newResendMailer(cfg config.MailConfig, log zerolog.Logger) (*resendMailer, error) {
	apiKey := strings.TrimSpace(cfg.Resend.APIKey)
	if apiKey == "" {
		return nil, oops.Code(string(errs.CodeInternal)).Wrap(errors.New("resend: api key cannot be empty"))
	}

	trimmedURL := strings.TrimRight(cmp.Or(strings.TrimSpace(cfg.Resend.BaseURL), "https://api.resend.com"), "/")

	client := resty.New().
		SetTimeout(15*time.Second).
		SetBaseURL(trimmedURL).
		SetHeader("User-Agent", "bgin-mailer/1.0").
		SetAuthToken(apiKey)

	return &resendMailer{
		cfg:     cfg,
		logger:  log,
		client:  client,
		baseURL: trimmedURL,
	}, nil
}

// Send 发送单封邮件。
func (m *resendMailer) Send(ctx context.Context, msg Message) error {
	if strings.TrimSpace(msg.To) == "" {
		return errs.BadRequest("email recipient 'To' cannot be empty")
	}

	reqPayload, err := m.buildPayload(msg)
	if err != nil {
		return err
	}

	var errResp resendErrorResponse
	resp, err := m.client.R().
		SetContext(ctx).
		SetBody(reqPayload).
		SetError(&errResp).
		Post("/emails")
	if err != nil {
		m.logger.Error().Err(err).Str("to", maskRecipient(msg.To)).Str("subject", msg.Subject).Msg("resend network request failed")
		return oops.Code(string(errs.CodeServiceUnavailable)).
			With("to", maskRecipient(msg.To)).
			Wrap(fmt.Errorf("resend: request failed: %w", err))
	}

	if resp.IsSuccess() {
		m.logger.Debug().Str("to", maskRecipient(msg.To)).Str("subject", msg.Subject).Msg("resend email sent successfully")
		return nil
	}

	if errResp.Message != "" {
		m.logger.Warn().
			Int("status", resp.StatusCode()).
			Str("error", errResp.Message).
			Str("to", maskRecipient(msg.To)).
			Msg("resend server returned error response")

		return oops.Code(string(errs.CodeBadRequest)).
			With("status", resp.StatusCode()).
			With("resend_error", errResp.Message).
			Wrap(fmt.Errorf("resend: send failed: %s", errResp.Message))
	}

	m.logger.Error().
		Int("status", resp.StatusCode()).
		Str("body", resp.String()).
		Msg("resend server returned unexpected status code")

	return oops.Code(string(errs.CodeServiceUnavailable)).
		With("status", resp.StatusCode()).
		Wrap(fmt.Errorf("resend: unexpected status code %d", resp.StatusCode()))
}

// SendMailable 渲染模板并发送结构化邮件。
func (m *resendMailer) SendMailable(ctx context.Context, to string, mailable Mailable) error {
	return sendMailable(ctx, m, to, mailable)
}

// Close 释放资源（HTTP 客户端无持久连接池，直接返回 nil）。
func (m *resendMailer) Close() error {
	return nil
}

// buildPayload 组装 Resend 邮件发送请求体。
func (m *resendMailer) buildPayload(msg Message) (resendEmailRequest, error) {
	from := cmp.Or(strings.TrimSpace(m.cfg.FromAddress), "onboarding@resend.dev")
	if m.cfg.FromName != "" {
		from = fmt.Sprintf("%s <%s>", m.cfg.FromName, from)
	}

	reqPayload := resendEmailRequest{
		From:    from,
		To:      []string{msg.To},
		Subject: msg.Subject,
		Cc:      msg.Cc,
		Bcc:     msg.Bcc,
	}

	if msg.IsHTML {
		reqPayload.HTML = msg.Body
	} else {
		reqPayload.Text = msg.Body
	}

	if len(msg.Attachments) > 0 {
		attachments, err := encodeAttachments(msg.Attachments)
		if err != nil {
			return reqPayload, err
		}
		reqPayload.Attachments = attachments
	}

	return reqPayload, nil
}

// encodeAttachments 读取并对邮件附件执行 Base64 编码。
func encodeAttachments(paths []string) ([]resendAttachment, error) {
	attachments := make([]resendAttachment, 0, len(paths))
	for _, attPath := range paths {
		data, err := os.ReadFile(attPath)
		if err != nil {
			return nil, oops.Code(string(errs.CodeBadRequest)).Wrap(fmt.Errorf("resend: read attachment failed: %w", err))
		}
		attachments = append(attachments, resendAttachment{
			Filename: filepath.Base(attPath),
			Content:  base64.StdEncoding.EncodeToString(data),
		})
	}
	return attachments, nil
}
