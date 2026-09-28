package console

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"github.com/uptrace/bun"

	"bgin/internal/config"
	"bgin/internal/console/core"
	"bgin/internal/database"
	_ "bgin/internal/indexes"
	"bgin/internal/platform/search"
)

// newScoutCmd 组装搜索引擎管理命令组。
func newScoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scout",
		Short: "Search engine index management",
	}

	cmd.AddCommand(newScoutImportCmd())
	cmd.AddCommand(newScoutFlushCmd())

	return cmd
}

// newScoutImportCmd 将指定模型或全部可搜索模型的数据批量导入搜索引擎。
func newScoutImportCmd() *cobra.Command {
	var (
		all       bool
		chunkSize int
	)

	cmd := &cobra.Command{
		Use:   "import [model]",
		Short: "Import model records into the search index",
		Args: func(cmd *cobra.Command, args []string) error {
			if !all && (len(args) == 0 || strings.TrimSpace(args[0]) == "") {
				return errors.New("please specify a model name or use --all to import all models")
			}
			return nil
		},
		ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return getSearchableModelNames(), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if !cfg.Search.Enabled() {
				return errors.New("search engine is disabled (check SCOUT_DRIVER and MEILISEARCH_HOST)")
			}

			searchClient, err := search.New(cfg.Search)
			if err != nil {
				return fmt.Errorf("failed to initialize search client: %w", err)
			}

			db, err := database.Open(cmd.Context(), cfg.Database, cfg.App.Name)
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			defer func() { _ = db.Close() }()

			if chunkSize <= 0 {
				chunkSize = 100
			}

			var targets []search.ModelDescriptor

			if all {
				targets = search.RegisteredModels()
				if len(targets) == 0 {
					core.Info(cmd, "No searchable models registered.")
					return nil
				}
			} else {
				name := args[0]
				desc, ok := search.GetModel(name)
				if !ok {
					return fmt.Errorf("model [%s] is not registered as searchable", name)
				}
				targets = []search.ModelDescriptor{desc}
			}

			for _, target := range targets {
				if err := importSearchTarget(cmd, db, searchClient, target, chunkSize); err != nil {
					return err
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "a", false, "Import all registered searchable models")
	cmd.Flags().IntVarP(&chunkSize, "chunk", "c", 100, "Number of records to process per batch")

	return cmd
}

// newScoutFlushCmd 清空指定可搜索模型的全文检索索引文档。
func newScoutFlushCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "flush <model>",
		Short: "Flush all of the model's records from the search index",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
				return errors.New("please provide a model name to flush")
			}
			return nil
		},
		ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return getSearchableModelNames(), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if !cfg.Search.Enabled() {
				return errors.New("search engine is disabled (check SCOUT_DRIVER and MEILISEARCH_HOST)")
			}

			name := args[0]
			desc, ok := search.GetModel(name)
			if !ok {
				return fmt.Errorf("model [%s] is not registered as searchable", name)
			}

			if !force {
				confirmed, err := core.Confirm(fmt.Sprintf("Are you sure you want to flush all records from index [%s]?", desc.IndexName),
					"All documents in this search index will be deleted.",
					"Yes, flush")
				if err != nil {
					return err
				}
				if !confirmed {
					core.Warn(cmd, "Scout flush operation cancelled.")
					return core.ErrCanceled
				}
			}

			searchClient, err := search.New(cfg.Search)
			if err != nil {
				return fmt.Errorf("failed to initialize search client: %w", err)
			}

			idx := searchClient.Index(desc.IndexName)
			if err := idx.DeleteAllDocuments(cmd.Context()); err != nil {
				return fmt.Errorf("failed to flush records from index [%s]: %w", desc.IndexName, err)
			}

			core.Success(cmd, "All records for model [%s] flushed from index [%s].", desc.Name, desc.IndexName)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, core.FlagForce, "f", false, core.UsageForceFlag)

	return cmd
}

// 兼容老 Artisan 风格冒号别名命令构造工厂（设为 Hidden: true 保持帮助信息精简）。

func newScoutImportAliasCmd() *cobra.Command {
	c := newScoutImportCmd()
	c.Use = "scout:import"
	c.Hidden = true
	return c
}

func newScoutFlushAliasCmd() *cobra.Command {
	c := newScoutFlushCmd()
	c.Use = "scout:flush"
	c.Hidden = true
	return c
}

// getSearchableModelNames 获取所有已注册的唯一可搜索模型名称列表。
func getSearchableModelNames() []string {
	return lo.Map(search.RegisteredModels(), func(d search.ModelDescriptor, _ int) string {
		return d.Name
	})
}

// importSearchTarget 导入单个可搜索模型的全量数据并驱动进度条展示。
func importSearchTarget(cmd *cobra.Command, db *bun.DB, searchClient search.Client, target search.ModelDescriptor, chunkSize int) error {
	core.Title(cmd, fmt.Sprintf("Importing [%s] records into search index [%s]...", target.Name, target.IndexName))
	idx := searchClient.Index(target.IndexName)

	var bar *core.ProgressBar
	if target.Counter != nil {
		if total, err := target.Counter(cmd.Context(), db); err == nil && total > 0 {
			bar = core.NewProgressBar(cmd, total, target.Name)
		}
	}

	count, err := target.Importer(cmd.Context(), db, chunkSize, func(docs []map[string]any) error {
		if err := idx.AddDocuments(cmd.Context(), docs); err != nil {
			return err
		}
		if bar != nil {
			bar.Incr(int64(len(docs)))
		}
		return nil
	})

	if bar != nil {
		bar.Finish()
	}

	if err != nil {
		return fmt.Errorf("failed to import model [%s]: %w", target.Name, err)
	}

	core.Success(cmd, "Successfully imported [%s] records for model [%s].", humanize.Comma(count), target.Name)
	return nil
}
