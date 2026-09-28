package console

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"bgin/internal/console/core"
)

const (
	scopeAdmin  = "admin"
	scopeClient = "client"
)

// newMakeCmd 组装代码生成相关的顶级命令 (bgin make)。
func newMakeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "make",
		Short: "Scaffold and generate code modules",
		Long:  "Commands for code generation and scaffolding (CRUD modules, boilerplate, etc.).",
	}

	cmd.AddCommand(newMakeCRUDCmd())
	cmd.AddCommand(newMakeDestroyCmd())

	return cmd
}

// newMakeCRUDCmd 构建 CRUD 模块生成子命令 (bgin make crud <name>)。
func newMakeCRUDCmd() *cobra.Command {
	var (
		client  bool
		force   bool
		dryRun  bool
		tag     string
		chinese string
	)

	cmd := &cobra.Command{
		Use:   "crud [name]",
		Short: "Generate a complete CRUD feature slice",
		Long: `Generate a complete production-grade CRUD feature slice following the Domain-First architecture.
Generates:
  - internal/features/[admin|client]/<name>/handler.go
  - internal/features/[admin|client]/<name>/repository.go
  - internal/features/[admin|client]/<name>/routes.go
  - internal/features/[admin|client]/<name>/service.go
  - internal/features/[admin|client]/<name>/request.go
  - internal/features/[admin|client]/<name>/response.go
  - internal/models/<name>.go (if not exists)

Examples:
  bgin make crud article
  bgin make crud article_category --tag "分类管理" --chinese "文章分类"
  bgin make crud post --client
  bgin make crud product --force
  bgin make crud order --dry-run`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && !core.IsInteractive() {
				return errors.New("accepts 1 arg(s), received 0. Usage: bgin make crud <name>")
			}
			if len(args) > 1 {
				return fmt.Errorf("accepts at most 1 arg(s), received %d", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				name string
				err  error
			)

			if len(args) > 0 {
				name = strings.TrimSpace(args[0])
			} else {
				name, err = promptMakeCRUD(&client, &tag, &chinese)
				if err != nil {
					return err
				}
			}

			opts := core.GeneratorOptions{
				Name:        name,
				IsAdmin:     !client,
				TagName:     tag,
				ChineseName: chinese,
				Force:       force,
				DryRun:      dryRun,
			}

			core.Title(cmd, fmt.Sprintf("Scaffolding CRUD module: %s (Admin=%v, DryRun=%v)", name, opts.IsAdmin, opts.DryRun))

			results, err := core.GenerateCRUD(opts)
			if err != nil {
				return err
			}

			var created, overwritten, skipped, dryRuns int

			for _, r := range results {
				switch r.Action {
				case core.ActionCreated:
					created++
					core.Success(cmd, "  + %-12s %s", r.Action, r.Path)
				case core.ActionOverwritten:
					overwritten++
					core.Warn(cmd, "  ! %-12s %s", r.Action, r.Path)
				case core.ActionSkipped:
					skipped++
					core.Info(cmd, "  - %-12s %s (%s)", r.Action, r.Path, r.Message)
				case core.ActionDryRun:
					dryRuns++
					core.Info(cmd, "  ? %-12s %s (%s)", r.Action, r.Path, r.Message)
				}
			}

			core.Info(cmd, "")
			if dryRun {
				core.Success(cmd, "Dry run completed: %d files planned.", dryRuns)
				return nil
			}

			core.Success(cmd, "Scaffolding finished: %d created, %d overwritten, %d skipped.", created, overwritten, skipped)

			core.Info(cmd, "\nNext steps:")
			registerFn := "RegisterRoutes"
			if opts.IsAdmin {
				registerFn = "RegisterAdminRoutes"
			}
			core.Info(cmd, "  1. Register routes in internal/bootstrap/engine.go:")
			data := core.BuildTemplateData(opts)
			core.Info(cmd, "     %s.%s(v1, %sHandler, mw)", data.PackageName, registerFn, data.CamelName)
			core.Info(cmd, "  2. Verify and format code:")
			core.Info(cmd, "     rtk just check")
			core.Info(cmd, "  3. Regenerate Swagger/OpenAPI documentation:")
			core.Info(cmd, "     rtk just docs")

			return nil
		},
	}

	cmd.Flags().BoolVar(&client, "client", false, "Generate in client namespace instead of admin")
	cmd.Flags().BoolVarP(&force, core.FlagForce, "f", false, "Force overwrite existing files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Simulate generation without writing files")
	cmd.Flags().StringVar(&tag, "tag", "", "Custom Swagger tag name")
	cmd.Flags().StringVar(&chinese, "chinese", "", "Custom Chinese business description")

	return cmd
}

// newMakeDestroyCmd 构建 CRUD 模块回退与清理子命令 (bgin make destroy <name>)。
func newMakeDestroyCmd() *cobra.Command {
	var (
		client bool
		force  bool
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:     "destroy [name]",
		Aliases: []string{"d", "rm"},
		Short:   "Destroy and clean up a generated CRUD feature slice",
		Long: `Revert and delete all files generated for a CRUD feature slice.
Removes:
  - internal/features/[admin|client]/<name>/handler.go
  - internal/features/[admin|client]/<name>/repository.go
  - internal/features/[admin|client]/<name>/routes.go
  - internal/features/[admin|client]/<name>/service.go
  - internal/features/[admin|client]/<name>/request.go
  - internal/features/[admin|client]/<name>/response.go
  - internal/models/<name>.go
  - internal/features/[admin|client]/<name>/ (if directory is empty)

Examples:
  bgin make destroy article
  bgin make destroy post --client
  bgin make destroy product --force
  bgin make destroy order --dry-run`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && !core.IsInteractive() {
				return errors.New("accepts 1 arg(s), received 0. Usage: bgin make destroy <name>")
			}
			if len(args) > 1 {
				return fmt.Errorf("accepts at most 1 arg(s), received %d", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				name string
				err  error
			)

			if len(args) > 0 {
				name = strings.TrimSpace(args[0])
			} else {
				name, err = promptMakeDestroy(&client)
				if err != nil {
					return err
				}
			}

			if !force && !dryRun {
				confirmed, err := core.Confirm(fmt.Sprintf("Are you sure you want to destroy CRUD module [%s]?", name),
					"All scaffolded files and the feature directory will be permanently deleted.",
					"Yes, destroy")
				if err != nil {
					return err
				}
				if !confirmed {
					core.Warn(cmd, "Destroy operation cancelled.")
					return core.ErrCanceled
				}
			}

			opts := core.GeneratorOptions{
				Name:    name,
				IsAdmin: !client,
				Force:   force,
				DryRun:  dryRun,
			}

			core.Title(cmd, fmt.Sprintf("Destroying CRUD module: %s (Admin=%v, DryRun=%v)", name, opts.IsAdmin, opts.DryRun))

			results, err := core.DestroyCRUD(opts)
			if err != nil {
				return err
			}

			var deleted, skipped, dryRuns int
			for _, r := range results {
				switch r.Action {
				case core.ActionDeleted:
					deleted++
					core.Warn(cmd, "  - %-12s %s", r.Action, r.Path)
				case core.ActionSkipped:
					skipped++
					core.Info(cmd, "  ~ %-12s %s (%s)", r.Action, r.Path, r.Message)
				case core.ActionDryRun:
					dryRuns++
					core.Info(cmd, "  ? %-12s %s (%s)", r.Action, r.Path, r.Message)
				}
			}

			core.Info(cmd, "")
			if dryRun {
				core.Success(cmd, "Dry run completed: %d files would be deleted.", dryRuns)
				return nil
			}

			core.Success(cmd, "Scaffolding cleanup finished: %d deleted, %d skipped.", deleted, skipped)
			core.Warn(cmd, "\nReminder: If routes were registered in internal/bootstrap/engine.go, please remove them manually.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&client, "client", false, "Destroy from client namespace instead of admin")
	cmd.Flags().BoolVarP(&force, core.FlagForce, "f", false, "Force delete without interactive confirmation")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Simulate deletion without removing files")

	return cmd
}

// newMakeCRUDAliasCmd 提供 Artisan 风格的 make:crud 别名。
func newMakeCRUDAliasCmd() *cobra.Command {
	cmd := newMakeCRUDCmd()
	cmd.Use = "make:crud [name]"
	cmd.Hidden = true
	return cmd
}

// newMakeDestroyAliasCmd 提供 Artisan 风格的 make:destroy 别名。
func newMakeDestroyAliasCmd() *cobra.Command {
	cmd := newMakeDestroyCmd()
	cmd.Use = "make:destroy [name]"
	cmd.Hidden = true
	return cmd
}

// promptMakeCRUD 在交互式终端下引导用户补全 CRUD 模块生成所需参数。
func promptMakeCRUD(client *bool, tag, chinese *string) (string, error) {
	var name string
	scope := scopeAdmin
	if *client {
		scope = scopeClient
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Module Name").
				Description("Singular or plural identifier (e.g. article, order)").
				Value(&name).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("module name cannot be empty")
					}
					return nil
				}),
			huh.NewSelect[string]().
				Title("Module Scope").
				Description("Target domain partition").
				Options(
					huh.NewOption("Admin (Management API)", scopeAdmin),
					huh.NewOption("Client (User API)", scopeClient),
				).
				Value(&scope),
			huh.NewInput().
				Title("Chinese Name (Optional)").
				Description("Business entity name (e.g. 文章, 订单)").
				Value(chinese),
			huh.NewInput().
				Title("OpenAPI Tag Name (Optional)").
				Description("Documentation tag (e.g. 文章管理, 订单中心)").
				Value(tag),
		),
	)

	if err := form.Run(); err != nil {
		return "", err
	}

	if scope == scopeClient {
		*client = true
	}
	return name, nil
}

// promptMakeDestroy 在交互式终端下引导用户指定需要回退删除的 CRUD 模块。
func promptMakeDestroy(client *bool) (string, error) {
	var name string
	scope := scopeAdmin
	if *client {
		scope = scopeClient
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Module Name to Destroy").
				Description("Singular or plural identifier to delete (e.g. article, order)").
				Value(&name).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("module name cannot be empty")
					}
					return nil
				}),
			huh.NewSelect[string]().
				Title("Module Scope").
				Description("Target domain partition").
				Options(
					huh.NewOption("Admin (Management API)", scopeAdmin),
					huh.NewOption("Client (User API)", scopeClient),
				).
				Value(&scope),
		),
	)

	if err := form.Run(); err != nil {
		return "", err
	}

	if scope == scopeClient {
		*client = true
	}
	return name, nil
}
