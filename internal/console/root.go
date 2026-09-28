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

	// 注册四大业务命令分类组（Cobra Group）
	groupServer := &cobra.Group{
		ID:    "server",
		Title: "Server & Worker Commands:",
	}
	groupDB := &cobra.Group{
		ID:    "database",
		Title: "Database Commands:",
	}
	groupSearch := &cobra.Group{
		ID:    "search",
		Title: "Search Engine Commands:",
	}
	groupSecurity := &cobra.Group{
		ID:    "security",
		Title: "Security & Utility Commands:",
	}
	groupMake := &cobra.Group{
		ID:    "make",
		Title: "Code Generation Commands:",
	}

	cmd.AddGroup(groupServer, groupDB, groupSearch, groupSecurity, groupMake)

	// 构建领域一级命令并分配分组
	serveCmd := newServeCmd()
	serveCmd.GroupID = groupServer.ID

	queueCmd := newQueueCmd()
	queueCmd.GroupID = groupServer.ID

	routeCmd := newRouteCmd()
	routeCmd.GroupID = groupServer.ID

	dbCmd := newDBCmd()
	dbCmd.GroupID = groupDB.ID

	scoutCmd := newScoutCmd()
	scoutCmd.GroupID = groupSearch.ID

	jwtCmd := newJWTCmd()
	jwtCmd.GroupID = groupSecurity.ID

	makeCmd := newMakeCmd()
	makeCmd.GroupID = groupMake.ID

	// 挂载现代层级子命令
	cmd.AddCommand(
		serveCmd,
		queueCmd,
		routeCmd,
		dbCmd,
		scoutCmd,
		jwtCmd,
		makeCmd,
	)

	// 挂载隐藏的顶层冒号命令别名（兼容 Artisan / 老脚本调用习惯）
	cmd.AddCommand(
		newRouteListAliasCmd(),
		newJWTSecretAliasCmd(),
		newDBMigrateAliasCmd(),
		newDBSeedAliasCmd(),
		newDBFreshAliasCmd(),
		newQueueWorkAliasCmd(),
		newQueueFailedAliasCmd(),
		newQueueRetryAliasCmd(),
		newQueueFlushAliasCmd(),
		newScoutImportAliasCmd(),
		newScoutFlushAliasCmd(),
		newMakeCRUDAliasCmd(),
		newMakeDestroyAliasCmd(),
	)

	return cmd
}
