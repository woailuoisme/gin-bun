// Package database 负责 PostgreSQL 连接与数据模型定义。
// 建表由 atlas 依据 schemas/ 驱动，Go 模型只用于运行期映射（见 ADR-0004）。
//
// 连接池建立在 database/sql 之上（bun 需要 *sql.DB），因此没有 pgxpool 那种后台健康
// 检查：陈旧的连接在使用时由 database/sql 发现，并对同一条语句重试一次。
//
// 若将来在应用与数据库之间加了连接池中间件（pgbouncer 的 transaction 模式），必须在
// DB_URL 上追加 default_query_exec_mode=exec：pgx 默认缓存 prepared statement，而缓存
// 是按后端连接算的，中间件会在不同后端之间复用同一个会话，prepare 会互相踩到。
package database

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"bgin/internal/config"
)

// Open 建立连接池并做一次连通性探测，探测失败即返回错误而非带病启动。
//
// applicationName 会作为会话的 application_name 上报，是共享 Postgres 上唯一能区分
// "这些连接是谁的"的手段：pg_stat_activity、pg_stat_statements 与慢查询日志都按它归集，
// 留空则只看到一个空名字。
func Open(ctx context.Context, cfg config.DatabaseConfig, applicationName string) (*bun.DB, error) {
	pgxConfig, err := pgx.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("database: parse DB_URL failed: %w", err)
	}

	// 单条连接的建立时间必须封顶：服务端不可达时，默认行为是一直拨号，请求会挂在这里
	// 直到更上层的超时，而那时已经没有任何有用的诊断信息。
	pgxConfig.ConnectTimeout = cfg.ConnectTimeout

	if pgxConfig.RuntimeParams == nil {
		pgxConfig.RuntimeParams = make(map[string]string, 4)
	}
	// 会话时区固定为 UTC，让服务端的时间函数与文本输出口径一致。
	// 注意这并不能解决解码问题：pgx 会把 timestamptz 解成 time.Local，瞬时正确但偏移不对，
	// 响应体的 UTC 归一必须在投影层做（见 models.UTCPtr）。
	pgxConfig.RuntimeParams["timezone"] = "UTC"
	if applicationName != "" {
		pgxConfig.RuntimeParams["application_name"] = applicationName
	}

	// 会话级超时是"连接不会被一条跑飞的 SQL 长期占住"的兜底。两个值都是显式下发，
	// 让应用行为不随部署环境的服务端默认漂移；配 0 则不下发，保留服务端自己的设置——
	// 注意这与"下发 0"不同，那是把 DBA 设的保护覆盖掉。
	if cfg.StatementTimeout > 0 {
		pgxConfig.RuntimeParams["statement_timeout"] = millis(cfg.StatementTimeout)
	}
	if cfg.IdleInTxTimeout > 0 {
		pgxConfig.RuntimeParams["idle_in_transaction_session_timeout"] = millis(cfg.IdleInTxTimeout)
	}

	sqlDB := stdlib.OpenDB(*pgxConfig)
	// 四个池参数缺一不可：上限决定占用服务端多少槽位，空闲回收与寿命换掉陈旧连接，
	// 空闲数低于上限则会让连接反复建立又关闭。
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("database: connect failed: %w", err)
	}

	db := bun.NewDB(sqlDB, pgdialect.New())
	AddQueryHooks(db, cfg)
	return db, nil
}

// BudgetWarning 检查池上限相对服务端连接预算是否过大，返回一句可直接写进日志的告警；
// 没有问题则返回空串。
//
// 分母用的是"这台 Postgres 还能给出多少连接"，而不是"本实例能用多少"：库通常不止服务
// 一个应用。池上限 × 实例数再加别人的用量一旦越过 max_connections，峰值时就会有人拿到
// "sorry, too many clients already"——那个报错出现在业务请求上，离这条启动日志很远。
//
// 判据是池上限不超过可用预算的三分之一，即假定"本应用至少还有两个同伴"。这是启发式：
// 独占的库可以合理放宽，所以只告警、不阻断启动。
func BudgetWarning(ctx context.Context, db *bun.DB, maxOpenConns int) (string, error) {
	var maxConns, reserved int
	if err := db.QueryRowContext(ctx,
		"select current_setting('max_connections')::int, current_setting('superuser_reserved_connections')::int",
	).Scan(&maxConns, &reserved); err != nil {
		return "", fmt.Errorf("database: read connection budget failed: %w", err)
	}

	usable := maxConns - reserved
	if usable <= 0 || maxOpenConns*3 <= usable {
		return "", nil
	}

	return fmt.Sprintf(
		"pool max open conns %d exceeds one third of available server budget %d (max_connections=%d, superuser_reserved=%d): "+
			"please reduce DB_MAX_OPEN_CONNS if database is shared",
		maxOpenConns, usable, maxConns, reserved), nil
}

// millis 把时长写成 Postgres 的毫秒表示（裸数字即毫秒），
// 避免 "1.5s" 这类要靠服务端解析、且各版本行为可能有差的写法。
func millis(d time.Duration) string {
	return strconv.FormatInt(d.Milliseconds(), 10)
}
