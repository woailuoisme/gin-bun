// Package console 是 bgin 的命令行界面：命令定义、参数解析与终端输出。
//
// 应用的装配与运行在 bgin/internal/bootstrap，命令只做参数校验与编排，
// 依赖方向是 console → bootstrap（见 ADR-0002）。
package console

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
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
	return NewRootCommand().Run(ctx, args)
}

// NewRootCommand 组装根命令与全部子命令。
func NewRootCommand() *cli.Command {
	return &cli.Command{
		Name:                  "bgin",
		Usage:                 "bgin 应用控制台",
		Version:               fmt.Sprintf("%s (commit %s, built %s)", version, commit, date),
		Suggest:               true,
		EnableShellCompletion: true,
		Commands: []*cli.Command{
			serveCommand(),
			dbMigrateCommand(),
			dbSeedCommand(),
			dbFreshCommand(),
		},
	}
}
