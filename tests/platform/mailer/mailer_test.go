// Package mailer_test 验证邮件服务抽象、Mailpit/Resend 驱动契约与 HTML 模板渲染。
package mailer_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/config"
	"bgin/internal/platform/mailer"
)

// testMailable 模拟带模板的结构化邮件。
type testMailable struct {
	subject string
	tmpl    string
	data    map[string]any
}

func (m *testMailable) Subject() string {
	return m.subject
}

func (m *testMailable) Template() (string, map[string]any) {
	return m.tmpl, m.data
}

func TestMailpitDriverLifecycle(t *testing.T) {
	cfg := config.MailConfig{
		Driver:      "mailpit",
		FromAddress: "no-reply@bgin.local",
		FromName:    "bgin",
		Mailpit: config.MailpitConfig{
			Host: "127.0.0.1",
			Port: 1025,
		},
	}

	client, err := mailer.New(cfg, zerolog.Nop())
	require.NoError(t, err)
	require.NotNil(t, client)

	// 验证收件人空校验
	err = client.Send(context.Background(), mailer.Message{
		To:      "",
		Subject: "Test",
		Body:    "Body",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recipient 'To' cannot be empty")

	// 验证安全关闭
	require.NoError(t, client.Close())
}

func TestResendDriverMissingAPIKey(t *testing.T) {
	cfg := config.MailConfig{
		Driver: "resend",
		Resend: config.ResendConfig{
			APIKey: "",
		},
	}

	client, err := mailer.New(cfg, zerolog.Nop())
	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "api key cannot be empty")
}

func TestResendDriverSendSuccess(t *testing.T) {
	receivedAuth := ""
	receivedAgent := ""
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedAgent = r.Header.Get("User-Agent")

		err := json.NewDecoder(r.Body).Decode(&receivedBody)
		assert.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"email_12345678"}`))
	}))
	defer server.Close()

	cfg := config.MailConfig{
		Driver:      "resend",
		FromAddress: "noreply@example.com",
		FromName:    "Shop",
		Resend: config.ResendConfig{
			APIKey:  "re_test_secret_key",
			BaseURL: server.URL,
		},
	}

	client, err := mailer.New(cfg, zerolog.Nop())
	require.NoError(t, err)
	require.NotNil(t, client)
	defer func() {
		require.NoError(t, client.Close())
	}()

	msg := mailer.Message{
		To:      "buyer@example.com",
		Cc:      []string{"cc@example.com"},
		Bcc:     []string{"audit@example.com"},
		Subject: "订单支付凭据",
		Body:    "您的订单已成功支付",
		IsHTML:  false,
	}

	err = client.Send(context.Background(), msg)
	require.NoError(t, err)

	assert.Equal(t, "Bearer re_test_secret_key", receivedAuth)
	assert.Equal(t, "bgin-mailer/1.0", receivedAgent)
	assert.Equal(t, "Shop <noreply@example.com>", receivedBody["from"])
	assert.Equal(t, []any{"buyer@example.com"}, receivedBody["to"])
	assert.Equal(t, "订单支付凭据", receivedBody["subject"])
	assert.Equal(t, "您的订单已成功支付", receivedBody["text"])
}

func TestResendDriverSendWithAttachments(t *testing.T) {
	tempDir := t.TempDir()
	invoiceFile := filepath.Join(tempDir, "invoice.pdf")
	require.NoError(t, os.WriteFile(invoiceFile, []byte("%PDF-1.4 Mock Invoice Content"), 0o600))

	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"email_with_attach"}`))
	}))
	defer server.Close()

	cfg := config.MailConfig{
		Driver: "resend",
		Resend: config.ResendConfig{
			APIKey:  "re_test_key",
			BaseURL: server.URL,
		},
	}

	client, err := mailer.New(cfg, zerolog.Nop())
	require.NoError(t, err)

	err = client.Send(context.Background(), mailer.Message{
		To:          "buyer@example.com",
		Subject:     "发票附件",
		Body:        "请查收附件",
		Attachments: []string{invoiceFile},
	})
	require.NoError(t, err)

	attachments, ok := receivedBody["attachments"].([]any)
	require.True(t, ok)
	require.Len(t, attachments, 1)

	attMap, ok := attachments[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "invoice.pdf", attMap["filename"])
	assert.NotEmpty(t, attMap["content"])
}

func TestResendDriverServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"statusCode":422,"name":"validation_error","message":"Domain not verified"}`))
	}))
	defer server.Close()

	cfg := config.MailConfig{
		Driver: "resend",
		Resend: config.ResendConfig{
			APIKey:  "re_invalid_key",
			BaseURL: server.URL,
		},
	}

	client, err := mailer.New(cfg, zerolog.Nop())
	require.NoError(t, err)

	err = client.Send(context.Background(), mailer.Message{
		To:      "buyer@example.com",
		Subject: "测试失败",
		Body:    "正文",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Domain not verified")
}

func TestSendMailableTemplateRendering(t *testing.T) {
	var receivedHTML string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if html, ok := body["html"].(string); ok {
			receivedHTML = html
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"mailable_test"}`))
	}))
	defer server.Close()

	cfg := config.MailConfig{
		Driver: "resend",
		Resend: config.ResendConfig{
			APIKey:  "re_test_key",
			BaseURL: server.URL,
		},
	}

	client, err := mailer.New(cfg, zerolog.Nop())
	require.NoError(t, err)

	mailable := &testMailable{
		subject: "欢迎加入系统",
		tmpl:    "welcome",
		data: map[string]any{
			"Name": "Alex",
			"Button": map[string]any{
				"Text": "进入系统",
				"URL":  "https://example.com/dashboard",
			},
		},
	}

	err = client.SendMailable(context.Background(), "alex@example.com", mailable)
	require.NoError(t, err)
	assert.Contains(t, receivedHTML, "Alex")
	assert.Contains(t, receivedHTML, "进入系统")
	assert.Contains(t, receivedHTML, "https://example.com/dashboard")
}

func TestUnsupportedDriver(t *testing.T) {
	cfg := config.MailConfig{
		Driver: "sendgrid",
	}

	client, err := mailer.New(cfg, zerolog.Nop())
	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "unsupported mail driver")
}

func TestConfigValidation(t *testing.T) {
	t.Run("mailpit合法端口", func(t *testing.T) {
		mailCfg := config.MailConfig{
			Driver: "mailpit",
			Mailpit: config.MailpitConfig{
				Host: "127.0.0.1",
				Port: 1025,
			},
		}
		require.NoError(t, mailCfg.Validate())
	})

	t.Run("mailpit非法端口", func(t *testing.T) {
		mailCfg := config.MailConfig{
			Driver: "mailpit",
			Mailpit: config.MailpitConfig{
				Port: 99999,
			},
		}
		err := mailCfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mailpit port must be between 1 and 65535")
	})

	t.Run("resend缺少APIKey", func(t *testing.T) {
		mailCfg := config.MailConfig{
			Driver: "resend",
			Resend: config.ResendConfig{
				APIKey: "",
			},
		}
		err := mailCfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resend api key cannot be empty")
	})
}
