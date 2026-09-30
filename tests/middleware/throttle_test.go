package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/middleware"
	"bgin/internal/middleware/global"
	"bgin/internal/platform/authentication"
	"bgin/internal/platform/cache"
	"bgin/internal/platform/errs"
)

// mockRateLimitStore 模拟内存限流存储。
type mockRateLimitStore struct {
	mu     sync.Mutex
	counts map[string]int64
	err    error
}

func newMockRateLimitStore() *mockRateLimitStore {
	return &mockRateLimitStore{
		counts: make(map[string]int64),
	}
}

func (s *mockRateLimitStore) Increment(_ context.Context, key string, _ time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return 0, s.err
	}
	s.counts[key]++
	return s.counts[key], nil
}

func (s *mockRateLimitStore) Get(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	val, ok := s.counts[key]
	if !ok {
		return "", cache.ErrNotFound
	}
	return strconv.FormatInt(val, 10), nil
}

// TestRateLimit_Allowed 验证在配额内请求被正常放行并附带标准限流响应头。
func TestRateLimit_Allowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockRateLimitStore()

	router := gin.New()
	router.Use(middleware.ThrottleWithStore(store, 10, time.Minute, middleware.WithThrottleTier(middleware.TierAnonymous)))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "10", w.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "9", w.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Reset"))
}

// TestRateLimit_Exceeded 验证超出配额时拦截为 429 并附带 Retry-After。
func TestRateLimit_Exceeded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockRateLimitStore()

	router := gin.New()
	router.Use(global.New(global.Deps{}).ErrorHandler())
	router.Use(middleware.ThrottleWithStore(store, 2, time.Minute, middleware.WithThrottleTier(middleware.TierAnonymous)))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 前 2 次允许
	for range 2 {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
	}

	// 第 3 次超限 (limit = 2)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "2", w.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "0", w.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Reset"))
	assert.NotEmpty(t, w.Header().Get("Retry-After"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, string(errs.CodeRateLimited), body["code"])
}

// TestRateLimit_SlidingWindow 验证前一分钟窗口的加权余量计入当前估算。
func TestRateLimit_SlidingWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockRateLimitStore()

	// 预置前一个 bucket 的高频请求数 (600次，保证无论当前秒数偏移如何，平滑加权都大于限流阈值 5)
	now := time.Now().UTC()
	bucket := now.Unix() / 60
	prevKey := cache.Key("ratelimit", string(middleware.TierAnonymous), "ip:10.0.0.1", strconv.FormatInt(60, 10), strconv.FormatInt(bucket-1, 10))
	store.counts[prevKey] = 600

	router := gin.New()
	router.Use(global.New(global.Deps{}).ErrorHandler())
	router.Use(middleware.ThrottleWithStore(store, 5, time.Minute, middleware.WithThrottleTier(middleware.TierAnonymous)))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 滑动窗口加权后应直接触发 429
	require.Equal(t, http.StatusTooManyRequests, w.Code)
}

// TestRateLimit_StoreFailOpen 验证缓存/存储异常时降级放行。
func TestRateLimit_StoreFailOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockRateLimitStore()
	store.err = errors.New("redis connection refused")

	router := gin.New()
	router.Use(middleware.ThrottleWithStore(store, 5, time.Minute, middleware.WithThrottleTier(middleware.TierAnonymous)))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 存储故障时应当降级放行
	require.Equal(t, http.StatusOK, w.Code)
}

// TestRateLimit_NilStoreOrZeroLimit 验证存储为空或配额<=0时直接放行。
func TestRateLimit_NilStoreOrZeroLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 1. nil store
	router1 := gin.New()
	router1.Use(middleware.ThrottleWithStore(nil, 5, time.Minute, middleware.WithThrottleTier(middleware.TierAnonymous)))
	router1.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	w1 := httptest.NewRecorder()
	router1.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code)

	// 2. 0 limit
	store := newMockRateLimitStore()
	router2 := gin.New()
	router2.Use(middleware.ThrottleWithStore(store, 0, time.Minute, middleware.WithThrottleTier(middleware.TierAnonymous)))
	router2.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	w2 := httptest.NewRecorder()
	router2.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
}

// TestRateLimit_AuthenticatedScope 验证认证用户维度按 UserID 隔离。
func TestRateLimit_AuthenticatedScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockRateLimitStore()

	router := gin.New()
	// 注入认证用户上下文
	router.Use(func(c *gin.Context) {
		ctx := authentication.With(c.Request.Context(), authentication.Identity{
			UserID: "usr_12345",
		})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.Use(middleware.ThrottleWithStore(store, 10, time.Minute, middleware.WithThrottleTier(middleware.TierAuthenticated)))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	// 验证存储中的 key 包含了 user:usr_12345
	now := time.Now().UTC()
	bucket := now.Unix() / 60
	expectedKey := cache.Key("ratelimit", string(middleware.TierAuthenticated), "user:usr_12345", strconv.FormatInt(60, 10), strconv.FormatInt(bucket, 10))
	assert.Equal(t, int64(1), store.counts[expectedKey])
}

// TestThrottle_CustomWindow 验证支持任意时间窗口（如 10 秒）。
func TestThrottle_CustomWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockRateLimitStore()

	router := gin.New()
	router.Use(middleware.ThrottleWithStore(store, 3, 10*time.Second))
	router.GET("/hourly", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/hourly", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "3", w.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "2", w.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Reset"))
}

// TestThrottle_CustomKey 验证支持自定义分区维度（如按登录账号 email 分区）。
func TestThrottle_CustomKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockRateLimitStore()

	router := gin.New()
	router.Use(middleware.ThrottleWithStore(
		store,
		5,
		time.Minute,
		middleware.WithThrottleKey(func(c *gin.Context) string {
			return "email:" + c.Query("email")
		}),
	))
	router.GET("/login-attempt", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/login-attempt?email=admin@example.com", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	now := time.Now().UTC()
	bucket := now.Unix() / 60
	expectedKey := cache.Key("ratelimit", "custom", "email:admin@example.com", strconv.FormatInt(60, 10), strconv.FormatInt(bucket, 10))
	assert.Equal(t, int64(1), store.counts[expectedKey])
}

// TestThrottle_Expression 验证类似 Laravel 的 "max_attempts,decay_minutes" 字符串表达式。
func TestThrottle_Expression(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockRateLimitStore()

	m := middleware.New(middleware.Deps{})
	m.SetLimiterStore(store)

	router := gin.New()
	router.Use(global.New(global.Deps{}).ErrorHandler())
	// 允许每 1 分钟 2 次请求
	router.GET("/api/fast", m.Throttle("2,1"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 第一次正常
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/api/fast", nil))
	assert.Equal(t, http.StatusOK, w1.Code)
	assert.Equal(t, "2", w1.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "1", w1.Header().Get("X-RateLimit-Remaining"))

	// 第二次正常
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/fast", nil))
	assert.Equal(t, http.StatusOK, w2.Code)
	assert.Equal(t, "0", w2.Header().Get("X-RateLimit-Remaining"))

	// 第三次触发限流 429
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/api/fast", nil))
	assert.Equal(t, http.StatusTooManyRequests, w3.Code)
}

// TestThrottle_NamedLimiter 验证通过 RegisterLimiter 注册并在路由中使用命名限流器。
func TestThrottle_NamedLimiter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockRateLimitStore()

	m := middleware.New(middleware.Deps{})
	m.SetLimiterStore(store)

	// 注册名为 "uploads" 的命名限流器
	m.RegisterLimiter("uploads", func(c *gin.Context) middleware.Limit {
		return middleware.PerMinute(1).By("user:" + c.Query("user_id"))
	})

	router := gin.New()
	router.Use(global.New(global.Deps{}).ErrorHandler())
	router.POST("/uploads", m.Throttle("uploads"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "uploaded"})
	})

	// 用户 1 第一次上传
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, httptest.NewRequest(http.MethodPost, "/uploads?user_id=101", nil))
	assert.Equal(t, http.StatusOK, w1.Code)

	// 用户 1 第二次上传 -> 429
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/uploads?user_id=101", nil))
	assert.Equal(t, http.StatusTooManyRequests, w2.Code)

	// 用户 2 第一次上传不受用户 1 影响
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, httptest.NewRequest(http.MethodPost, "/uploads?user_id=102", nil))
	assert.Equal(t, http.StatusOK, w3.Code)
}

// TestThrottle_LimitBuilders 验证 Limit 链式辅助函数的周期设置。
func TestThrottle_LimitBuilders(t *testing.T) {
	l1 := middleware.PerMinute(60)
	assert.Equal(t, 60, l1.MaxAttempts)
	assert.Equal(t, time.Minute, l1.Decay)

	l2 := middleware.PerMinutes(5, 100)
	assert.Equal(t, 100, l2.MaxAttempts)
	assert.Equal(t, 5*time.Minute, l2.Decay)

	l3 := middleware.PerHour(1000)
	assert.Equal(t, 1000, l3.MaxAttempts)
	assert.Equal(t, time.Hour, l3.Decay)

	l4 := middleware.PerDay(5000)
	assert.Equal(t, 5000, l4.MaxAttempts)
	assert.Equal(t, 24*time.Hour, l4.Decay)

	l5 := middleware.None()
	assert.Equal(t, 0, l5.MaxAttempts)
}

// TestThrottle_CustomResponse 验证自定义超限拦截响应回调。
func TestThrottle_CustomResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockRateLimitStore()

	m := middleware.New(middleware.Deps{})
	m.SetLimiterStore(store)

	m.RegisterLimiter("custom_res", func(c *gin.Context) middleware.Limit {
		return middleware.PerMinute(1).Response(func(c *gin.Context) {
			c.JSON(http.StatusTooManyRequests, gin.H{"custom_error": "please_wait"})
		})
	})

	router := gin.New()
	router.GET("/custom", m.Throttle("custom_res"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/custom", nil))
	assert.Equal(t, http.StatusOK, w1.Code)

	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/custom", nil))
	assert.Equal(t, http.StatusTooManyRequests, w2.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &body))
	assert.Equal(t, "please_wait", body["custom_error"])
}

// TestThrottle_UnknownLimiterNilLogger 验证当未配置 Logger 时请求未知限流器不会发生空指针 panic，且优雅回退至匿名档位。
func TestThrottle_UnknownLimiterNilLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockRateLimitStore()

	// 显式传入 Logger 为 nil 的 Deps
	m := middleware.New(middleware.Deps{
		Logger: nil,
	})
	m.SetLimiterStore(store)

	router := gin.New()
	router.GET("/fallback", m.Throttle("non_existent_limiter"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	assert.NotPanics(t, func() {
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/fallback", nil))
	})
	assert.Equal(t, http.StatusOK, w.Code)
}
