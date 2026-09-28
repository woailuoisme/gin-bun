// Package logger 提供 zerolog 的构建与请求作用域日志传递。
package logger

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"bgin/internal/config"
)

// ctxKey 是日志器在 context 中的私有键，避免与其他包冲突。
type ctxKey struct{}

// defaultLogger 是缺少请求作用域日志器时的并发安全兜底输出。
var (
	defaultLoggerMu sync.RWMutex
	defaultLogger   = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).Level(zerolog.ErrorLevel)
)

// setDefault 在锁保护下替换兜底日志器。
func setDefault(l *zerolog.Logger) {
	defaultLoggerMu.Lock()
	defaultLogger = *l
	defaultLoggerMu.Unlock()
}

// defaultLog 在锁保护下取得兜底日志器副本。
func defaultLog() *zerolog.Logger {
	defaultLoggerMu.RLock()
	defer defaultLoggerMu.RUnlock()

	l := defaultLogger
	return &l
}

// New 按配置构建日志器：console 模式用于终端交互，json 模式用于日志采集。
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
	if cfg.Log.Channel == "json" {
		base = zerolog.New(os.Stdout)
	} else {
		base = zerolog.New(zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
			NoColor:    !shouldColor(cfg),
		})
	}

	builder := base.Level(level).With().Timestamp()
	if cfg.Log.Channel == "json" {
		builder = builder.Str("service", cfg.App.Name)
	}

	scoped := builder.Logger()
	setDefault(&scoped)
	return &scoped
}

// SetDefault 显式设置全局默认的兜底日志器。
func SetDefault(l *zerolog.Logger) {
	if l != nil {
		setDefault(l)
	}
}

// shouldColor 综合判断终端状态与环境变量决定是否启用 ANSI 着色：
// 1. 遵循 https://no-color.org 规范：NO_COLOR 存在且非空时禁用着色；
// 2. 遵循 FORCE_COLOR 规范：值为 "0" 或 "false" 时显式禁用，其他非空值或 CLICOLOR_FORCE=1 时强制启用；
// 3. 本地开发环境（local/dev）且日志通道为 console 时默认启用着色（兼容 air 与 concurrently 管道重定向）；
// 4. 其他环境依标准输出是否为 TTY 字符终端决定。
func shouldColor(cfg *config.Config) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if val := os.Getenv("FORCE_COLOR"); val != "" {
		if val == "0" || strings.EqualFold(val, "false") {
			return false
		}
		return true
	}
	if os.Getenv("CLICOLOR_FORCE") == "1" {
		return true
	}
	if cfg != nil && (cfg.App.Env == "local" || cfg.App.Env == "development") {
		return true
	}
	return isTerminal()
}

// isTerminal 判断标准输出是否为交互式终端，非终端时关闭 ANSI 着色。
func isTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// WithContext 将日志器与请求标识注入 context。
func WithContext(ctx context.Context, l *zerolog.Logger, requestID string) context.Context {
	if l == nil {
		l = defaultLog()
	}

	scoped := l.With().Str("request_id", requestID).Logger()
	return context.WithValue(ctx, ctxKey{}, &scoped)
}

// FromContextOK 从 context 中提取请求作用域日志器。
func FromContextOK(ctx context.Context) (*zerolog.Logger, bool) {
	if ctx != nil {
		if l, ok := ctx.Value(ctxKey{}).(*zerolog.Logger); ok {
			return l, true
		}
	}
	return nil, false
}

// FromContext 提取请求作用域日志器，缺失时返回默认兜底日志器。
func FromContext(ctx context.Context) *zerolog.Logger {
	if l, ok := FromContextOK(ctx); ok {
		return l
	}
	return defaultLog()
}
