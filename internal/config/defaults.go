package config

import (
	"reflect"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// setDefaults 集中注册所有配置项的开箱即用默认值。
func setDefaults(v *viper.Viper) {
	// App
	v.SetDefault("app_name", "bgin")
	v.SetDefault("app_env", "local")
	v.SetDefault("app_debug", false)
	v.SetDefault("app_port", 3300)
	v.SetDefault("enable_docs", false)
	v.SetDefault("app_url", "http://localhost:3300")
	v.SetDefault("app_timezone", "UTC")

	// HTTP Server
	v.SetDefault("http_read_header_timeout", 10*time.Second)
	v.SetDefault("http_read_timeout", 15*time.Second)
	v.SetDefault("http_write_timeout", 35*time.Second)
	v.SetDefault("http_idle_timeout", 120*time.Second)
	v.SetDefault("http_shutdown_timeout", 15*time.Second)
	v.SetDefault("http_max_header_bytes", 1048576)
	v.SetDefault("http_request_timeout", 15*time.Second)

	// Database
	v.SetDefault("db_max_open_conns", 10)
	v.SetDefault("db_max_idle_conns", 10)
	v.SetDefault("db_conn_max_lifetime", 30*time.Minute)
	v.SetDefault("db_conn_max_idle_time", 5*time.Minute)
	v.SetDefault("db_connect_timeout", 5*time.Second)
	v.SetDefault("db_statement_timeout", 30*time.Second)
	v.SetDefault("db_idle_in_tx_timeout", 60*time.Second)
	v.SetDefault("db_slow_threshold", 200*time.Millisecond)
	v.SetDefault("db_debug", false)

	// Seed
	v.SetDefault("seed_admin_name", "Administrator")

	// Storage
	v.SetDefault("filesystem_disk", "local")
	v.SetDefault("storage_local_root", "./storage/app/uploads")
	v.SetDefault("aws_default_region", "us-east-1")
	v.SetDefault("aws_use_ssl", true)

	// JWT
	v.SetDefault("jwt_issuer", "bgin")
	v.SetDefault("jwt_ttl", 15*time.Minute)
	v.SetDefault("jwt_refresh_ttl", 720*time.Hour)

	// Mail
	v.SetDefault("mail_driver", "mailpit")
	v.SetDefault("mail_from_address", "no-reply@bgin.local")
	v.SetDefault("mail_from_name", "bgin")
	v.SetDefault("mailpit_host", "127.0.0.1")
	v.SetDefault("mailpit_port", 1025)
	v.SetDefault("resend_base_url", "https://api.resend.com")

	// I18n
	v.SetDefault("app_locale", "en")

	// Log
	v.SetDefault("log_level", "info")
	v.SetDefault("log_channel", "console")

	// Auth
	v.SetDefault("require_email_verification", false)
	v.SetDefault("otp_ttl", 10*time.Minute)
	v.SetDefault("otp_max_attempts", 5)
	v.SetDefault("password_reset_ttl", 30*time.Minute)

	// Limiter
	v.SetDefault("ratelimit_anonymous", 60)
	v.SetDefault("ratelimit_authenticated", 300)
	v.SetDefault("ratelimit_sensitive", 10)

	// Centrifugo (Realtime)
	v.SetDefault("centrifugo_enabled", false)
	v.SetDefault("centrifugo_token_ttl", 2*time.Hour)

	// EventBus
	v.SetDefault("eventbus_driver", "go_channel")
	v.SetDefault("eventbus_topic_prefix", "bgin")
	v.SetDefault("eventbus_log_level", "warn")
	v.SetDefault("eventbus_buffer_size", 2048)

	// Media
	v.SetDefault("media_max_file_size", int64(20971520))
	v.SetDefault("media_default_collection", "default")
	v.SetDefault("imgproxy_enabled", false)
	v.SetDefault("imgproxy_url", "http://localhost:8088")
	v.SetDefault("imgproxy_insecure", false)
	v.SetDefault("imgproxy_default_format", "webp")
	v.SetDefault("imgproxy_default_quality", 85)

	// Notification
	v.SetDefault("notification_worker_pool", 10)
	v.SetDefault("notification_queue_size", 100)
	v.SetDefault("notification_gotify_priority", 5)
	// notification_sms_driver 故意不设默认值：非生产环境由通知装配层回落到 log 驱动，
	// 生产环境则归一为 none。若在此填入 "log"，生产部署会带着日志驱动启动并把验证码写进日志。
	v.SetDefault("notification_sms_tencent_region", "ap-guangzhou")
	v.SetDefault("notification_telegram_api_url", "https://api.telegram.org")

	// Payment
	v.SetDefault("payment_fake_enabled", false)
	v.SetDefault("payment_certs_dir", "./storage/app/certs")

	// Queue
	v.SetDefault("queue_concurrency", 10)
	v.SetDefault("queue_shutdown_timeout", 10*time.Second)

	// Scout
	v.SetDefault("scout_driver", "noop")
	v.SetDefault("meilisearch_host", "http://127.0.0.1:7700")

	// Services
	v.SetDefault("geoip_enabled", false)
	v.SetDefault("geoip_db_path", "./storage/app/geoip/ip2region_v4.xdb")
	v.SetDefault("excel_temp_path", ".cache/excel")
	v.SetDefault("pdf_font_path", "./storage/fonts/SourceHanSans.ttf")
	v.SetDefault("pdf_font_family", "SourceHanSans")
	v.SetDefault("pdf_page_size", "A4")
	v.SetDefault("pdf_margin", 10.0)
}

// bindEnvKeys 自动递归提取 Config 结构体中声明的所有配置键并显式绑定对应的环境变量。
func bindEnvKeys(v *viper.Viper) {
	bindStructFields(v, reflect.TypeFor[Config]())
}

func bindStructFields(v *viper.Viper, t reflect.Type) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for field := range t.Fields() {
		tag := field.Tag.Get("mapstructure")
		if tag == "" || tag == "-" {
			if field.Type.Kind() == reflect.Struct {
				bindStructFields(v, field.Type)
			}
			continue
		}
		if tag == ",squash" || strings.HasSuffix(tag, ",squash") {
			bindStructFields(v, field.Type)
			continue
		}
		key, _, _ := strings.Cut(tag, ",")
		if key != "" {
			envName := strings.ToUpper(key)
			_ = v.BindEnv(key, envName)
		}
	}
}

// stringToSliceHookFunc 将逗号分隔的字符串转换为 []string 切片，空字符串安全转换为由空切片。
func stringToSliceHookFunc(sep string) mapstructure.DecodeHookFunc {
	return func(f reflect.Type, t reflect.Type, data any) (any, error) {
		if f.Kind() != reflect.String || t != reflect.TypeFor[[]string]() {
			return data, nil
		}
		raw, ok := data.(string)
		if !ok {
			return data, nil
		}
		if raw == "" {
			return []string{}, nil
		}
		return strings.Split(raw, sep), nil
	}
}
