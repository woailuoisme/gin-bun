package bootstrap

import (
	"context"
	"slices"

	"github.com/golang-module/carbon/v2"
	"github.com/rs/zerolog"
	"github.com/uptrace/bun"

	"bgin/internal/config"
	"bgin/internal/database"
	adminauth "bgin/internal/features/admin/auth"
	admin "bgin/internal/features/admin/user"
	"bgin/internal/features/client/auth"
	"bgin/internal/features/client/profile"
	paymentFeature "bgin/internal/features/payment"
	realtimeFeature "bgin/internal/features/realtime"
	"bgin/internal/features/system"
	"bgin/internal/middleware"
	"bgin/internal/platform/authz"
	"bgin/internal/platform/cache"
	"bgin/internal/platform/hasher"
	"bgin/internal/platform/i18n"
	"bgin/internal/platform/logger"
	"bgin/internal/platform/mailer"
	"bgin/internal/platform/notification"
	"bgin/internal/platform/payment"
	"bgin/internal/platform/pdf"
	"bgin/internal/platform/realtime"
	"bgin/internal/platform/search"
	"bgin/internal/platform/storage"
	"bgin/internal/platform/token"
	"bgin/internal/platform/wechat"
	"bgin/internal/shared/media"
	"bgin/internal/shared/user"
)

// NewApplication 按依赖顺序完成装配：配置 → 日志 → 基础设施 → 领域 → HTTP。
func NewApplication(ctx context.Context, portOverride int) (*Application, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if portOverride > 0 {
		cfg.App.Port = portOverride
	}

	carbon.SetTimezone(cfg.App.Timezone)
	carbon.SetLocale(cfg.I18n.DefaultLanguage)

	log := logger.New(cfg)

	// 登记 teardown 资源清理链，装配中途失败时逆序释放
	td := &teardown{log: log}

	// 初始化 PostgreSQL 数据库连接池
	db, err := database.Open(ctx, cfg.Database, cfg.App.Name)
	if err != nil {
		return nil, err
	}
	td.add(db.Close)

	logDatabaseReady(ctx, log, db, cfg.Database)

	cacheClient, err := cache.Open(ctx, cfg.Cache)
	if err != nil {
		return nil, td.fail(err)
	}
	td.add(cacheClient.Close)

	mailerClient, err := mailer.New(cfg.Mail, *log)
	if err != nil {
		return nil, td.fail(err)
	}
	td.add(mailerClient.Close)

	// 探测并初始化文件/对象存储驱动
	disk, err := storage.Open(ctx, cfg.Storage)
	if err != nil {
		return nil, td.fail(err)
	}

	translator, err := i18n.New(cfg)
	if err != nil {
		return nil, td.fail(err)
	}
	i18n.SetDefault(translator)

	enforcer, err := authz.New()
	if err != nil {
		return nil, td.fail(err)
	}

	tokens := token.New(cfg.JWT, cacheClient)
	hasherClient := hasher.New()

	mw := middleware.New(middleware.Deps{
		Tokens:         tokens,
		Cache:          cacheClient,
		Authz:          enforcer,
		Logger:         log,
		Limiter:        cfg.Limiter,
		CORS:           cfg.CORS,
		Production:     cfg.IsProduction(),
		Translator:     translator,
		RequestTimeout: cfg.App.Server.RequestTimeout,
	})

	authService := auth.NewService(auth.Deps{
		Repo:       auth.NewRepository(db),
		Tokens:     tokens,
		Hasher:     hasherClient,
		Cache:      cacheClient,
		Mailer:     mailerClient,
		Translator: translator,
		Config:     cfg,
	})
	// 初始化共享用户仓储及各业务服务
	userRepo := user.NewRepository(db)
	profileService := profile.NewService(profile.Deps{Repo: userRepo})
	adminService := admin.NewService(admin.Deps{Repo: userRepo, Hasher: hasherClient})
	adminAuthService := adminauth.NewService(adminauth.Deps{
		Repo:   adminauth.NewRepository(db),
		Tokens: tokens,
		Hasher: hasherClient,
	})

	searchClient, err := search.New(cfg.Search)
	if err != nil {
		return nil, td.fail(err)
	}

	realtimeReg := realtime.NewRegistry()
	realtimeFeature.RegisterChannels(realtimeReg, userRepo)

	realtimeGateway, err := realtime.RegisterRealtimeGateway(cfg, realtimeReg)
	if err != nil {
		return nil, td.fail(err)
	}

	mediaManager := media.New(cfg.Media, cfg.Storage, disk, db)

	// 初始化健康就绪探针 Handler
	systemHandler := system.NewHandler(
		system.NewService(system.Deps{
			Database: databasePinger{db: db},
			Cache:    cacheClient,
			Storage:  disk,
			Search:   searchClient,
			Realtime: realtimeGateway,
			Imgproxy: mediaManager.Image(),
		}),
		log,
	)

	realtimeService := realtimeFeature.NewService(realtimeFeature.Deps{
		Gateway: realtimeGateway,
		Cfg:     cfg,
		Secret:  cfg.Realtime.Secret,
		Users:   userRepo,
	})
	realtimeHandler := realtimeFeature.NewHandler(realtimeService)
	var wechatClient *wechat.Client
	if cfg.WeChat.MiniProgram.AppID != "" {
		wechatClient = wechat.NewClient(cfg.WeChat.MiniProgram, cacheClient)
	}

	paymentManager, err := payment.NewManager(cfg, *log)
	if err != nil {
		return nil, td.fail(err)
	}

	paymentService := paymentFeature.NewService(paymentManager, cacheClient, cfg)
	paymentHandler := paymentFeature.NewHandler(paymentService)

	notifier, err := notification.New(cfg.Notification, db, mailerClient, realtimeGateway)
	if err != nil {
		return nil, td.fail(err)
	}
	td.add(notifier.Close)

	engine, err := buildEngine(
		cfg, mw, systemHandler,
		auth.NewHandler(authService),
		profile.NewHandler(profileService),
		admin.NewHandler(adminService),
		adminauth.NewHandler(adminAuthService),
		realtimeHandler,
		paymentHandler,
	)
	if err != nil {
		return nil, td.fail(err)
	}

	pdfService, err := pdf.RegisterPDF(cfg, disk, log)
	if err != nil {
		return nil, td.fail(err)
	}

	return &Application{
		cfg:          cfg,
		logger:       log,
		db:           db,
		cache:        cacheClient,
		mailer:       mailerClient,
		notification: notifier,
		storage:      disk,
		media:        mediaManager,
		pdf:          pdfService,
		wechat:       wechatClient,
		payment:      paymentManager,
		realtime:     realtimeGateway,
		search:       searchClient,
		engine:       engine,
	}, nil
}

// 编译期断言满足探针接口。
var (
	_ system.Pinger = databasePinger{}
	_ system.Pinger = (*cache.Client)(nil)
)

// databasePinger 将 *bun.DB 适配为 system.Pinger。
type databasePinger struct{ db *bun.DB }

// Ping 实现 system.Pinger。
func (p databasePinger) Ping(ctx context.Context) error { return p.db.PingContext(ctx) }

// Driver 返回底层驱动标识。
func (databasePinger) Driver() string { return "postgres" }

// logDatabaseReady 打印数据库连接池就绪日志并进行连接池配额诊断。
func logDatabaseReady(ctx context.Context, log *zerolog.Logger, db *bun.DB, cfg config.DatabaseConfig) {
	log.Info().
		Int("max_open_conns", cfg.MaxOpenConns).
		Int("max_idle_conns", cfg.MaxIdleConns).
		Str("conn_max_lifetime", cfg.ConnMaxLifetime.String()).
		Str("conn_max_idle_time", cfg.ConnMaxIdleTime.String()).
		Str("connect_timeout", cfg.ConnectTimeout.String()).
		Str("statement_timeout", cfg.StatementTimeout.String()).
		Str("idle_in_tx_timeout", cfg.IdleInTxTimeout.String()).
		Str("slow_threshold", cfg.SlowThreshold.String()).
		Bool("debug", cfg.Debug).
		Msg("database connection pool ready")

	if warning, err := database.BudgetWarning(ctx, db, cfg.MaxOpenConns); err != nil {
		log.Debug().Err(err).Msg("database connection budget check skipped")
	} else if warning != "" {
		log.Warn().Str("detail", warning).Msg("database max open connections exceed configured budget")
	}
}

// teardown 管理装配中途失败时的逆序资源清理。
type teardown struct {
	log     *zerolog.Logger
	closers []func() error
}

// add 登记资源释放回调函数。
func (t *teardown) add(closer func() error) { t.closers = append(t.closers, closer) }

// fail 逆序释放已登记资源并返回原始错误。
func (t *teardown) fail(cause error) error {
	for _, closer := range slices.Backward(t.closers) {
		if err := closer(); err != nil {
			t.log.Warn().Err(err).Msg("failed to close resource during teardown")
		}
	}
	return cause
}
