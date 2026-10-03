package database

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"bgin/internal/config"
	"bgin/internal/platform/logger"
)

// ZapQueryHook 将 Bun 的查询生命周期映射到系统的结构化日志体系。
// 支持全链路 request_id 透传、慢查询自动捕获告警以及 Debug 级别的 SQL 审计。
type ZapQueryHook struct {
	slowThreshold time.Duration
	debugEnabled  bool
}

// NewZapQueryHook 创建一个基于 zap 的结构化查询钩子。
func NewZapQueryHook(slowThreshold time.Duration, debugEnabled bool) *ZapQueryHook {
	return &ZapQueryHook{
		slowThreshold: slowThreshold,
		debugEnabled:  debugEnabled,
	}
}

// BeforeQuery 在 SQL 执行前触发，此处直接透传上下文。
func (h *ZapQueryHook) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

// AfterQuery 在 SQL 执行完成后触发，负责记录异常报错、慢查询告警与 Debug 审计。
func (h *ZapQueryHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	dur := time.Since(event.StartTime)
	log := logger.FromContext(ctx)

	query := cmp.Or(event.Query, event.QueryTemplate)

	// 1. 查询执行失败：排除 sql.ErrNoRows 和 sql.ErrTxDone（属于正常领域/事务控制状态）
	if isActionableDBError(event.Err) {
		log.Error("database query failed",
			zap.Error(event.Err),
			zap.String("query", query),
			zap.String("duration", dur.String()),
			zap.String("operation", event.Operation()))
		return
	}

	// 2. 慢查询捕获：执行耗时超过阈值记录 Warn 日志
	if h.slowThreshold > 0 && dur >= h.slowThreshold {
		log.Warn("slow database query detected",
			zap.String("query", query),
			zap.String("duration", dur.String()),
			zap.String("threshold", h.slowThreshold.String()),
			zap.String("operation", event.Operation()))
		return
	}

	// 3. 调试输出：当开启调试且当前日志级别为 Debug 或更低时以 Debug 级别输出
	if h.debugEnabled && log.Core().Enabled(zapcore.DebugLevel) {
		fields := []zap.Field{
			zap.String("query", query),
			zap.String("duration", dur.String()),
			zap.String("operation", event.Operation()),
		}
		if event.Err != nil {
			fields = append(fields, zap.Error(event.Err))
		}
		log.Debug("database query executed", fields...)
	}
}

// AddQueryHooks 为 bun.DB 挂载基于 zap 的统一结构化日志与慢查询监控钩子，以及 OpenTelemetry 链路追踪钩子。
func AddQueryHooks(db *bun.DB, cfg config.DatabaseConfig) *bun.DB {
	if db == nil {
		return nil
	}
	return db.WithQueryHook(NewOTelQueryHook()).
		WithQueryHook(NewZapQueryHook(cfg.SlowThreshold, cfg.Debug))
}

// isActionableDBError 判断数据库错误是否为需要记录与追踪的非预期故障（排除记录未命中与正常事务控制状态）。
func isActionableDBError(err error) bool {
	return err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, sql.ErrTxDone)
}
