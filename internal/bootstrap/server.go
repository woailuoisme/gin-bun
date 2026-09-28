package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// serverURL 构造本地服务访问 URL。
func serverURL(port int) string {
	return fmt.Sprintf("http://localhost:%d", port)
}

// Serve 阻塞式启动 HTTP 服务，收到 ctx 取消信号后执行优雅停机。
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
		if a.cfg.App.EnableDocs {
			event = event.Str("docs", base+docsPagePath)
		}
		event.Msg("HTTP server started and listening")

		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("HTTP server exited unexpectedly: %w", err)
	case <-ctx.Done():
		a.logger.Info().Msg("shutdown signal received, initiating graceful shutdown")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), srvCfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}
	return nil
}
