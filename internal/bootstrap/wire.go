// 本文件是唯一知道"谁依赖谁"的地方：手工装配的全部顺序都在这里，不使用 DI 容器
// （ADR-0002）。应用对象本身见 app.go。
//
// 与 package 子句之间留了空行：紧贴的话 go/doc 会把这段文件级说明并进包注释，
// 于是 pkg.go.dev 上的包文档会变成几段"本文件……"的拼接。
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
	"bgin/internal/features/auth"
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
	"bgin/internal/platform/media"
	"bgin/internal/platform/notification"
	"bgin/internal/platform/payment"
	"bgin/internal/platform/pdf"
	"bgin/internal/platform/realtime"
	"bgin/internal/platform/storage"
	"bgin/internal/platform/token"
	"bgin/internal/platform/wechat"
	"bgin/internal/shared/user"
)

// NewApplication 按依赖顺序完成装配：配置 → 日志 → 基础设施 → 领域 → HTTP。
// 任何一步失败都立即返回，不做带病启动。
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

	// 从第一个需要释放的资源起，把释放动作登记进 teardown：中途失败时逆序关闭。
	td := &teardown{log: log}

	// application_name 取自 APP_NAME：共享 Postgres 上只有它能区分连接属于谁。
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

	// 存储驱动在启动期就做一次可用性探测（见 storage.Open）：桶不存在或目录不可写时
	// 直接失败，而不是等到第一次上传才发现。
	disk, err := storage.Open(ctx, cfg.Storage)
	if err != nil {
		return nil, td.fail(err)
	}

	translator, err := i18n.New(cfg)
	if err != nil {
		return nil, td.fail(err)
	}
	// 注册一份全局默认翻译器：PDF 文档模版的 Build 只收到 ctx 与 Builder，
	// 无法从签名上拿到翻译器，与 pdf.RegisterPDF 的全局默认服务同构。
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
	// user 域内核只装配一份：前端档案与后台管理两个交付面共用同一个仓库实例（ADR-0005）。
	userRepo := user.NewRepository(db)
	profileService := profile.NewService(profile.Deps{Repo: userRepo})
	adminService := admin.NewService(admin.Deps{Repo: userRepo, Hasher: hasherClient})
	adminAuthService := adminauth.NewService(adminauth.Deps{
		Repo:   adminauth.NewRepository(db),
		Tokens: tokens,
		Hasher: hasherClient,
	})

	// 就绪探针只探测数据库与缓存：它们决定请求能否被服务。
	// 存储刻意不进来——它只影响上传类端点，让存储故障摘掉全部流量是过度反应；
	// 而它在装配期已被 storage.Open 探过一次（见上）。
	systemHandler := system.NewHandler(
		system.NewService(system.Deps{Database: databasePinger{db: db}, Cache: cacheClient}),
		log,
	)

	realtimeGateway, err := realtime.RegisterRealtimeGateway(cfg, nil)
	if err != nil {
		return nil, td.fail(err)
	}
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

	mediaManager := media.New(cfg.Media, cfg.Storage, disk, db)

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
		engine:       engine,
	}, nil
}

// 编译期断言：这两个适配器一旦不再满足探针接口，构建就会失败在定义处，
// 而不是等到别处的赋值语句才报错。
var (
	_ system.Pinger = databasePinger{}
	_ system.Pinger = (*cache.Client)(nil)
)

// databasePinger 把 *bun.DB 适配成探针要求的 system.Pinger。
// bun 暴露的是 PingContext，与接口差一个名字；在接线处补这一层，比让探针包依赖具体驱动更合适。
type databasePinger struct{ db *bun.DB }

// Ping 实现 system.Pinger。
func (p databasePinger) Ping(ctx context.Context) error { return p.db.PingContext(ctx) }

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

// teardown 累积已装配资源的释放动作，供装配中途失败时逆序调用。
//
// 手工装配下每个失败分支都要把先前打开的资源关掉，逐个手写会随依赖数量增长而漏掉某一个，
// 而漏掉的后果是把连接留给 GC。把清单收在这里之后，失败分支只剩一行 td.fail(err)。
type teardown struct {
	log     *zerolog.Logger
	closers []func() error
}

// add 登记一个释放动作。装配成功的资源由 Application.Close 负责关闭，不必登记。
func (t *teardown) add(closer func() error) { t.closers = append(t.closers, closer) }

// fail 逆序释放已登记的资源，然后原样回传失败原因——释放自身的错误只进日志，
// 不能盖掉"为什么装配失败"这个更重要的信息。
func (t *teardown) fail(cause error) error {
	for _, closer := range slices.Backward(t.closers) {
		if err := closer(); err != nil {
			t.log.Warn().Err(err).Msg("failed to close resource during teardown")
		}
	}
	return cause
}
