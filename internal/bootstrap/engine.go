package bootstrap

import (
	"errors"
	"fmt"
	"io"

	"github.com/gin-gonic/gin"

	"bgin/internal/config"
	adminauth "bgin/internal/features/admin/auth"
	admin "bgin/internal/features/admin/user"
	"bgin/internal/features/auth"
	"bgin/internal/features/client/profile"
	paymentFeature "bgin/internal/features/payment"
	realtimeFeature "bgin/internal/features/realtime"
	"bgin/internal/features/system"
	"bgin/internal/middleware"
	"bgin/internal/platform/codec"
	"bgin/internal/platform/errs"
	"bgin/internal/platform/validator"
)

// buildEngine 组装 gin 引擎与全部路由。
func buildEngine(
	cfg *config.Config,
	mw *middleware.Middleware,
	systemHandler *system.Handler,
	authHandler *auth.Handler,
	profileHandler *profile.Handler,
	adminHandler *admin.Handler,
	adminAuthHandler *adminauth.Handler,
	realtimeHandler *realtimeFeature.Handler,
	paymentHandler *paymentFeature.Handler,
) (*gin.Engine, error) {
	// 启用高性能 go-json 运行时编解码器与校验器规则
	codec.EnableGoJSON()
	if err := validator.InitEngine(); err != nil {
		return nil, fmt.Errorf("init validator engine failed: %w", err)
	}

	// 设置 Gin 运行模式并静默默认控制台输出，由结构化日志中间件统一记录
	gin.SetMode(cfg.ResolveGinMode())
	gin.DefaultWriter = io.Discard
	gin.DebugPrintRouteFunc = func(httpMethod, absolutePath, handlerName string, numHandlers int) {}

	engine := gin.New()
	engine.HandleMethodNotAllowed = true

	// 不信任代理头，客户端真实 IP 直接取 RemoteAddr，防止 IP 伪造
	if err := engine.SetTrustedProxies(nil); err != nil {
		return nil, fmt.Errorf("set trusted proxies failed: %w", err)
	}

	// 全局中间件链：链路追踪、访问日志、错误处理、恐慌恢复、超时控制、安全头、请求体缓存、幂等与跨域
	engine.Use(
		mw.RequestID(),
		mw.AccessLog(),
		mw.ErrorHandler(),
		mw.Recovery(),
		mw.Timeout(),
		mw.SecurityHeaders(),
		mw.BodyLimit(),
		mw.CacheBody(),
		mw.Idempotency(),
		mw.CORS(),
	)

	// 统一 404 与 405 错误响应，交由统一错误处理中间件渲染
	engine.NoRoute(func(c *gin.Context) {
		_ = c.Error(errs.NotFound(""))
		c.Abort()
	})
	engine.NoMethod(func(c *gin.Context) {
		_ = c.Error(errs.MethodNotAllowed(""))
		c.Abort()
	})

	// 注册根路径与系统探针路由（/、/livez、/readyz）
	system.RegisterRoutes(engine, systemHandler)

	// 仅在开启 ENABLE_DOCS 时注册 API 文档路由
	if cfg.App.EnableDocs {
		registerDocs(engine)
	}

	v1 := engine.Group("/v1")
	auth.RegisterRoutes(v1, authHandler, mw)
	profile.RegisterRoutes(v1, profileHandler, mw)
	adminauth.RegisterAdminAuthRoutes(v1, adminAuthHandler, mw)
	admin.RegisterAdminRoutes(v1, adminHandler, mw)
	realtimeFeature.RegisterRoutes(v1, realtimeHandler, mw)
	paymentFeature.RegisterRoutes(v1, paymentHandler, mw)

	return engine, nil
}

// InspectEngine 组装用于路由探测与文档自省的轻量级 Gin 引擎实例（零数据库与缓存网络连接）。
func InspectEngine(cfg *config.Config) (*gin.Engine, error) {
	if cfg == nil {
		return nil, errors.New("bootstrap: inspect engine requires a non-nil config")
	}

	mw := middleware.New(middleware.Deps{
		Limiter:        cfg.Limiter,
		CORS:           cfg.CORS,
		Production:     cfg.IsProduction(),
		RequestTimeout: cfg.App.Server.RequestTimeout,
	})

	return buildEngine(
		cfg,
		mw,
		&system.Handler{},
		&auth.Handler{},
		&profile.Handler{},
		&admin.Handler{},
		&adminauth.Handler{},
		&realtimeFeature.Handler{},
		&paymentFeature.Handler{},
	)
}
