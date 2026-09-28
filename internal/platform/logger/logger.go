// Package logger 提供 zerolog 的构建与请求作用域日志传递。
package logger

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"bgin/internal/config"
)

// ctxKey 是日志器在 context 中的私有键，避免与其他包冲突。
type ctxKey struct{}

// defaultLogger 是缺少请求作用域日志器时的兜底。
// 只保留 error 及以上并写 stderr，保证 panic 之类的严重问题永远不会被静默丢弃。
//
// 写入发生在启动期与显式注入点，读取发生在每个请求路径（FromContext），
// 因此必须用读写锁分离：无保护的并发读写会触发数据竞争。
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
	if cfg.Log.Channel == "json" {
		base = zerolog.New(os.Stdout)
	} else {
		base = zerolog.New(zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
			NoColor:    !isTerminal(),
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

// isTerminal 判断标准输出是否为交互式终端，非终端时关闭 ANSI 着色。
func isTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// WithContext 将日志器与请求标识注入 context，供下游无侵入取用。
//
// l 为 nil 时退回默认日志器：读取路径（FromContext）一直有兜底，写入路径此前没有，
// 于是忘记注入依赖时会直接空指针崩溃。
func WithContext(ctx context.Context, l *zerolog.Logger, requestID string) context.Context {
	if l == nil {
		l = defaultLog()
	}

	scoped := l.With().Str("request_id", requestID).Logger()
	return context.WithValue(ctx, ctxKey{}, &scoped)
}

// FromContextOK 取出请求作用域日志器，并报告上下文中是否真的存在作用域日志器。
//
// FromContext 为保证调用方永不为 nil 而在缺失时退回兜底日志器，
// 需要区分"作用域日志器"与"兜底日志器"（例如优先使用装配期注入的日志器）时用本方法。
func FromContextOK(ctx context.Context) (*zerolog.Logger, bool) {
	if ctx != nil {
		if l, ok := ctx.Value(ctxKey{}).(*zerolog.Logger); ok {
			return l, true
		}
	}
	return nil, false
}

// FromContext 取出请求作用域日志器，缺失时返回默认日志器而非 nil。
func FromContext(ctx context.Context) *zerolog.Logger {
	if ctx != nil {
		if l, ok := ctx.Value(ctxKey{}).(*zerolog.Logger); ok {
			return l
		}
	}
	return defaultLog()
}
