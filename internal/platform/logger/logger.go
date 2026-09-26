// Package logger 提供 zerolog 的构建与请求作用域日志传递。
package logger

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog"

	"bgin/internal/config"
)

// ctxKey 是日志器在 context 中的私有键，避免与其他包冲突。
type ctxKey struct{}

// defaultLogger 是缺少请求作用域日志器时的兜底。
// 只保留 error 及以上并写 stderr，保证 panic 之类的严重问题永远不会被静默丢弃。
var defaultLogger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).Level(zerolog.ErrorLevel)

// New 按配置构建日志器：console 给人看，json 给采集器看。
func New(cfg *config.Config) *zerolog.Logger {
	// 统一 UTC 且以 Z 结尾，与 api.md 2.2 的时间约定一致。
	zerolog.TimestampFunc = func() time.Time { return time.Now().UTC() }
	zerolog.TimeFieldFormat = time.RFC3339

	level, err := zerolog.ParseLevel(cfg.Log.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	var base zerolog.Logger
	if cfg.Log.Output == "json" {
		base = zerolog.New(os.Stdout)
	} else {
		base = zerolog.New(zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
			NoColor:    !isTerminal(),
		})
	}

	scoped := base.Level(level).With().Timestamp().Str("service", cfg.App.Name).Logger()
	return &scoped
}

// isTerminal 判断标准输出是否为交互式终端，非终端时关闭 ANSI 着色。
func isTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// WithContext 将日志器与请求标识注入 context，供下游无侵入取用。
func WithContext(ctx context.Context, l *zerolog.Logger, requestID string) context.Context {
	scoped := l.With().Str("request_id", requestID).Logger()
	return context.WithValue(ctx, ctxKey{}, &scoped)
}

// FromContext 取出请求作用域日志器，缺失时返回静默日志器而非 nil。
func FromContext(ctx context.Context) *zerolog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*zerolog.Logger); ok {
		return l
	}
	return &defaultLogger
}
