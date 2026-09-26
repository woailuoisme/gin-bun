package console

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"charm.land/huh/v2"
	"github.com/uptrace/bun"
	"github.com/urfave/cli/v3"

	"bgin/internal/config"
	"bgin/internal/database"
	"bgin/internal/database/model"
	"bgin/internal/platform/hasher"
	"bgin/internal/platform/validator"
	"bgin/internal/shared/user"
)

// dbMigrateCommand 委托 atlas 应用版本式迁移。
// 迁移文件由 `atlas migrate diff` 依据 schema.sql 生成，因此这里不自行实现迁移器，
// 也不去伪造 atlas 的版本记账表（见 ADR-0004）。
func dbMigrateCommand() *cli.Command {
	return &cli.Command{
		Name:  "db:migrate",
		Usage: "应用迁移（委托 atlas migrate apply）",
		Action: func(ctx context.Context, _ *cli.Command) error {
			return runAtlas(ctx, "migrate", "apply", "--env", "app")
		},
	}
}

// dbSeedCommand 创建首个管理员账号，幂等：已存在则跳过。
func dbSeedCommand() *cli.Command {
	return &cli.Command{
		Name:  "db:seed",
		Usage: "创建首个管理员账号（幂等）",
		Action: func(ctx context.Context, _ *cli.Command) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			db, err := database.Open(ctx, cfg.Database, cfg.App.Name)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			return seedAdmin(ctx, cfg, db)
		},
	}
}

// dbFreshCommand 清空数据库后重放迁移并写入种子数据。破坏性操作默认需要交互确认。
func dbFreshCommand() *cli.Command {
	return &cli.Command{
		Name:  "db:fresh",
		Usage: "清空数据库、重放迁移并写入种子数据（破坏性）",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "force", Usage: "跳过交互确认，仅供自动化使用"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if !cmd.Bool("force") {
				confirmed, err := confirmFresh(cfg)
				if err != nil || !confirmed {
					warn("已取消")
					return err
				}
			}

			db, err := database.Open(ctx, cfg.Database, cfg.App.Name)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			title("正在重建 public schema…")
			if _, err := db.ExecContext(ctx, "DROP SCHEMA IF EXISTS public CASCADE"); err != nil {
				return fmt.Errorf("删除 public schema 失败: %w", err)
			}
			if _, err := db.ExecContext(ctx, "CREATE SCHEMA public"); err != nil {
				return fmt.Errorf("重建 public schema 失败: %w", err)
			}

			title("正在重放迁移…")
			if err := runAtlas(ctx, "migrate", "apply", "--env", "app"); err != nil {
				return err
			}

			title("正在写入种子数据…")
			return seedAdmin(ctx, cfg, db)
		},
	}
}

// seedAdmin 创建首个管理员。凭据来自环境变量，绝不硬编码（Q28）。
func seedAdmin(ctx context.Context, cfg *config.Config, db *bun.DB) error {
	if cfg.Seed.AdminEmail == "" || cfg.Seed.AdminPassword == "" {
		return errors.New("请先设置 SEED_ADMIN_EMAIL 与 SEED_ADMIN_PASSWORD")
	}
	if err := validator.PasswordStrength("password", cfg.Seed.AdminPassword); err != nil {
		return fmt.Errorf("SEED_ADMIN_PASSWORD 不满足口令强度要求: %w", err)
	}

	repo := user.NewRepository(db)
	_, err := repo.FindByEmail(ctx, cfg.Seed.AdminEmail)
	switch {
	case err == nil:
		warn("管理员 %s 已存在，跳过", cfg.Seed.AdminEmail)
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("查询管理员失败: %w", err)
	}

	hasherClient := hasher.New()
	hash, err := hasherClient.Make(cfg.Seed.AdminPassword)
	if err != nil {
		return err
	}

	// 变量刻意不叫 user，避免遮蔽上面引入的 user 包名。
	administrator := &model.User{
		Name:          cfg.Seed.AdminName,
		Email:         strings.ToLower(strings.TrimSpace(cfg.Seed.AdminEmail)),
		EmailVerified: true,
		Role:          model.RoleAdmin,
		Status:        model.StatusActive,
	}
	if err := repo.CreateWithCredential(ctx, administrator, hash); err != nil {
		return fmt.Errorf("创建管理员失败: %w", err)
	}

	success("管理员 %s 已创建（id=%s）", administrator.Email, administrator.ID)
	return nil
}

// confirmFresh 用 huh 收集破坏性操作的确认。
func confirmFresh(cfg *config.Config) (bool, error) {
	confirmed := false
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("确认清空数据库？").
			Description(fmt.Sprintf("将删除 %s 的全部数据并重放迁移，此操作不可撤销。", cfg.App.Name)).
			Affirmative("确认清空").
			Negative("取消").
			Value(&confirmed),
	))

	if err := form.Run(); err != nil {
		return false, fmt.Errorf("读取确认失败: %w", err)
	}
	return confirmed, nil
}

// runAtlas 以当前工作目录为基准调用 atlas 命令行工具，输出直接透传给用户。
func runAtlas(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "atlas", args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Stdin = os.Stdin

	if err := command.Run(); err != nil {
		return fmt.Errorf("执行 atlas %s 失败（请确认已安装 atlas 且已配置 .env）: %w", strings.Join(args, " "), err)
	}
	return nil
}
