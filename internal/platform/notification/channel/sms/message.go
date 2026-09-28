// Package sms 提供短消息通知渠道与多厂商驱动实现。
package sms

import "maps"

// Message 表示标准的短信通知载荷。
type Message struct {
	PhoneNumber   string
	SignName      string
	TemplateCode  string
	TemplateParam map[string]string
	Content       string
}

// NewMessage 创建初始化为空的短信消息对象。
func NewMessage() *Message {
	return &Message{
		TemplateParam: make(map[string]string),
	}
}

// WithPhone 设置接收手机号。
func (m *Message) WithPhone(phone string) *Message {
	m.PhoneNumber = phone
	return m
}

// WithSignName 设置短信签名。
func (m *Message) WithSignName(sign string) *Message {
	m.SignName = sign
	return m
}

// WithTemplate 设置模板 ID 或模板编码。
func (m *Message) WithTemplate(code string) *Message {
	m.TemplateCode = code
	return m
}

// WithParam 设置单项模板变量。
func (m *Message) WithParam(key, val string) *Message {
	if m.TemplateParam == nil {
		m.TemplateParam = make(map[string]string)
	}
	m.TemplateParam[key] = val
	return m
}

// WithParams 批量设置模板变量。
func (m *Message) WithParams(params map[string]string) *Message {
	if m.TemplateParam == nil {
		m.TemplateParam = make(map[string]string)
	}
	maps.Copy(m.TemplateParam, params)
	return m
}

// WithContent 设置纯文本短信正文。
func (m *Message) WithContent(content string) *Message {
	m.Content = content
	return m
}

// Notification 声明支持短信格式化的通知接口契约。
type Notification interface {
	ToSMS(notifiable any) any
}
