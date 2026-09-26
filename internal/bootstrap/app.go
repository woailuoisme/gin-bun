// Package bootstrap 把依赖装配成一个可运行的应用，并负责它的运行与收尾。
//
// 命令行界面在 bgin/internal/console 里，依赖方向是 console → bootstrap。
package bootstrap

// 本文件只回答两个问题：装配完成后的应用是什么，以及它怎么收尾。
// 组装过程见 wire.go，HTTP 引擎与路由见 engine.go，服务的运行与停机见 server.go。

import (
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/uptrace/bun"

	"bgin/internal/config"
	"bgin/internal/platform/cache"
	"bgin/internal/platform/mailer"
	"bgin/internal/platform/storage"
)

// Application 聚合装配完成的依赖与 HTTP 引擎。
//
// 字段一律不导出：外部只能经 Engine 取路由、经 Serve 启服务，依赖不会被绕开装配层取用。
type Application struct {
	cfg     *config.Config
	logger  *zerolog.Logger
	db      *bun.DB
	cache   *cache.Client
	mailer  *mailer.Mailer
	storage storage.Disk
	engine  *gin.Engine
}

// Engine 暴露已装配的路由引擎，供集成测试直接驱动请求而不必真的监听端口。
func (a *Application) Engine() *gin.Engine { return a.engine }

// Close 释放全部外部资源。
//
// 三者互不依赖，无需讲究关闭顺序；这里只保证每个失败都留下日志——调用方在退出路径上，
// 已经没有更合适的处理位置了。
func (a *Application) Close() {
	if err := a.mailer.Close(); err != nil {
		a.logger.Warn().Err(err).Msg("关闭 SMTP 连接失败")
	}
	if err := a.cache.Close(); err != nil {
		a.logger.Warn().Err(err).Msg("关闭 Redis 连接失败")
	}
	if err := a.db.Close(); err != nil {
		a.logger.Warn().Err(err).Msg("关闭数据库连接失败")
	}
}
