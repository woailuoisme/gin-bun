// Package bootstrap 负责应用依赖装配与生命周期管理。
package bootstrap

import (
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/uptrace/bun"

	"bgin/internal/config"
	"bgin/internal/platform/cache"
	"bgin/internal/platform/mailer"
	"bgin/internal/platform/media"
	"bgin/internal/platform/notification"
	"bgin/internal/platform/payment"
	"bgin/internal/platform/pdf"
	"bgin/internal/platform/storage"
	"bgin/internal/platform/wechat"
)

// Application 聚合装配完成的系统依赖与 HTTP 引擎。
type Application struct {
	cfg          *config.Config
	logger       *zerolog.Logger
	db           *bun.DB
	cache        *cache.Client
	mailer       mailer.Mailer
	notification *notification.Notifier
	storage      storage.Disk
	media        *media.Manager
	pdf          *pdf.Service
	wechat       *wechat.Client
	payment      *payment.Manager
	engine       *gin.Engine
}

// Engine 暴露已装配的路由引擎，供集成测试直接驱动请求。
func (a *Application) Engine() *gin.Engine { return a.engine }

// Close 释放全部外部资源与客户端连接。
func (a *Application) Close() {
	if a.notification != nil {
		if err := a.notification.Close(); err != nil {
			a.logger.Warn().Err(err).Msg("failed to close notification worker pool")
		}
	}
	if err := a.mailer.Close(); err != nil {
		a.logger.Warn().Err(err).Msg("failed to close SMTP connection")
	}
	if err := a.cache.Close(); err != nil {
		a.logger.Warn().Err(err).Msg("failed to close Redis connection")
	}
	if err := a.db.Close(); err != nil {
		a.logger.Warn().Err(err).Msg("failed to close database connection")
	}
}
