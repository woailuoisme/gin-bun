// Package response_test 以真实 gin 引擎验证成功响应契约（api.md 5.1）。
package response_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/platform/response"
)

func init() { gin.SetMode(gin.TestMode) }

// run 用真实引擎执行 handler，避免 gin.CreateTestContext 与状态码书写顺序的差异。
func run(handler gin.HandlerFunc) *httptest.ResponseRecorder {
	engine := gin.New()
	engine.GET("/probe", handler)

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/probe", nil))
	return recorder
}

// TestNewMeta 校验 totalPages 向上取整，且 pageSize 为 0 时不除零。
func TestNewMeta(t *testing.T) {
	assert.Equal(t, response.Meta{Page: 2, PageSize: 20, Total: 41, TotalPages: 3}, response.NewMeta(2, 20, 41))
	assert.Equal(t, 0, response.NewMeta(1, 20, 0).TotalPages)
	assert.Equal(t, 0, response.NewMeta(1, 0, 10).TotalPages)
	assert.Equal(t, 5, response.NewMeta(1, 20, 100).TotalPages)
}

// TestPaginatedSerialisesEmptyAsArray 校验空列表序列化为 [] 而不是 null（api.md 2.3）。
func TestPaginatedSerialisesEmptyAsArray(t *testing.T) {
	recorder := run(func(c *gin.Context) {
		response.Paginated[string](c, nil, response.NewMeta(1, 20, 0))
	})

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t,
		`{"items":[],"meta":{"page":1,"pageSize":20,"total":0,"totalPages":0}}`,
		recorder.Body.String(),
	)
}

// TestPaginatedKeepsItems 校验非空列表原样输出并保持信封结构。
func TestPaginatedKeepsItems(t *testing.T) {
	recorder := run(func(c *gin.Context) {
		response.Paginated(c, []int{1, 2}, response.NewMeta(1, 2, 2))
	})

	var body response.Page[int]
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, []int{1, 2}, body.Items)
	assert.Equal(t, 1, body.Meta.Page)
}

// TestOKReturnsBareObject 校验单体成功响应直接返回业务对象，不带任何外层包装。
func TestOKReturnsBareObject(t *testing.T) {
	recorder := run(func(c *gin.Context) {
		response.OK(c, map[string]string{"id": "u_1"})
	})

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"id":"u_1"}`, recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), "items")
	assert.NotContains(t, recorder.Body.String(), "success")
}

// TestCreated 校验创建成功返回 201 与新建对象。
func TestCreated(t *testing.T) {
	recorder := run(func(c *gin.Context) {
		response.Created(c, map[string]string{"id": "u_1"})
	})

	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.JSONEq(t, `{"id":"u_1"}`, recorder.Body.String())
}

// TestNoContent 校验 204 且响应体为 0 字节（api.md 5.1.4）。
func TestNoContent(t *testing.T) {
	recorder := run(response.NoContent)

	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Empty(t, recorder.Body.String())
}

// TestCollection 校验无信封集合序列化时将 nil 切片保持为 [] 而非 null。
func TestCollection(t *testing.T) {
	t.Run("nil 切片序列化为 []", func(t *testing.T) {
		recorder := run(func(c *gin.Context) {
			response.Collection[string](c, nil)
		})

		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.JSONEq(t, `[]`, recorder.Body.String())
	})

	t.Run("非空切片原样输出", func(t *testing.T) {
		recorder := run(func(c *gin.Context) {
			response.Collection(c, []string{"a", "b"})
		})

		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.JSONEq(t, `["a","b"]`, recorder.Body.String())
	})
}
