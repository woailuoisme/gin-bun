package console

import (
	"errors"
	"fmt"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/golang-module/carbon/v2"
	"github.com/hibiken/asynq"
	"github.com/spf13/cobra"

	"bgin/internal/config"
	"bgin/internal/console/core"
	"bgin/internal/platform/queue"
)

// newQueueWorkCmd 启动 Asynq 统一工作协程与计划任务调度器。
func newQueueWorkCmd() *cobra.Command {
	var (
		queueArg         string
		concurrency      int
		withoutScheduler bool
	)

	cmd := &cobra.Command{
		Use:   "queue:work",
		Short: "Start processing jobs on the queue as a worker daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			client, cleanup, err := queue.NewClient(cfg.Queue)
			if err != nil {
				return fmt.Errorf("failed to initialize queue client: %w", err)
			}
			defer cleanup()

			if concurrency > 0 {
				client.SetConcurrency(concurrency)
			} else {
				concurrency = cfg.Queue.Concurrency
			}

			queues := ParseQueueWeights(queueArg)

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			if !withoutScheduler {
				if err := client.StartScheduler(cfg); err != nil {
					return fmt.Errorf("failed to start schedule runner: %w", err)
				}
			}

			go func() {
				<-ctx.Done()
				_ = client.StopWorker()
				_ = client.StopScheduler()
			}()

			core.Title(cmd, fmt.Sprintf("Queue worker started [queues=%s concurrency=%d]...", queueArg, concurrency))
			return client.RunWorker(queues)
		},
	}

	cmd.Flags().StringVarP(&queueArg, "queue", "q", queue.DefaultQueueName, "Names and weights of queues to listen to (e.g. 'default' or 'high:3,default:1')")
	cmd.Flags().IntVarP(&concurrency, "concurrency", "c", 0, "Maximum number of concurrent worker goroutines (0 to use config)")
	cmd.Flags().BoolVar(&withoutScheduler, "without-scheduler", false, "Disable the built-in recurring schedule runner in this worker")

	return cmd
}

// newQueueFailedCmd 列出全部或指定队列中的失败任务。
func newQueueFailedCmd() *cobra.Command {
	var (
		targetQueue string
		page        int
		pageSize    int
	)

	cmd := &cobra.Command{
		Use:   "queue:failed",
		Short: "List all failed queue jobs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			client, cleanup, err := queue.NewClient(cfg.Queue)
			if err != nil {
				return fmt.Errorf("failed to initialize queue client: %w", err)
			}
			defer cleanup()

			targetQueue = strings.TrimSpace(targetQueue)

			var (
				tasks []*asynq.TaskInfo
			)

			if targetQueue != "" {
				tasks, err = client.ListFailed(targetQueue, page, pageSize)
			} else {
				tasks, err = client.ListFailedAll(page, pageSize)
			}

			if err != nil {
				return fmt.Errorf("failed to query failed jobs: %w", err)
			}

			if len(tasks) == 0 {
				core.Info(cmd, "No failed jobs found.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tQUEUE\tTASK\tERROR\tFAILED AT")
			for _, t := range tasks {
				errMsg := t.LastErr
				if len(errMsg) > 60 {
					errMsg = errMsg[:57] + "..."
				}
				failedAt := carbon.CreateFromStdTime(t.LastFailedAt)
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					t.ID,
					t.Queue,
					t.Type,
					errMsg,
					fmt.Sprintf("%s (%s)", failedAt.ToDateTimeString(), failedAt.DiffForHumans()),
				)
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVarP(&targetQueue, "queue", "q", "", "Filter failed jobs by queue name")
	cmd.Flags().IntVar(&page, "page", 1, "Page number to list")
	cmd.Flags().IntVar(&pageSize, "page-size", 50, "Number of records per page")

	return cmd
}

// newQueueRetryCmd 重新投递指定的失败任务或全部失败任务。
func newQueueRetryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "queue:retry <id|all>",
		Short: "Retry a failed queue job by ID or 'all'",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
				return errors.New("please provide a job ID or 'all'")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			client, cleanup, err := queue.NewClient(cfg.Queue)
			if err != nil {
				return fmt.Errorf("failed to initialize queue client: %w", err)
			}
			defer cleanup()

			target := args[0]
			if strings.EqualFold(target, "all") {
				count, err := client.RetryFailedAll()
				if err != nil {
					return fmt.Errorf("failed to retry failed jobs: %w", err)
				}
				core.Success(cmd, "Retried [%d] failed jobs successfully.", count)
				return nil
			}

			if err := client.RetryFailed("", target); err != nil {
				return fmt.Errorf("failed to retry job [%s]: %w", target, err)
			}

			core.Success(cmd, "Failed job [%s] has been pushed back to the queue.", target)
			return nil
		},
	}
}

// newQueueFlushCmd 清空所有失败任务。
func newQueueFlushCmd() *cobra.Command {
	var (
		targetQueue string
		force       bool
	)

	cmd := &cobra.Command{
		Use:   "queue:flush",
		Short: "Flush all failed queue jobs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			targetQueue = strings.TrimSpace(targetQueue)

			if !force {
				promptText := "Are you sure you want to flush all failed jobs across all queues?"
				if targetQueue != "" {
					promptText = fmt.Sprintf("Are you sure you want to flush all failed jobs in [%s] queue?", targetQueue)
				}
				confirmed, err := core.Confirm(promptText,
					"This operation will permanently purge all archived failed jobs.",
					"Yes, flush")
				if err != nil {
					return err
				}
				if !confirmed {
					core.Warn(cmd, "Queue flush operation cancelled.")
					return core.ErrCanceled
				}
			}

			client, cleanup, err := queue.NewClient(cfg.Queue)
			if err != nil {
				return fmt.Errorf("failed to initialize queue client: %w", err)
			}
			defer cleanup()

			if targetQueue != "" {
				if err := client.PurgeArchived(targetQueue); err != nil {
					return fmt.Errorf("failed to flush jobs for queue [%s]: %w", targetQueue, err)
				}
				core.Success(cmd, "All failed jobs in queue [%s] have been flushed.", targetQueue)
				return nil
			}

			if err := client.PurgeArchivedAll(); err != nil {
				return fmt.Errorf("failed to flush all failed jobs: %w", err)
			}

			core.Success(cmd, "All failed jobs across all queues have been flushed.")
			return nil
		},
	}

	cmd.Flags().StringVarP(&targetQueue, "queue", "q", "", "Filter queue to flush (empty for all queues)")
	cmd.Flags().BoolVarP(&force, core.FlagForce, "f", false, core.UsageForceFlag)

	return cmd
}

// ParseQueueWeights 解析队列权重参数字符串（如 "high:3,default:1" 或 "default"）。
func ParseQueueWeights(raw string) map[string]int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]int{queue.DefaultQueueName: 1}
	}

	result := make(map[string]int)
	for token := range strings.SplitSeq(raw, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		parts := strings.SplitN(token, ":", 2)
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		weight := 1
		if len(parts) == 2 {
			if w, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil && w > 0 {
				weight = w
			}
		}
		result[name] = weight
	}

	if len(result) == 0 {
		return map[string]int{queue.DefaultQueueName: 1}
	}
	return result
}
