package database_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"bgin/internal/config"
	"bgin/internal/database"
	"bgin/internal/platform/logger"
)

// setupTestLogger 构造将日志输出到内存 buffer 的上下文 logger。
func setupTestLogger(level zapcore.Level, reqID string) (context.Context, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	encCfg := zap.NewProductionEncoderConfig()
	encCfg.EncodeTime = zapcore.TimeEncoderOfLayout(time.RFC3339)
	encCfg.EncodeDuration = zapcore.StringDurationEncoder
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encCfg),
		zapcore.AddSync(buf),
		level,
	)
	l := zap.New(core)
	ctx := logger.WithContext(context.Background(), l, reqID)
	return ctx, buf
}

func TestZapQueryHook_NormalQueryDebug(t *testing.T) {
	ctx, buf := setupTestLogger(zapcore.DebugLevel, "test-req-1")

	hook := database.NewZapQueryHook(200*time.Millisecond, true)

	ctx = hook.BeforeQuery(ctx, nil)

	event := &bun.QueryEvent{
		StartTime: time.Now().Add(-10 * time.Millisecond),
		Query:     "SELECT * FROM users WHERE id = 1",
	}

	hook.AfterQuery(ctx, event)

	out := buf.String()
	assert.Contains(t, out, `"level":"debug"`)
	assert.Contains(t, out, "SELECT * FROM users WHERE id = 1")
	assert.Contains(t, out, `"request_id":"test-req-1"`)
	assert.Contains(t, out, "database query executed")
}

func TestZapQueryHook_FailedQuery(t *testing.T) {
	ctx, buf := setupTestLogger(zapcore.InfoLevel, "test-req-2")

	hook := database.NewZapQueryHook(200*time.Millisecond, false)

	event := &bun.QueryEvent{
		StartTime: time.Now().Add(-5 * time.Millisecond),
		Query:     "SELECT * FROM non_existent_table",
		Err:       errors.New("table does not exist"),
	}

	hook.AfterQuery(ctx, event)

	out := buf.String()
	assert.Contains(t, out, `"level":"error"`)
	assert.Contains(t, out, "table does not exist")
	assert.Contains(t, out, `"request_id":"test-req-2"`)
	assert.Contains(t, out, "database query failed")
}

func TestZapQueryHook_ErrNoRowsNotTreatedAsError(t *testing.T) {
	ctx, buf := setupTestLogger(zapcore.InfoLevel, "test-req-3")

	hook := database.NewZapQueryHook(200*time.Millisecond, false)

	event := &bun.QueryEvent{
		StartTime: time.Now().Add(-5 * time.Millisecond),
		Query:     "SELECT * FROM users WHERE email = 'notfound@example.com'",
		Err:       sql.ErrNoRows,
	}

	hook.AfterQuery(ctx, event)

	out := buf.String()
	assert.NotContains(t, out, `"level":"error"`)
	assert.NotContains(t, out, "database query failed")
}

func TestZapQueryHook_SlowQueryWarning(t *testing.T) {
	ctx, buf := setupTestLogger(zapcore.InfoLevel, "test-req-4")

	hook := database.NewZapQueryHook(50*time.Millisecond, false)

	event := &bun.QueryEvent{
		StartTime: time.Now().Add(-100 * time.Millisecond),
		Query:     "SELECT pg_sleep(1)",
	}

	hook.AfterQuery(ctx, event)

	out := buf.String()
	assert.Contains(t, out, `"level":"warn"`)
	assert.Contains(t, out, "SELECT pg_sleep(1)")
	assert.Contains(t, out, `"threshold":"50ms"`)
	assert.Contains(t, out, "slow database query detected")
}

func TestAddQueryHooks(t *testing.T) {
	db := bun.NewDB(nil, pgdialect.New())
	cfg := config.DatabaseConfig{
		SlowThreshold: 100 * time.Millisecond,
		Debug:         true,
	}

	assert.NotPanics(t, func() {
		db = database.AddQueryHooks(db, cfg)
	})
	assert.NotNil(t, db)

	assert.Nil(t, database.AddQueryHooks(nil, cfg))
}
