package mailer

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"bgin/internal/platform/errs"
)

// MemoryMailer 纯内存邮件发送器，专用于脱机单元测试与发送行为断言。
type MemoryMailer struct {
	mu       sync.RWMutex
	messages []Message
	closed   bool
}

var _ Mailer = (*MemoryMailer)(nil)

// NewMemoryMailer 创建全新的内存邮件发送器。
func NewMemoryMailer() *MemoryMailer {
	return &MemoryMailer{
		messages: make([]Message, 0),
	}
}

// Send 记录单封邮件至内存消息列表。
func (m *MemoryMailer) Send(ctx context.Context, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errors.New("mailer: memory mailer is closed")
	}

	if strings.TrimSpace(msg.To) == "" {
		return errs.BadRequest("email recipient 'To' cannot be empty")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	m.messages = append(m.messages, msg)
	return nil
}

// SendMailable 渲染结构化模板邮件并存入内存消息列表。
func (m *MemoryMailer) SendMailable(ctx context.Context, to string, mailable Mailable) error {
	return sendMailable(ctx, m, to, mailable)
}

// Close 标记邮件器为关闭状态。
func (m *MemoryMailer) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// SentMessages 获取已发送的所有邮件副本。
func (m *MemoryMailer) SentMessages() []Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.messages)
}

// LastMessage 获取最后发送的一封邮件。
func (m *MemoryMailer) LastMessage() (Message, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.messages) == 0 {
		return Message{}, false
	}
	return m.messages[len(m.messages)-1], true
}

// Count 获取已发送邮件的总数。
func (m *MemoryMailer) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.messages)
}

// Reset 清空已发送邮件记录。
func (m *MemoryMailer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = m.messages[:0]
}

// AssertSent 断言已发送邮件的数量是否符合预期。
func (m *MemoryMailer) AssertSent(tb testing.TB, expectedCount int) {
	tb.Helper()
	actual := m.Count()
	if actual != expectedCount {
		tb.Fatalf("mailer: expected %d sent messages, got %d", expectedCount, actual)
	}
}

// AssertSentTo 断言是否存在投递给目标收件人的邮件。
func (m *MemoryMailer) AssertSentTo(tb testing.TB, recipient string) {
	tb.Helper()
	msgs := m.SentMessages()
	for _, msg := range msgs {
		if msg.To == recipient || slices.Contains(msg.Cc, recipient) || slices.Contains(msg.Bcc, recipient) {
			return
		}
	}
	tb.Fatalf("mailer: expected email sent to %q, but none was found", recipient)
}
