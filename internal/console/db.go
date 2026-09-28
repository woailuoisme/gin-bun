package console

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/uptrace/bun"

	"bgin/internal/config"
	"bgin/internal/console/core"
	"bgin/internal/database"
	"bgin/internal/database/seeders"
)

// newDBCmd 组装数据库管理命令组。
func newDBCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database migrations, seeding, and management",
	}

	cmd.AddCommand(newDBMigrateCmd())
	cmd.AddCommand(newDBSeedCmd())
	cmd.AddCommand(newDBFreshCmd())

	return cmd
}

// newDBMigrateCmd 委托 atlas 应用版本式迁移。
func newDBMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply database migrations via Atlas (atlas migrate apply)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAtlas(cmd, "migrate", "apply", "--env", "app")
		},
	}
}

// newDBSeedCmd 调度运行数据播种器（支持通过 --class 指定单个 Seeder，缺省为 DatabaseSeeder）。
func newDBSeedCmd() *cobra.Command {
	var className string

	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Seed database with records (default: DatabaseSeeder)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			db, err := database.Open(cmd.Context(), cfg.Database, cfg.App.Name)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			if className == "" {
				className = "DatabaseSeeder"
			}

			targetSeeder, ok := seeders.Get(className)
			if !ok {
				available := seeders.List()
				return fmt.Errorf("seeder [%s] not found. Available seeders: %v", className, available)
			}

			core.Title(cmd, fmt.Sprintf("Seeding [%s]...", className))
			if err := runSeederWithProgress(cmd, db, targetSeeder); err != nil {
				return err
			}

			core.Success(cmd, "Database seeding completed successfully.")
			return nil
		},
	}

	cmd.Flags().StringVarP(&className, "class", "c", "DatabaseSeeder", "The class name of the seeder to run")
	_ = cmd.RegisterFlagCompletionFunc("class", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return seeders.List(), cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

// newDBFreshCmd 清空数据库后重放迁移并写入种子数据。破坏性操作默认需要交互确认。
func newDBFreshCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "fresh",
		Short: "Drop all tables, re-run migrations, and seed database (destructive)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if !force {
				confirmed, err := core.Confirm("Are you sure you want to fresh the database?",
					fmt.Sprintf("All data in %s will be permanently dropped and migrations will be re-run.", cfg.App.Name),
					"Yes, drop and re-migrate")
				if err != nil {
					return err
				}
				if !confirmed {
					core.Warn(cmd, "Database fresh operation cancelled.")
					return core.ErrCanceled
				}
			}

			db, err := database.Open(cmd.Context(), cfg.Database, cfg.App.Name)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			core.Title(cmd, "Rebuilding public schema...")
			if _, err := db.ExecContext(cmd.Context(), "DROP SCHEMA IF EXISTS public CASCADE"); err != nil {
				return fmt.Errorf("failed to drop public schema: %w", err)
			}
			if _, err := db.ExecContext(cmd.Context(), "CREATE SCHEMA public"); err != nil {
				return fmt.Errorf("failed to recreate public schema: %w", err)
			}

			core.Title(cmd, "Applying versioned migrations...")
			if err := runAtlas(cmd, "migrate", "apply", "--env", "app"); err != nil {
				return err
			}

			core.Title(cmd, "Seeding initial data via DatabaseSeeder...")
			if err := runSeederWithProgress(cmd, db, &seeders.DatabaseSeeder{}); err != nil {
				return err
			}

			core.Success(cmd, "Database refreshed and seeded successfully.")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, core.FlagForce, "f", false, core.UsageForceFlag)

	return cmd
}

// 兼容老 Artisan 风格冒号别名命令构造工厂（设为 Hidden: true 保持帮助信息精简）。

func newDBMigrateAliasCmd() *cobra.Command {
	c := newDBMigrateCmd()
	c.Use = "db:migrate"
	c.Hidden = true
	return c
}

func newDBSeedAliasCmd() *cobra.Command {
	c := newDBSeedCmd()
	c.Use = "db:seed"
	c.Hidden = true
	return c
}

func newDBFreshAliasCmd() *cobra.Command {
	c := newDBFreshCmd()
	c.Use = "db:fresh"
	c.Hidden = true
	return c
}

// runAtlas 以当前工作目录为基准调用 atlas 命令行工具，输出直接透传给用户。
func runAtlas(cmd *cobra.Command, args ...string) error {
	command := exec.CommandContext(cmd.Context(), "atlas", args...)
	command.Stdout = cmd.OutOrStdout()
	command.Stderr = cmd.ErrOrStderr()
	command.Stdin = os.Stdin

	if err := command.Run(); err != nil {
		return fmt.Errorf("failed to execute atlas %s (ensure atlas is installed and .env configured): %w", strings.Join(args, " "), err)
	}
	return nil
}

// runSeederWithProgress 带有控制台步骤展示与细粒度进度条调度的 Seeder 执行函数。
func runSeederWithProgress(cmd *cobra.Command, db *bun.DB, targetSeeder seeders.Seeder) error {
	var subBar *core.ProgressBar

	stepHook := func(name string, index, total int, duration time.Duration, done bool) {
		if !done {
			core.Info(cmd, "• [%d/%d] Seeding %s...", index, total, name)
		} else {
			if subBar != nil {
				subBar.Finish()
				subBar = nil
			}
			core.Success(cmd, "✓ [%d/%d] %s completed (%s)", index, total, name, duration.Round(time.Millisecond))
		}
	}

	subHook := func(current, total int64) {
		if total <= 0 {
			return
		}
		if subBar == nil {
			subBar = core.NewProgressBar(cmd, total, "Records")
		}
		subBar.Set(current)
	}

	ctx := seeders.WithProgressHook(cmd.Context(), stepHook)
	ctx = seeders.WithSubProgressHook(ctx, subHook)

	runner := seeders.NewRunner(db).WithStepHook(stepHook)
	err := runner.Call(ctx, targetSeeder)
	if subBar != nil {
		subBar.Finish()
	}
	return err
}
