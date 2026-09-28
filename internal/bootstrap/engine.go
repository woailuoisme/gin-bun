// 本文件只负责把 gin 引擎与路由装出来：中间件顺序、探针、文档开关、各领域的路由注册。
// 文档页的具体实现见 docs.go，服务如何跑起来见 server.go。
//
// 与 package 子句之间留空行，避免这段文件级说明被 go/doc 并进包注释。
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
	// 启用高性能 go-json 运行时编解码器与校验器规则。
	codec.EnableGoJSON()
	if err := validator.InitEngine(); err != nil {
		return nil, fmt.Errorf("init validator engine failed: %w", err)
	}

	// 模式由配置层定夺（见 config.ResolveGinMode）。
	// 静默 gin 的标准 debug 输出与路由注册明细，所有可观测性由结构化中间件与 zerolog 统一承载。
	gin.SetMode(cfg.ResolveGinMode())
	gin.DefaultWriter = io.Discard
	gin.DebugPrintRouteFunc = func(httpMethod, absolutePath, handlerName string, numHandlers int) {}

	engine := gin.New()
	engine.HandleMethodNotAllowed = true

	// 不信任任何代理头：客户端 IP 直接取 RemoteAddr，避免限流配额被伪造的 X-Forwarded-For 绕过。
	if err := engine.SetTrustedProxies(nil); err != nil {
		return nil, fmt.Errorf("set trusted proxies failed: %w", err)
	}

	// 顺序有讲究：RequestID 必须最先执行，后续中间件与 handler 都依赖它注入的日志器；
	// AccessLog 记录最终状态码与 c.Errors，Recovery 捕获 panic，ErrorHandler 统一渲染错误。
	engine.Use(
		mw.RequestID(),
		mw.AccessLog(),
		mw.ErrorHandler(),
		mw.Recovery(),
		mw.Timeout(),
		mw.SecurityHeaders(),
		mw.BodyLimit(),
		mw.CacheBody(),
		// 幂等必须排在 CacheBody 之后：它要读已经被缓存下来的请求体来计算载荷指纹。
		// 探测模式（不强制携带 Idempotency-Key），因此不会破坏既有客户端与渠道回调。
		mw.Idempotency(),
		mw.CORS(),
	)

	// 全局未匹配路由与不支持方法回退：统一向上下文写入标准业务错误，
	// 交由后置 ErrorHandler 中间件渲染为统一的 JSON 响应，杜绝框架级纯文本泄漏。
	engine.NoRoute(func(c *gin.Context) {
		_ = c.Error(errs.NotFound(""))
		c.Abort()
	})
	engine.NoMethod(func(c *gin.Context) {
		_ = c.Error(errs.MethodNotAllowed(""))
		c.Abort()
	})

	// 探针：/healthz 存活、/readyz 就绪。两者都不挂限流与鉴权——依赖故障时它们仍须可被触发。
	system.RegisterRoutes(engine, systemHandler)

	// 文档路由默认不挂载：它会暴露完整端点清单、请求参数与认证方式，
	// 只在显式开启 ENABLE_DOCS 时注册，未开启时 /docs 与普通未知路径一样返回 404。
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
// cfg 必须由调用方显式提供：此处不再隐式装载配置，更不伪造默认配置兜底，
// 否则 route list 展示的中间件行为可能与真实运行时不一致。
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
