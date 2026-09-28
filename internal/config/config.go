// Package config 负责从环境变量装载并校验应用程序的全局配置。
//
// 架构设计采用 Laravel 多文件配置规范组织：
//   - app.go: 进程环境与 HTTP 服务参数 (config/app.php)
//   - auth.go: 身份认证与凭据策略 (config/auth.php)
//   - jwt.go: 双令牌生命周期与签名 (config/jwt.php)
//   - database.go: 数据库连接池与播种 (config/database.php)
//   - cache.go: 缓存存储驱动 (config/cache.php)
//   - filesystems.go: 本地与对象存储磁盘 (config/filesystems.php)
//   - mail.go: SMTP 邮件服务 (config/mail.php)
//   - logging.go: 日志级别与输出通道 (config/logging.php)
//   - queue.go: 异步队列与任务并发 (config/queue.php)
//   - scout.go: 全文检索与索引驱动 (config/scout.php)
//   - broadcasting.go: Centrifugo 实时通信配置
//   - services.go: 第三方与平台扩展服务 (config/services.php)
//   - cors.go: 跨域访问控制 (config/cors.php)
//   - limiter.go: 请求速率限制 (Throttle)
//   - i18n.go: 国际化语言设置
//   - eventbus.go: 事件总线驱动
//   - media.go: 媒体处理与图片优化
//   - notification.go: 通知渠道与驱动
//   - payment.go: 支付渠道与应用配置
//   - wechat.go: 微信生态客户端配置
package config

// Config 汇总应用的所有配置模块。
type Config struct {
	App          AppConfig
	Database     DatabaseConfig
	Cache        CacheConfig
	Storage      StorageConfig
	JWT          JWTConfig
	Mail         MailConfig
	I18n         I18nConfig
	Log          LogConfig
	Auth         AuthConfig
	Limiter      LimiterConfig
	CORS         CORSConfig
	Seed         SeedConfig
	Queue        QueueConfig
	Search       SearchConfig
	Realtime     RealtimeConfig
	GeoIP        GeoIPConfig
	Excel        ExcelConfig
	WeChat       WeChatConfig
	Payment      PaymentConfig
	Notification NotificationConfig
	Media        MediaConfig
	PDF          PDFConfig
	EventBus     EventBusConfig
}

// validate 依次执行各模块的语义与有效性校验。
func (c *Config) validate() error {
	validators := []func() error{
		c.validateApp,
		c.validateServer,
		c.validateDatabase,
		c.validateCache,
		c.validateStorage,
		c.validateJWT,
		c.validateAuth,
		c.validateMail,
		c.validateLog,
		c.validateQueue,
		c.validateSearch,
		c.validateRealtime,
		c.validateServices,
		c.validateCORS,
		c.validateLimiter,
		c.validateI18n,
		c.validateWeChat,
		c.validatePayment,
		c.validateNotification,
		c.validateMedia,
		c.validateEventBus,
	}
	for _, fn := range validators {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}
