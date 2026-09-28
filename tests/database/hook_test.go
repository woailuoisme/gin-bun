package database_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"bgin/internal/config"
	"bgin/internal/database"
	"bgin/internal/platform/logger"
)

// setupTestLogger 构造将日志输出到内存 buffer 的上下文 logger。
func setupTestLogger(level zerolog.Level, reqID string) (context.Context, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	l := zerolog.New(buf).Level(level).With().Timestamp().Logger()
	ctx := logger.WithContext(context.Background(), &l, reqID)
	return ctx, buf
}

func TestZerologQueryHook_NormalQueryDebug(t *testing.T) {
	ctx, buf := setupTestLogger(zerolog.DebugLevel, "test-req-1")

	hook := database.NewZerologQueryHook(200*time.Millisecond, true)

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
	assert.Contains(t, out, `"message":"database query executed"`)
}

func TestZerologQueryHook_FailedQuery(t *testing.T) {
	ctx, buf := setupTestLogger(zerolog.InfoLevel, "test-req-2")

	hook := database.NewZerologQueryHook(200*time.Millisecond, false)

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
	assert.Contains(t, out, `"message":"database query failed"`)
}

func TestZerologQueryHook_ErrNoRowsNotTreatedAsError(t *testing.T) {
	ctx, buf := setupTestLogger(zerolog.InfoLevel, "test-req-3")

	hook := database.NewZerologQueryHook(200*time.Millisecond, false)

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

func TestZerologQueryHook_SlowQueryWarning(t *testing.T) {
	ctx, buf := setupTestLogger(zerolog.InfoLevel, "test-req-4")

	hook := database.NewZerologQueryHook(50*time.Millisecond, false)

	event := &bun.QueryEvent{
		StartTime: time.Now().Add(-100 * time.Millisecond),
		Query:     "SELECT pg_sleep(1)",
	}

	hook.AfterQuery(ctx, event)

	out := buf.String()
	assert.Contains(t, out, `"level":"warn"`)
	assert.Contains(t, out, "SELECT pg_sleep(1)")
	assert.Contains(t, out, `"threshold":"50ms"`)
	assert.Contains(t, out, `"message":"slow database query detected"`)
}

func TestAddQueryHooks(t *testing.T) {
	db := bun.NewDB(nil, pgdialect.New())
	cfg := config.DatabaseConfig{
		SlowThreshold: 100 * time.Millisecond,
		Debug:         true,
	}

	assert.NotPanics(t, func() {
		database.AddQueryHooks(db, cfg)
	})
}
