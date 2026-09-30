// Package bootstrap 负责应用依赖装配与生命周期管理。
package bootstrap

import (
	"github.com/gin-gonic/gin"
	"github.com/uptrace/bun"
	"go.uber.org/zap"

	"bgin/internal/config"
	"bgin/internal/platform/cache"
	"bgin/internal/platform/mailer"
	"bgin/internal/platform/notification"
	"bgin/internal/platform/payment"
	"bgin/internal/platform/pdf"
	"bgin/internal/platform/realtime"
	"bgin/internal/platform/search"
	"bgin/internal/platform/storage"
	"bgin/internal/platform/wechat"
	"bgin/internal/shared/media"
)

// Application 聚合装配完成的系统依赖与 HTTP 引擎。
type Application struct {
	cfg          *config.Config
	logger       *zap.Logger
	db           *bun.DB
	cache        *cache.Client
	locker       cache.Locker
	mailer       mailer.Mailer
	notification *notification.Notifier
	storage      storage.Disk
	media        *media.Manager
	pdf          *pdf.Service
	wechat       *wechat.Manager
	payment      *payment.Manager
	realtime     realtime.Manager
	search       search.Client
	engine       *gin.Engine
}

// Engine 暴露已装配的路由引擎，供集成测试直接驱动请求。
func (a *Application) Engine() *gin.Engine { return a.engine }

// WeChat 暴露已装配的微信多应用管理器。
func (a *Application) WeChat() *wechat.Manager { return a.wechat }

// Locker 暴露系统级分布式互斥锁管理器（支持看门狗自动续期与原子释放）。
func (a *Application) Locker() cache.Locker { return a.locker }

// Close 释放全部外部资源与客户端连接。
func (a *Application) Close() {
	if a.notification != nil {
		if err := a.notification.Close(); err != nil {
			a.logger.Warn("failed to close notification worker pool", zap.Error(err))
		}
	}
	if a.mailer != nil {
		if err := a.mailer.Close(); err != nil {
			a.logger.Warn("failed to close SMTP connection", zap.Error(err))
		}
	}
	if a.locker != nil {
		if err := a.locker.Close(); err != nil {
			a.logger.Warn("failed to close distributed locker", zap.Error(err))
		}
	}
	if a.cache != nil {
		if err := a.cache.Close(); err != nil {
			a.logger.Warn("failed to close Redis connection", zap.Error(err))
		}
	}
	if a.db != nil {
		if err := a.db.Close(); err != nil {
			a.logger.Warn("failed to close database connection", zap.Error(err))
		}
	}
}
