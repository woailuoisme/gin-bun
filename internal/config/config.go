// Package config 负责从环境装载并校验应用配置。
//
// 装载沿用 caarlos0/env 的方式：字段上的 env 标签就是环境变量名，默认值写在 envDefault，
// 存在性约束作为选项写在 env 标签内部、逗号分隔——例如 env:"DB_URL,required,notEmpty"。
// 名字、默认值与存在性约束都与字段写在一起，新增一个配置项只需改这个结构体。
//
// 两个必须知道的坑（都会静默失效，不报错）：
//
//  1. 选项只能在 env 标签里。写成独立的 required:"true" 不会被读取——该包只认 env 标签
//     的逗号后缀，独立标签既不是错误也不会生效。可用的选项：required（未设置即失败）、
//     notEmpty（设为空串即失败）、expand、file、unset、init。
//  2. 该包**不支持 validate 标签**（v11.4.1 源码里没有相关实现），写了同样不生效：曾经
//     写过 validate:"oneof=..." 而非法 APP_ENV 照样通过。
//
// 因此取值白名单、长度下限、跨字段关系这类语义约束一律写在 validate() 里，那里能给出
// 点名环境变量的中文报错。
//
// 本包曾有一版 koanf 四层装载（结构体标签 + 编进二进制的 app.toml 默认值 + 按环境的覆盖
// 文件 + 环境变量白名单），因"同一项要在三处同步、漏改只会静默取默认值"的成本被换回这里。
// 代价是失去了"默认值编进二进制"与"按环境覆盖文件"两层，结构性差异（端口、配额、开关）
// 以后只能靠环境变量表达。
package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Config 是应用的全部配置。字段本身不带标签，环境变量名写在各自字段上。
type Config struct {
	App      AppConfig
	Database DatabaseConfig
	Cache    CacheConfig
	Storage  StorageConfig
	JWT      JWTConfig
	Mail     MailConfig
	I18n     I18nConfig
	Log      LogConfig
	Auth     AuthConfig
	Limiter  LimiterConfig
	CORS     CORSConfig
	Seed     SeedConfig
}

// AppConfig 描述进程自身的运行参数。
type AppConfig struct {
	Name string `env:"APP_NAME" envDefault:"bgin"`
	// Env 同时决定 gin 的运行模式与调试输出的宽松程度，因此取值必须收敛：
	// 一个拼错的值会被静默当成"非本地、非生产"，两头的便利与收敛性同时失效。
	// test 由集成测试置入（tests/integration/support_test.go）。
	Env  string `env:"APP_ENV" envDefault:"local"`
	Port int    `env:"APP_PORT" envDefault:"3300"`
	// GinMode 显式覆盖 gin 的运行模式（debug / release / test）。留空则按 Env 推导，
	// 见 ResolveGinMode。
	GinMode string `env:"GIN_MODE"`
	// EnableDocs 控制是否挂载 /docs 上的 Scalar UI。默认关闭：它会暴露完整端点
	// 清单、请求参数与认证方式，属于需要显式开启的能力。
	EnableDocs bool `env:"ENABLE_DOCS" envDefault:"false"`
	// URL 是前端站点根地址，用于拼接密码重置链接，不是 API 自身的地址。
	URL string `env:"APP_URL" envDefault:"http://localhost:3300"`
	// Server 描述 HTTP 传输层超时与停机预算。
	Server HTTPServerConfig
}

// HTTPServerConfig 描述 HTTP 传输层超时与停机预算。
type HTTPServerConfig struct {
	ReadHeaderTimeout time.Duration `env:"HTTP_READ_HEADER_TIMEOUT" envDefault:"10s"`
	ReadTimeout       time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"15s"`
	WriteTimeout      time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"35s"`
	IdleTimeout       time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"120s"`
	ShutdownTimeout   time.Duration `env:"HTTP_SHUTDOWN_TIMEOUT" envDefault:"15s"`
	MaxHeaderBytes    int           `env:"HTTP_MAX_HEADER_BYTES" envDefault:"1048576"`
}

// DatabaseConfig 描述 PostgreSQL 连接池参数。URL 必填且没有默认值。
//
// 池大小要按"服务端预算 ÷ 实例数"来定，而不是越大越好：本机开发栈的 Postgres 是多个
// 应用共享的，它只允许 50 条连接（其中 3 条留给超级用户）。默认值 10 意味着同一个
// Postgres 上最多能并排放四个本应用实例；实例更多时要往下调，而不是等连接被拒绝。
type DatabaseConfig struct {
	URL string `env:"DB_URL,required,notEmpty"`
	// MaxOpenConns 是连接池上限。0 在 database/sql 里表示"不限"，因此被 validate 拒绝。
	MaxOpenConns int `env:"DB_MAX_OPEN_CONNS" envDefault:"10"`
	// MaxIdleConns 默认与 MaxOpenConns 相等：低于它只会让连接反复建立又关闭，
	// 真正要回收空闲连接请用 ConnMaxIdleTime。
	MaxIdleConns int `env:"DB_MAX_IDLE_CONNS" envDefault:"10"`
	// ConnMaxLifetime 在连接被服务端或中间件回收前主动换掉它，避免拿到一条已死的连接。
	ConnMaxLifetime time.Duration `env:"DB_CONN_MAX_LIFETIME" envDefault:"30m"`
	// ConnMaxIdleTime 把空闲连接还给服务端：空闲连接同样占用服务端的连接槽位与内存。
	ConnMaxIdleTime time.Duration `env:"DB_CONN_MAX_IDLE_TIME" envDefault:"5m"`
	// ConnectTimeout 限制建立单条连接的时间，避免服务端不可达时请求一直挂在拨号上。
	ConnectTimeout time.Duration `env:"DB_CONNECT_TIMEOUT" envDefault:"5s"`
	// StatementTimeout 是会话级的查询超时：一条跑飞的 SQL 在池里就是一个被长期占住的
	// 连接槽位，服务端不会替你收回。显式下发是为了让口径不随部署环境漂移——同一台库的
	// 全局默认可能被 DBA 设成别的值（本机是 1min，比这里的 30s 宽）。
	// 0 表示不下发这个参数，沿用服务端设置。
	StatementTimeout time.Duration `env:"DB_STATEMENT_TIMEOUT" envDefault:"30s"`
	// IdleInTxTimeout 掐掉"开着事务却不动"的会话：它持有快照与锁，是最典型的连接泄漏形态。
	// 0 表示不下发这个参数，沿用服务端设置（本机是 2min）。
	IdleInTxTimeout time.Duration `env:"DB_IDLE_IN_TX_TIMEOUT" envDefault:"60s"`
}

// CacheConfig 目前只承载令牌黑名单与限流计数。URL 必填且没有默认值。
type CacheConfig struct {
	URL string `env:"REDIS_URL,required,notEmpty"`
}

// 存储驱动名，也是 STORAGE_DRIVER 仅有的两个合法取值。storage.Open 按它分派。
const (
	StorageDriverLocal = "local"
	StorageDriverS3    = "s3"
)

// StorageConfig 描述对象存储的驱动选择与各驱动的参数。
//
// 驱动是单选而非回退链：配了 s3 就不会因为本地目录恰好存在而静默改走本地。
type StorageConfig struct {
	Driver string `env:"STORAGE_DRIVER" envDefault:"local"`
	Local  LocalStorageConfig
	S3     S3StorageConfig
}

// LocalStorageConfig 描述本地文件系统驱动。
type LocalStorageConfig struct {
	// Root 是本地对象的物理根目录，所有读写都被限制在它之下。
	// 默认取 storage/app 下的专用子目录：storage/app 里还留着从 lunchbox 拷来的
	// saml/sp.key、ip2region_v4.xdb 等与本项目无关的文件，对象不该与它们共享命名空间
	// ——否则一旦把公开地址前缀指向这个根目录，那些文件会一并变成可下载的。
	Root string `env:"STORAGE_LOCAL_ROOT" envDefault:"./storage/app/uploads"`
	// URL 是该根目录对外的公开地址前缀，由部署环境的静态服务（Caddy / Nginx）提供。
	// 留空表示没有公开地址，此时 storage.Disk.URL 返回 storage.ErrNoPublicURL。
	URL string `env:"STORAGE_LOCAL_URL"`
}

// S3StorageConfig 描述 S3 兼容对象存储，适用 AWS S3、MinIO、Garage、Cloudflare R2 与阿里云 OSS。
type S3StorageConfig struct {
	Endpoint string `env:"S3_ENDPOINT"`
	Bucket   string `env:"S3_BUCKET"`
	// Region 多数兼容实现不校验，AWS 必须给出正确的桶区域。
	// 本机 garage 的 garage.toml 里是 s3_region = "garage"，签名范围会带上它，必须对齐。
	Region string `env:"S3_REGION" envDefault:"us-east-1"`
	// 凭证没有默认值；驱动选 s3 时由 validate 要求它们在位（见 validate 的条件必填）。
	AccessKeyID     string `env:"S3_ACCESS_KEY_ID"`
	SecretAccessKey string `env:"S3_SECRET_ACCESS_KEY"`
	UseSSL          bool   `env:"S3_USE_SSL" envDefault:"true"`
	// PublicURL 是桶对外的公开地址前缀（CDN 或反向代理）。留空时按 endpoint 与桶名推导，
	// 那条地址只在桶允许匿名读取时可用。
	PublicURL string `env:"S3_PUBLIC_URL"`
}

// JWTConfig 描述双令牌的签名与寿命参数。Secret 必填且不得短于 32 字符。
type JWTConfig struct {
	Secret string `env:"JWT_SECRET,required,notEmpty"`
	Issuer string `env:"JWT_ISSUER" envDefault:"bgin"`
	// 两个 TTL 的取值与相对关系（refresh 必须大于 access）都在 validate 里校验。
	AccessTTL  time.Duration `env:"JWT_ACCESS_TTL" envDefault:"15m"`
	RefreshTTL time.Duration `env:"JWT_REFRESH_TTL" envDefault:"720h"`
}

// MailConfig 描述 SMTP 发信出口。
type MailConfig struct {
	URL         string `env:"MAIL_URL" envDefault:"smtp://127.0.0.1:1025"`
	FromAddress string `env:"MAIL_FROM_ADDRESS" envDefault:"no-reply@bgin.local"`
	FromName    string `env:"MAIL_FROM_NAME" envDefault:"bgin"`
}

// I18nConfig 描述校验与邮件文案的默认语言。
type I18nConfig struct {
	DefaultLanguage string `env:"I18N_DEFAULT_LANGUAGE" envDefault:"en"`
}

// LogConfig 描述日志级别与输出格式。
type LogConfig struct {
	Level  string `env:"LOG_LEVEL" envDefault:"info"`
	Output string `env:"LOG_OUTPUT" envDefault:"console"`
}

// AuthConfig 描述认证策略开关与一次性凭据寿命。
type AuthConfig struct {
	RequireEmailVerification bool          `env:"REQUIRE_EMAIL_VERIFICATION" envDefault:"false"`
	OTPTTL                   time.Duration `env:"OTP_TTL" envDefault:"10m"`
	OTPMaxAttempts           int           `env:"OTP_MAX_ATTEMPTS" envDefault:"5"`
	PasswordResetTTL         time.Duration `env:"PASSWORD_RESET_TTL" envDefault:"30m"`
}

// LimiterConfig 描述三档限流配额（每分钟）。填 0 表示该档不限制。
type LimiterConfig struct {
	Anonymous     int `env:"RATELIMIT_ANONYMOUS" envDefault:"60"`
	Authenticated int `env:"RATELIMIT_AUTHENTICATED" envDefault:"300"`
	Sensitive     int `env:"RATELIMIT_SENSITIVE" envDefault:"10"`
}

// CORSConfig 描述跨域放行策略。
type CORSConfig struct {
	AllowedOrigins []string `env:"CORS_ALLOWED_ORIGINS" envDefault:"*"`
}

// SeedConfig 仅供 db:seed 创建首个管理员使用，两个关键值没有默认值。
type SeedConfig struct {
	AdminEmail    string `env:"SEED_ADMIN_EMAIL"`
	AdminPassword string `env:"SEED_ADMIN_PASSWORD"`
	AdminName     string `env:"SEED_ADMIN_NAME" envDefault:"Administrator"`
}

// knownEnvs 是 APP_ENV 的合法取值。它同时决定 gin 的运行模式与调试输出的宽松程度。
//
// 拼错的值不能被容忍：它会被静默当成"非本地、非生产"——本地的调试便利与生产的收敛性
// 同时失效。test 由集成测试置入（tests/integration/support_test.go）。
var knownEnvs = []string{"local", "development", "test", "staging", "production"}

// validate 承担全部语义约束：取值白名单、长度下限、跨字段关系，以及"随驱动变化的必填项"。
//
// 这些规则落在这里而不是字段标签上，是因为 caarlos0/env 不支持 validate 标签（写了也不
// 生效，见包注释）；它只提供 required 这类存在性约束，那条已经写在字段上。
//
// 按配置段拆成几个方法：每段各自成篇，读的人不必在一个函数里跨过无关的规则。
func (c *Config) validate() error {
	// 环境标识最先校验：它决定 gin 模式与调试输出的宽松程度，其余取值都建立在它合法之上。
	if !slices.Contains(knownEnvs, c.App.Env) {
		return fmt.Errorf("config: APP_ENV=%q is not a valid value, options: %s", c.App.Env, strings.Join(knownEnvs, " / "))
	}

	if err := c.validateDatabase(); err != nil {
		return err
	}
	if err := c.validateJWT(); err != nil {
		return err
	}
	if err := c.validateServer(); err != nil {
		return err
	}
	return c.validateStorage()
}

// validateServer 检查 HTTP 传输层超时与请求头上限约束。
func (c *Config) validateServer() error {
	if c.App.Server.ReadHeaderTimeout <= 0 || c.App.Server.ReadTimeout <= 0 ||
		c.App.Server.WriteTimeout <= 0 || c.App.Server.ShutdownTimeout <= 0 {
		return errors.New("config: HTTP server timeouts must be positive")
	}
	if c.App.Server.MaxHeaderBytes <= 0 {
		return errors.New("config: HTTP_MAX_HEADER_BYTES must be positive")
	}
	return nil
}

// validateDatabase 检查连接池的取值约束，以及两个会话级超时的区间。
func (c *Config) validateDatabase() error {
	// 这三条都不会报错，只会静默变成另一种行为：database/sql 把 0 当"不限"、
	// 把 MaxIdleConns 静默钳到 MaxOpenConns。在共享的 Postgres 上，
	// "不限"意味着别的应用连不进来。
	if c.Database.MaxOpenConns <= 0 {
		return errors.New("config: DB_MAX_OPEN_CONNS must be positive")
	}
	if c.Database.MaxIdleConns < 0 || c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		return fmt.Errorf("config: DB_MAX_IDLE_CONNS must be between 0 and DB_MAX_OPEN_CONNS(%d), got %d",
			c.Database.MaxOpenConns, c.Database.MaxIdleConns)
	}
	if c.Database.ConnectTimeout <= 0 {
		return errors.New("config: DB_CONNECT_TIMEOUT must be positive")
	}

	// 两个会话级超时：0 表示沿用服务端设置，合法；负值与"非零但不足 1ms"都要拦下——
	// 前者会被服务端拒绝，后者下发时被截断成 0，等于把超时悄悄关掉。
	for _, item := range []struct {
		name  string
		value time.Duration
	}{
		{"DB_STATEMENT_TIMEOUT", c.Database.StatementTimeout},
		{"DB_IDLE_IN_TX_TIMEOUT", c.Database.IdleInTxTimeout},
	} {
		if item.value < 0 || (item.value > 0 && item.value < time.Millisecond) {
			return fmt.Errorf("config: %s must be 0 (server default) or at least 1ms, got %s", item.name, item.value)
		}
	}

	return nil
}

// validateJWT 检查密钥强度与两个有效期的关系。
func (c *Config) validateJWT() error {
	if len(c.JWT.Secret) < 32 {
		return fmt.Errorf("config: JWT_SECRET must be at least 32 characters, got %d", len(c.JWT.Secret))
	}
	if c.JWT.AccessTTL <= 0 || c.JWT.RefreshTTL <= 0 {
		return errors.New("config: JWT_ACCESS_TTL and JWT_REFRESH_TTL must be positive")
	}
	if c.JWT.RefreshTTL <= c.JWT.AccessTTL {
		return errors.New("config: JWT_REFRESH_TTL must be greater than JWT_ACCESS_TTL")
	}

	return nil
}

// validateStorage 检查驱动取值与随之变化的必填项：local 要根目录，s3 要端点、桶与凭证。
func (c *Config) validateStorage() error {
	// required 标签是无条件的，表达不了"必填项随驱动变化"这种关系，只能按驱动分支。
	switch c.Storage.Driver {
	case StorageDriverLocal:
		if strings.TrimSpace(c.Storage.Local.Root) == "" {
			return errors.New("config: STORAGE_LOCAL_ROOT cannot be empty when STORAGE_DRIVER=local")
		}
	case StorageDriverS3:
		for _, item := range []struct{ name, value string }{
			{"S3_ENDPOINT", c.Storage.S3.Endpoint},
			{"S3_BUCKET", c.Storage.S3.Bucket},
			{"S3_ACCESS_KEY_ID", c.Storage.S3.AccessKeyID},
			{"S3_SECRET_ACCESS_KEY", c.Storage.S3.SecretAccessKey},
		} {
			if strings.TrimSpace(item.value) == "" {
				return fmt.Errorf("config: %s is required when STORAGE_DRIVER=s3", item.name)
			}
		}
	default:
		return fmt.Errorf("config: STORAGE_DRIVER=%q is not a valid value, options: %s / %s",
			c.Storage.Driver, StorageDriverLocal, StorageDriverS3)
	}

	return nil
}

// gin 的运行模式取值，与 gin.DebugMode / gin.ReleaseMode / gin.TestMode 一致。
// 这里用普通字符串而非 gin 常量，是为了不让配置层依赖 HTTP 框架。
const (
	ModeDebug   = "debug"
	ModeRelease = "release"
	ModeTest    = "test"
)

// ResolveGinMode 返回 gin 应当运行的模式。
//
// 必须由配置层读取后再显式应用，不能交给 gin 自己去读 GIN_MODE：gin 在包 init()
// 阶段读取该变量，早于 .env 被装载，会让同一份配置在不同启动方式下给出不同结果
// （经 just 启动时 just 会把变量导出给子进程而生效，直接跑二进制则不生效）。
//
// 未知的 APP_ENV 一律 release，避免把完整路由表打进非本地环境的日志。
func (c *Config) ResolveGinMode() string {
	if c.App.GinMode != "" {
		return c.App.GinMode
	}
	if c.IsLocal() {
		return ModeDebug
	}
	return ModeRelease
}

// IsLocal 报告进程是否运行在本地开发环境。只有它对应的环境允许暴露调试输出。
func (c *Config) IsLocal() bool {
	return c.App.Env == "local" || c.App.Env == "development"
}

// IsProduction 报告进程是否运行在生产环境，供需要区分部署环境的逻辑使用。
//
// 它不参与错误响应的脱敏：那由 errs.Map 的兜底分支无条件保证，任何未被识别的错误
// 都只返回通用文案，与运行环境无关。
func (c *Config) IsProduction() bool { return c.App.Env == "production" }
