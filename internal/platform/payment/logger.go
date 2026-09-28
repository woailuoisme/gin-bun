package payment

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/rs/zerolog"
)

// paymentLogPlaceholder 是 SDK 传入空日志参数时的占位消息。
const paymentLogPlaceholder = "gopay sdk log entry"

// xlogAdapter 把 zerolog 适配为 gopay 期望的 xlog.XLogger 接口。
//
// gopay 只通过 SetLogger 暴露日志注入点，接口仅接受可变参数，无法保留 zerolog 的字段化能力，
// 因此这里统一把参数拼装为单条消息转发。gopay 请求与响应的明细日志都在 Debug 级别，
// 生产环境按 LOG_LEVEL 过滤即可，不必担心凭据泄漏到信息级日志。
type xlogAdapter struct {
	logger zerolog.Logger
}

// newXLogAdapter 创建 gopay 日志适配器。
func newXLogAdapter(logger zerolog.Logger) xlogAdapter {
	return xlogAdapter{logger: logger.With().Str("component", "payment_sdk").Logger()}
}

// Debug 记录 Debug 级别日志。
func (a xlogAdapter) Debug(args ...any) { a.logger.Debug().Msg(composeLogMessage(args...)) }

// Info 记录 Info 级别日志。
func (a xlogAdapter) Info(args ...any) { a.logger.Info().Msg(composeLogMessage(args...)) }

// Warn 记录 Warn 级别日志。
func (a xlogAdapter) Warn(args ...any) { a.logger.Warn().Msg(composeLogMessage(args...)) }

// Error 记录 Error 级别日志。
func (a xlogAdapter) Error(args ...any) { a.logger.Error().Msg(composeLogMessage(args...)) }

// Debugf 按格式记录 Debug 级别日志。
func (a xlogAdapter) Debugf(format string, args ...any) { a.logger.Debug().Msgf(format, args...) }

// Infof 按格式记录 Info 级别日志。
func (a xlogAdapter) Infof(format string, args ...any) { a.logger.Info().Msgf(format, args...) }

// Warnf 按格式记录 Warn 级别日志。
func (a xlogAdapter) Warnf(format string, args ...any) { a.logger.Warn().Msgf(format, args...) }

// Errorf 按格式记录 Error 级别日志。
func (a xlogAdapter) Errorf(format string, args ...any) { a.logger.Error().Msgf(format, args...) }

// composeLogMessage 把可变参数拼装为单条日志消息，空消息回退为占位符。
func composeLogMessage(args ...any) string {
	return cmp.Or(strings.TrimSpace(fmt.Sprint(args...)), paymentLogPlaceholder)
}
