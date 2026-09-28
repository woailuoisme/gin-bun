package console

import (
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"bgin/internal/bootstrap"
)

// newServeCmd 启动 HTTP 服务，对应 .air.toml 的 args_bin。
func newServeCmd() *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			application, err := bootstrap.NewApplication(ctx, port)
			if err != nil {
				return err
			}
			defer application.Close()

			return application.Serve(ctx)
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 0, "Override APP_PORT (0 to use configured value)")

	return cmd
}
