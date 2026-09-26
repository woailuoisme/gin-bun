// 本文件只负责"把已装好的引擎跑起来、并在退出信号到来时停下来"。
// 引擎怎么装见 engine.go，应用对象见 app.go，serve 这个命令行入口见 serve.go。
//
// 与 package 子句之间留空行，避免这段文件级说明被 go/doc 并进包注释。
package bootstrap

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// serverURL 拼出可直接粘贴进浏览器的服务地址。
//
// 主机名固定为 localhost：进程绑的是所有接口（真实绑定地址在 addr 字段里），
// 而这条日志的主要读者是本机开发者；容器部署请以端口映射为准。
func serverURL(port int) string {
	return fmt.Sprintf("http://localhost:%d", port)
}

// Serve 阻塞式启动 HTTP 服务，收到 ctx 取消后优雅停机。
func (a *Application) Serve(ctx context.Context) error {
	srvCfg := a.cfg.App.Server
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", a.cfg.App.Port),
		Handler:           a.engine,
		ReadHeaderTimeout: srvCfg.ReadHeaderTimeout,
		ReadTimeout:       srvCfg.ReadTimeout,
		WriteTimeout:      srvCfg.WriteTimeout,
		IdleTimeout:       srvCfg.IdleTimeout,
		MaxHeaderBytes:    srvCfg.MaxHeaderBytes,
	}

	errCh := make(chan error, 1)
	go func() {
		base := serverURL(a.cfg.App.Port)
		event := a.logger.Info().
			Str("addr", server.Addr).
			Str("url", base).
			Str("env", a.cfg.App.Env).
			Str("gin", gin.Mode()).
			Str("storage", a.cfg.Storage.Driver)
		// 文档链接只在开启时才给，避免日志里出现一个必然 404 的地址。
		if a.cfg.App.EnableDocs {
			event = event.Str("docs", base+docsPagePath)
		}
		event.Msg("HTTP 服务已启动")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("HTTP server exited unexpectedly: %w", err)
	case <-ctx.Done():
		a.logger.Info().Msg("收到退出信号，开始优雅停机")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), srvCfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}
	return nil
}
