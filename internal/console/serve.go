package console

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"

	"bgin/internal/bootstrap"
)

// serveCommand 启动 HTTP 服务，对应 .air.toml 的 args_bin。
func serveCommand() *cli.Command {
	return &cli.Command{
		Name:  "serve",
		Usage: "启动 HTTP 服务",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "port", Aliases: []string{"p"}, Usage: "覆盖 APP_PORT，0 表示使用配置值"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			application, err := bootstrap.NewApplication(ctx, cmd.Int("port"))
			if err != nil {
				return err
			}
			defer application.Close()

			return application.Serve(ctx)
		},
	}
}
