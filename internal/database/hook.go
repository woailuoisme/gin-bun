package database

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/rs/zerolog"
	"github.com/uptrace/bun"

	"bgin/internal/config"
	"bgin/internal/platform/logger"
)

// ZerologQueryHook 将 Bun 的查询生命周期映射到系统的结构化日志体系。
// 支持全链路 request_id 透传、慢查询自动捕获告警以及 Debug 级别的 SQL 审计。
type ZerologQueryHook struct {
	slowThreshold time.Duration
	debugEnabled  bool
}

// NewZerologQueryHook 创建一个基于 zerolog 的结构化查询钩子。
func NewZerologQueryHook(slowThreshold time.Duration, debugEnabled bool) *ZerologQueryHook {
	return &ZerologQueryHook{
		slowThreshold: slowThreshold,
		debugEnabled:  debugEnabled,
	}
}

// BeforeQuery 在 SQL 执行前触发，此处直接透传上下文。
func (h *ZerologQueryHook) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

// AfterQuery 在 SQL 执行完成后触发，负责记录异常报错、慢查询告警与 Debug 审计。
func (h *ZerologQueryHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	dur := time.Since(event.StartTime)
	log := logger.FromContext(ctx)

	query := cmp.Or(event.Query, event.QueryTemplate)

	// 1. 查询执行失败：排除 sql.ErrNoRows 和 sql.ErrTxDone（属于正常领域/事务控制状态）
	if event.Err != nil && !errors.Is(event.Err, sql.ErrNoRows) && !errors.Is(event.Err, sql.ErrTxDone) {
		log.Error().
			Err(event.Err).
			Str("query", query).
			Str("duration", dur.String()).
			Str("operation", event.Operation()).
			Msg("database query failed")
		return
	}

	// 2. 慢查询捕获：执行耗时超过阈值记录 Warn 日志
	if h.slowThreshold > 0 && dur >= h.slowThreshold {
		log.Warn().
			Str("query", query).
			Str("duration", dur.String()).
			Str("threshold", h.slowThreshold.String()).
			Str("operation", event.Operation()).
			Msg("slow database query detected")
		return
	}

	// 3. 调试输出：当开启调试且当前日志级别为 Debug 或更低时以 Debug 级别输出
	if h.debugEnabled && log.GetLevel() <= zerolog.DebugLevel {
		evt := log.Debug().
			Str("query", query).
			Str("duration", dur.String()).
			Str("operation", event.Operation())

		if event.Err != nil {
			evt.Err(event.Err)
		}
		evt.Msg("database query executed")
	}
}

// AddQueryHooks 为 bun.DB 挂载基于 zerolog 的统一结构化日志与慢查询监控钩子。
func AddQueryHooks(db *bun.DB, cfg config.DatabaseConfig) {
	db.AddQueryHook(NewZerologQueryHook(cfg.SlowThreshold, cfg.Debug))
}
