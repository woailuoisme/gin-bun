package middleware_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/config"
	"bgin/internal/middleware"
	"bgin/internal/platform/authentication"
	"bgin/internal/platform/authorization"
	"bgin/internal/platform/cache"
	"bgin/internal/platform/errs"
	"bgin/internal/shared/models"
	"bgin/internal/shared/user"
)

type memAuthBlacklistStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func newMemAuthBlacklistStore() *memAuthBlacklistStore {
	return &memAuthBlacklistStore{data: make(map[string]string)}
}

func (s *memAuthBlacklistStore) Get(_ context.Context, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.data[key]
	if !ok {
		return "", cache.ErrNotFound
	}
	return val, nil
}

func (s *memAuthBlacklistStore) Set(_ context.Context, key, value string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return nil
}

func (s *memAuthBlacklistStore) Exists(_ context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[key]
	return ok, nil
}

type mockUserRepo struct {
	users map[string]*models.User
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{users: make(map[string]*models.User)}
}

func (r *mockUserRepo) FindByID(_ context.Context, id string) (*models.User, error) {
	u, ok := r.users[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return u, nil
}

func (r *mockUserRepo) FindByCredentials(_ context.Context, creds authentication.Credentials) (*models.User, error) {
	u, ok := r.users[creds.Account]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return u, nil
}

func (r *mockUserRepo) ValidateCredentials(_ context.Context, _ *models.User, _ authentication.Credentials) (bool, error) {
	return true, nil
}

// setupAuthEnv 构建测试用中间件与守卫环境。
func setupAuthEnv() (
	*middleware.Middleware,
	*authentication.TokenIssuer,
	*authentication.Guard[*models.User],
	*authentication.Guard[*models.User],
	*mockUserRepo,
) {
	gin.SetMode(gin.TestMode)

	store := newMemAuthBlacklistStore()
	tokens := authentication.NewTokenIssuer(config.JWTConfig{
		Secret:     "0123456789abcdef0123456789abcdef",
		Issuer:     "bgin-test",
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 24 * time.Hour,
	}, store)

	repo := newMockUserRepo()
	clientGuard := authentication.NewGuard(tokens, repo, user.ClientPolicy{})
	adminGuard := authentication.NewGuard(tokens, repo, user.AdminPolicy{})

	mgr := authentication.NewManager()
	mgr.Register("client", clientGuard)
	mgr.Register("admin", adminGuard)
	mgr.SetDefault("client")

	enforcer, err := authorization.New()
	if err != nil {
		panic(err)
	}

	m := middleware.New(middleware.Deps{
		Tokens:      tokens,
		AuthManager: mgr,
		Authz:       enforcer,
	})

	return m, tokens, clientGuard, adminGuard, repo
}

// TestRequireAuth_MissingOrMalformedHeader 验证缺失或畸形 Authorization 报头时的 401 拦截。
func TestRequireAuth_MissingOrMalformedHeader(t *testing.T) {
	m, _, _, _, _ := setupAuthEnv()

	router := gin.New()
	router.Use(m.ErrorHandler())
	router.GET("/protected", m.RequireAuth(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// 1. 缺失报头
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var errResp errs.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	assert.Equal(t, string(errs.CodeAuthTokenInvalid), errResp.Code)

	// 2. 畸形 scheme
	req = httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 3. 空 Bearer
	req = httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer ")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRequireAuth_DefaultGuardSuccess 验证默认守卫 (client) 成功解析令牌并将身份写入上下文。
func TestRequireAuth_DefaultGuardSuccess(t *testing.T) {
	m, _, clientGuard, _, repo := setupAuthEnv()

	repo.users["usr_client_1"] = &models.User{
		ID:     "usr_client_1",
		Role:   models.RoleUser,
		Status: models.StatusActive,
		Name:   "Client User",
	}

	sess, err := clientGuard.Login(context.Background(), repo.users["usr_client_1"])
	require.NoError(t, err)

	router := gin.New()
	router.Use(m.ErrorHandler())

	var capturedID authentication.Identity
	router.GET("/client/me", m.RequireAuth(), func(c *gin.Context) {
		id, ok := authentication.FromContext(c.Request.Context())
		if ok {
			capturedID = id
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/client/me", nil)
	req.Header.Set("Authorization", "Bearer "+sess.Token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "usr_client_1", capturedID.UserID)
	assert.Equal(t, models.RoleUser, capturedID.Role)
}

// TestRequireAuth_NamedAdminGuard 验证指定 admin 守卫时的权限拦截与放行。
func TestRequireAuth_NamedAdminGuard(t *testing.T) {
	m, _, clientGuard, adminGuard, repo := setupAuthEnv()

	repo.users["usr_normal"] = &models.User{
		ID:     "usr_normal",
		Role:   models.RoleUser,
		Status: models.StatusActive,
	}
	repo.users["usr_admin"] = &models.User{
		ID:     "usr_admin",
		Role:   models.RoleAdmin,
		Status: models.StatusActive,
	}

	clientSess, err := clientGuard.Login(context.Background(), repo.users["usr_normal"])
	require.NoError(t, err)

	adminSess, err := adminGuard.Login(context.Background(), repo.users["usr_admin"])
	require.NoError(t, err)

	router := gin.New()
	router.Use(m.ErrorHandler())
	router.GET("/admin/dashboard", m.RequireAuth("admin"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// 1. 普通用户携带有效令牌访问 admin 接口，被 AdminPolicy 拦截返回 403 Forbidden
	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+clientSess.Token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	var errResp errs.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	assert.Equal(t, string(errs.CodeForbidden), errResp.Code)

	// 2. 管理员携带有效令牌访问 admin 接口，放行 200 OK
	req = httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+adminSess.Token)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestRequireAuth_RevocationEpoch 验证用户级注销纪元触发后，HTTP 网关即时拒绝已注销令牌。
func TestRequireAuth_RevocationEpoch(t *testing.T) {
	m, _, clientGuard, _, repo := setupAuthEnv()

	repo.users["usr_revoked"] = &models.User{
		ID:     "usr_revoked",
		Role:   models.RoleUser,
		Status: models.StatusActive,
	}

	sess, err := clientGuard.Login(context.Background(), repo.users["usr_revoked"])
	require.NoError(t, err)

	router := gin.New()
	router.Use(m.ErrorHandler())
	router.GET("/client/profile", m.RequireAuth("client"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// 初始状态下有效
	req := httptest.NewRequest(http.MethodGet, "/client/profile", nil)
	req.Header.Set("Authorization", "Bearer "+sess.Token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 触发用户级全局作废（如密码重置或点击注销所有设备）
	err = clientGuard.InvalidateAll(context.Background(), "usr_revoked")
	require.NoError(t, err)

	// 再次请求返回 401 撤销错误
	req = httptest.NewRequest(http.MethodGet, "/client/profile", nil)
	req.Header.Set("Authorization", "Bearer "+sess.Token)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	var errResp errs.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	assert.Equal(t, string(errs.CodeAuthTokenRevoked), errResp.Code)
}

// TestRequireAuth_FallbackWithoutAuthManager 验证未注入 AuthManager 时的向下兼容降级行为。
func TestRequireAuth_FallbackWithoutAuthManager(t *testing.T) {
	tokens := authentication.NewTokenIssuer(config.JWTConfig{
		Secret:     "0123456789abcdef0123456789abcdef",
		Issuer:     "bgin-test",
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 24 * time.Hour,
	}, nil)

	m := middleware.New(middleware.Deps{
		Tokens:      tokens,
		AuthManager: nil, // 未提供 AuthManager
	})

	pair, err := tokens.Issue("usr_fallback", "user")
	require.NoError(t, err)

	router := gin.New()
	router.Use(m.ErrorHandler())
	router.GET("/fallback", m.RequireAuth(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/fallback", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestRequireAuth_NamedGuardWithoutAuthManager 验证未注入 AuthManager 时使用指定守卫触发 fail-closed 错误。
func TestRequireAuth_NamedGuardWithoutAuthManager(t *testing.T) {
	tokens := authentication.NewTokenIssuer(config.JWTConfig{
		Secret:     "0123456789abcdef0123456789abcdef",
		Issuer:     "bgin-test",
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 24 * time.Hour,
	}, nil)

	m := middleware.New(middleware.Deps{
		Tokens:      tokens,
		AuthManager: nil,
	})

	pair, err := tokens.Issue("usr_admin_1", "admin")
	require.NoError(t, err)

	router := gin.New()
	router.Use(m.ErrorHandler())
	router.GET("/admin/protected", m.RequireAuth("admin"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/protected", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestRequireAuth_NoTokensWithoutAuthManager 验证未注入 AuthManager 且未配置 Tokens 时触发 500 内部错误。
func TestRequireAuth_NoTokensWithoutAuthManager(t *testing.T) {
	m := middleware.New(middleware.Deps{
		Tokens:      nil,
		AuthManager: nil,
	})

	router := gin.New()
	router.Use(m.ErrorHandler())
	router.GET("/protected", m.RequireAuth(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestRequirePermission_Unauthenticated 验证无认证身份访问受保护资源时返回 401。
func TestRequirePermission_Unauthenticated(t *testing.T) {
	m, _, _, _, _ := setupAuthEnv()

	router := gin.New()
	router.Use(m.ErrorHandler())
	router.GET("/users", m.RequirePermission("users", "read"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	var errResp errs.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	assert.Equal(t, string(errs.CodeAuthTokenInvalid), errResp.Code)
}

// TestRequirePermission_EnforcerNotConfigured 验证未配置 Authz Enforcer 时触发 500 内部错误。
func TestRequirePermission_EnforcerNotConfigured(t *testing.T) {
	m := middleware.New(middleware.Deps{
		Authz: nil,
	})

	router := gin.New()
	router.Use(m.ErrorHandler())
	router.GET("/users", func(c *gin.Context) {
		ctx := authentication.With(c.Request.Context(), authentication.Identity{
			UserID: "usr_admin_1",
			Role:   "admin",
		})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}, m.RequirePermission("users", "read"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestRequirePermission_RoleBasedAccess 验证基于 Casbin 角色策略的放行与拒绝。
func TestRequirePermission_RoleBasedAccess(t *testing.T) {
	m, _, _, _, _ := setupAuthEnv()

	router := gin.New()
	router.Use(m.ErrorHandler())
	router.GET("/users", m.RequirePermission("users", "read"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// 1. 普通用户角色无权读取 users 资源 -> 403 Forbidden
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	ctx := authentication.With(req.Context(), authentication.Identity{
		UserID: "usr_user_1",
		Role:   models.RoleUser,
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	var errResp errs.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	assert.Equal(t, string(errs.CodeForbidden), errResp.Code)

	// 2. 管理员角色有权读取 users 资源 -> 200 OK
	req = httptest.NewRequest(http.MethodGet, "/users", nil)
	ctx = authentication.With(req.Context(), authentication.Identity{
		UserID: "usr_admin_1",
		Role:   models.RoleAdmin,
	})
	req = req.WithContext(ctx)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
