// Package eventbus 提供基于 Watermill CQRS 的事件总线与消息路由基础设施。
package eventbus

import (
	"maps"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/rs/zerolog"
)

// ZerologLoggerAdapter 将系统统一的 zerolog.Logger 适配为 Watermill 的 LoggerAdapter 接口。
type ZerologLoggerAdapter struct {
	logger zerolog.Logger
	fields watermill.LogFields
}

// NewZerologLoggerAdapter 创建一个新的 ZerologLoggerAdapter 实例。
func NewZerologLoggerAdapter(logger zerolog.Logger) *ZerologLoggerAdapter {
	return &ZerologLoggerAdapter{
		logger: logger,
	}
}

func (a *ZerologLoggerAdapter) log(e *zerolog.Event, msg string, fields watermill.LogFields) {
	for k, v := range a.fields {
		e.Interface(k, v)
	}
	for k, v := range fields {
		e.Interface(k, v)
	}
	e.Msg(msg)
}

// Error 记录错误级别日志。
func (a *ZerologLoggerAdapter) Error(msg string, err error, fields watermill.LogFields) {
	a.log(a.logger.Error().Err(err), msg, fields)
}

// Info 记录信息级别日志。
func (a *ZerologLoggerAdapter) Info(msg string, fields watermill.LogFields) {
	a.log(a.logger.Info(), msg, fields)
}

// Debug 记录调试级别日志。
func (a *ZerologLoggerAdapter) Debug(msg string, fields watermill.LogFields) {
	a.log(a.logger.Debug(), msg, fields)
}

// Trace 记录跟踪级别日志。
func (a *ZerologLoggerAdapter) Trace(msg string, fields watermill.LogFields) {
	a.log(a.logger.Trace(), msg, fields)
}

// With 返回携带指定上下文字段的新适配器实例。
func (a *ZerologLoggerAdapter) With(fields watermill.LogFields) watermill.LoggerAdapter {
	if len(fields) == 0 {
		return a
	}
	combined := make(watermill.LogFields, len(a.fields)+len(fields))
	maps.Copy(combined, a.fields)
	maps.Copy(combined, fields)
	return &ZerologLoggerAdapter{
		logger: a.logger,
		fields: combined,
	}
}
