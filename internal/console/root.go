// Package console 是 bgin 的命令行界面：命令定义、参数解析与终端输出。
//
// 应用的装配与运行在 bgin/internal/bootstrap，命令只做参数校验与编排，
// 依赖方向是 console → bootstrap（见 ADR-0002）。
package console

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/golang-module/carbon/v2"
	"github.com/spf13/cobra"

	"bgin/internal/config"
	"bgin/internal/console/core"
)

// 构建元信息，由链接期 -X 注入。
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// SetVersion 供 main 注入构建元信息。
func SetVersion(v, c, d string) {
	version, commit, date = v, c, d
}

// Execute 解析命令行并执行选中的命令。
func Execute(ctx context.Context, args []string) error {
	cmd := NewRootCommand()
	// 如果传入包含可执行程序名自身的 argv（如 os.Args），切掉程序名
	if len(args) > 0 && (args[0] == "bgin" || filepath.Base(args[0]) == filepath.Base(os.Args[0])) {
		args = args[1:]
	}
	cmd.SetArgs(args)
	return cmd.ExecuteContext(ctx)
}

// NewRootCommand 组装根命令与全部子命令。
func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "bgin",
		Short:         "bgin application console",
		Version:       fmt.Sprintf("%s (commit %s, built %s)", version, commit, date),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				// 刻意不阻断：route list、make:* 等命令无需完整配置即可工作。
				// 但配置损坏会让时区与语言静默退回默认值，必须让运维看见。
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: load config failed, timezone and locale fall back to defaults: %v\n", err)
				return nil
			}
			carbon.SetTimezone(cfg.App.Timezone)
			carbon.SetLocale(cfg.I18n.DefaultLanguage)
			return nil
		},
	}

	// 注册色彩丰富且对齐严密的终端帮助与用法模板
	core.SetupColoredHelp(cmd)

	// 注册以命名空间命名的业务命令分类组（Laravel Artisan 风格分组）
	groupDB := &cobra.Group{
		ID:    "db",
		Title: "db",
	}
	groupJWT := &cobra.Group{
		ID:    "jwt",
		Title: "jwt",
	}
	groupMake := &cobra.Group{
		ID:    "make",
		Title: "make",
	}
	groupQueue := &cobra.Group{
		ID:    "queue",
		Title: "queue",
	}
	groupRoute := &cobra.Group{
		ID:    "route",
		Title: "route",
	}
	groupScout := &cobra.Group{
		ID:    "scout",
		Title: "scout",
	}

	cmd.AddGroup(groupDB, groupJWT, groupMake, groupQueue, groupRoute, groupScout)

	// 通用与基础服务命令（无前缀，归属于根级 Available commands）
	serveCmd := newServeCmd()

	// 数据库命令（db:migrate, db:seed, db:fresh）
	dbMigrateCmd := newDBMigrateCmd()
	dbMigrateCmd.GroupID = groupDB.ID

	dbSeedCmd := newDBSeedCmd()
	dbSeedCmd.GroupID = groupDB.ID

	dbFreshCmd := newDBFreshCmd()
	dbFreshCmd.GroupID = groupDB.ID

	// JWT 安全凭据命令（jwt:secret）
	jwtSecretCmd := newJWTSecretCmd()
	jwtSecretCmd.GroupID = groupJWT.ID

	// 代码生成命令（make:crud）
	makeCRUDCmd := newMakeCRUDCmd()
	makeCRUDCmd.GroupID = groupMake.ID

	// 异步队列命令（queue:work, queue:failed, queue:retry, queue:flush）
	queueWorkCmd := newQueueWorkCmd()
	queueWorkCmd.GroupID = groupQueue.ID

	queueFailedCmd := newQueueFailedCmd()
	queueFailedCmd.GroupID = groupQueue.ID

	queueRetryCmd := newQueueRetryCmd()
	queueRetryCmd.GroupID = groupQueue.ID

	queueFlushCmd := newQueueFlushCmd()
	queueFlushCmd.GroupID = groupQueue.ID

	// 路由诊断命令（route:list）
	routeListCmd := newRouteListCmd()
	routeListCmd.GroupID = groupRoute.ID

	// 搜索引擎命令（scout:import, scout:flush）
	scoutImportCmd := newScoutImportCmd()
	scoutImportCmd.GroupID = groupScout.ID

	scoutFlushCmd := newScoutFlushCmd()
	scoutFlushCmd.GroupID = groupScout.ID

	// 挂载纯冒号 Artisan 风格一等公民命令
	cmd.AddCommand(
		serveCmd,
		dbMigrateCmd,
		dbSeedCmd,
		dbFreshCmd,
		jwtSecretCmd,
		makeCRUDCmd,
		queueWorkCmd,
		queueFailedCmd,
		queueRetryCmd,
		queueFlushCmd,
		routeListCmd,
		scoutImportCmd,
		scoutFlushCmd,
	)

	return cmd
}
