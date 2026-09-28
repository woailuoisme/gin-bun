package middleware_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/middleware"
	"bgin/internal/platform/errs"
)

// TestAccessLog404SingleWarn 验证 404 请求仅输出一条 WARN 访问日志，
// 且错误文本不含 gin 内部前缀与尾部换行符。
func TestAccessLog404SingleWarn(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var buf bytes.Buffer
	testLogger := zerolog.New(&buf).Level(zerolog.DebugLevel)

	mw := middleware.New(middleware.Deps{Logger: &testLogger})
	engine := gin.New()
	engine.Use(
		mw.RequestID(),
		mw.AccessLog(),
		mw.ErrorHandler(),
	)

	engine.NoRoute(func(c *gin.Context) {
		_ = c.Error(errs.NotFound(""))
		c.Abort()
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/not-found", nil))

	require.Equal(t, http.StatusNotFound, rec.Code)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 1, "404 请求必须且仅输出一条结构化访问日志，禁止重复打印告警日志")

	type logEntry struct {
		Level   string `json:"level"`
		Message string `json:"message"`
		Status  int    `json:"status"`
		Code    string `json:"code"`
		Error   string `json:"error"`
		Path    string `json:"path"`
		Method  string `json:"method"`
		Stack   string `json:"stack"`
	}

	var entry logEntry
	err := json.Unmarshal([]byte(lines[0]), &entry)
	require.NoError(t, err)

	assert.Equal(t, "warn", entry.Level)
	assert.Equal(t, "access", entry.Message)
	assert.Equal(t, 404, entry.Status)
	assert.Equal(t, "NOT_FOUND", entry.Code)
	assert.Equal(t, "The requested resource was not found", entry.Error)
	assert.NotContains(t, entry.Error, "Error #01")
	assert.NotContains(t, entry.Error, "\n")
	assert.Equal(t, "/not-found", entry.Path)
	assert.Equal(t, "GET", entry.Method)
}

// TestAccessLog500ErrorWithStack 验证 500 服务端内部错误具备调用栈日志与访问日志分工。
func TestAccessLog500ErrorWithStack(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var buf bytes.Buffer
	testLogger := zerolog.New(&buf).Level(zerolog.DebugLevel)

	mw := middleware.New(middleware.Deps{Logger: &testLogger})
	engine := gin.New()
	engine.Use(
		mw.RequestID(),
		mw.AccessLog(),
		mw.ErrorHandler(),
	)

	engine.GET("/panic-or-500", func(c *gin.Context) {
		_ = c.Error(errs.Internal(errors.New("db connection lost")))
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic-or-500", nil))

	require.Equal(t, http.StatusInternalServerError, rec.Code)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2, "500 请求应分别输出堆栈异常日志 (request processing failed) 与访问日志 (access)")

	type logEntry struct {
		Level   string `json:"level"`
		Message string `json:"message"`
		Status  int    `json:"status"`
		Stack   string `json:"stack"`
	}

	var errLog, accessLog logEntry
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &errLog))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &accessLog))

	assert.Equal(t, "error", errLog.Level)
	assert.Equal(t, "request processing failed", errLog.Message)
	assert.NotEmpty(t, errLog.Stack)

	assert.Equal(t, "error", accessLog.Level)
	assert.Equal(t, "access", accessLog.Message)
	assert.Equal(t, 500, accessLog.Status)
}
