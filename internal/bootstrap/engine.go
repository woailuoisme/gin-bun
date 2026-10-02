package bootstrap

import (
	"errors"
	"fmt"
	"io"

	"github.com/gin-gonic/gin"

	"bgin/internal/config"
	adminauth "bgin/internal/features/admin/auth"
	admin "bgin/internal/features/admin/user"
	"bgin/internal/features/client/auth"
	"bgin/internal/features/client/profile"
	paymentFeature "bgin/internal/features/payment"
	realtimeFeature "bgin/internal/features/realtime"
	"bgin/internal/features/system"
	"bgin/internal/middleware"
	"bgin/internal/middleware/global"
	"bgin/internal/platform/errs"
	"bgin/internal/platform/validator"
)

// buildEngine 组装 gin 引擎与全部路由。
func buildEngine(
	cfg *config.Config,
	globalMW *global.Middleware,
	routeMW *middleware.Middleware,
	systemHandler *system.Handler,
	authHandler *auth.Handler,
	profileHandler *profile.Handler,
	adminHandler *admin.Handler,
	adminAuthHandler *adminauth.Handler,
	realtimeHandler *realtimeFeature.Handler,
	paymentHandler *paymentFeature.Handler,
) (*gin.Engine, error) {
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

	// 全局中间件链：遥测追踪、请求标识、Sentry监控、访问日志、错误处理、恐慌恢复、超时控制、安全头、请求体缓存、幂等与跨域
	engine.Use(
		globalMW.Telemetry(),
		globalMW.RequestID(),
		globalMW.Sentry(),
		globalMW.AccessLog(),
		globalMW.ErrorHandler(),
		globalMW.Recovery(),
		globalMW.Timeout(),
		globalMW.SecurityHeaders(),
		globalMW.BodyLimit(),
		globalMW.CacheBody(),
		globalMW.Idempotency(),
		globalMW.CORS(),
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
	auth.RegisterRoutes(v1, authHandler, routeMW)
	profile.RegisterRoutes(v1, profileHandler, routeMW)
	adminauth.RegisterAdminAuthRoutes(v1, adminAuthHandler, routeMW)
	admin.RegisterAdminRoutes(v1, adminHandler, routeMW)
	realtimeFeature.RegisterRoutes(v1, realtimeHandler, routeMW)
	paymentFeature.RegisterRoutes(v1, paymentHandler, routeMW)

	return engine, nil
}

// InspectEngine 组装用于路由探测与文档自省的轻量级 Gin 引擎实例（零数据库与缓存网络连接）。
func InspectEngine(cfg *config.Config) (*gin.Engine, error) {
	if cfg == nil {
		return nil, errors.New("bootstrap: inspect engine requires a non-nil config")
	}

	globalMW := global.New(global.Deps{
		CORS:           cfg.CORS,
		Production:     cfg.IsProduction(),
		RequestTimeout: cfg.App.Server.RequestTimeout,
	})

	routeMW := middleware.New(middleware.Deps{
		Limiter: cfg.Limiter,
	})

	return buildEngine(
		cfg,
		globalMW,
		routeMW,
		&system.Handler{},
		&auth.Handler{},
		&profile.Handler{},
		&admin.Handler{},
		&adminauth.Handler{},
		&realtimeFeature.Handler{},
		&paymentFeature.Handler{},
	)
}
