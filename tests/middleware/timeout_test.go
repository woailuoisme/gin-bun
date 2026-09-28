package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/middleware"
)

// TestTimeoutMiddlewareWithinBudget 验证请求在超时预算内正常返回。
func TestTimeoutMiddlewareWithinBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := middleware.New(middleware.Deps{
		RequestTimeout: 50 * time.Millisecond,
	})

	router := gin.New()
	router.Use(m.ErrorHandler())
	router.Use(m.Timeout())

	router.GET("/fast", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/fast", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}

// TestTimeoutMiddlewareExceeded 验证超出截止期时返回 504 GATEWAY_TIMEOUT。
func TestTimeoutMiddlewareExceeded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := middleware.New(middleware.Deps{
		RequestTimeout: 20 * time.Millisecond,
	})

	router := gin.New()
	router.Use(m.ErrorHandler())
	router.Use(m.Timeout())

	router.GET("/slow", func(c *gin.Context) {
		select {
		case <-time.After(100 * time.Millisecond):
			c.JSON(http.StatusOK, gin.H{"status": "late"})
		case <-c.Request.Context().Done():
			// 模拟下游感知到超时后主动退出
			return
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusGatewayTimeout, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "GATEWAY_TIMEOUT", resp["code"])
}
