package queue

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/hibiken/asynq"

	"bgin/internal/config"
)

// RunWorker 启动后台工作协程服务，按队列权重消费任务。
// 队列组成属于部署决策，因此权重由调用方给出；未声明时只监听默认队列。
func (c *Client) RunWorker(queues map[string]int) error {
	if c == nil {
		return errors.New("queue: client is nil")
	}
	if len(queues) == 0 {
		c.log().Warn().Msg("no queue weights specified, worker listening on default queue only")
		queues = map[string]int{DefaultQueueName: 1}
	}

	srv := c.NewServer(queues)
	c.mu.Lock()
	c.server = srv
	c.mu.Unlock()

	return srv.Run(c.BuildServeMux())
}

// StopWorker 优雅停机并释放工作协程服务。
func (c *Client) StopWorker() error {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	srv := c.server
	c.server = nil
	c.mu.Unlock()

	if srv == nil {
		return nil
	}

	srv.Shutdown()

	return nil
}

// BuildServeMux 将注册表中的任务工厂编译挂载到 Asynq 的 ServeMux 多路复用器中。
// 每个任务执行前由工厂创建实例（元数据由构造器给出），再用载荷覆盖业务字段。
func (c *Client) BuildServeMux() *asynq.ServeMux {
	mux := asynq.NewServeMux()
	defaultReg.mu.RLock()
	defer defaultReg.mu.RUnlock()

	for name, factory := range defaultReg.jobs {
		taskName := name
		jobFactory := factory

		mux.HandleFunc(taskName, func(ctx context.Context, t *asynq.Task) (execErr error) {
			job := jobFactory()
			if job == nil {
				c.log().Error().Str("task", taskName).Msg("job factory returned nil instance")
				return errors.New("queue: job factory returned nil instance")
			}

			if err := unmarshalPayload(t.Payload(), job); err != nil {
				c.log().Error().Str("task", taskName).Err(err).Msg("failed to unmarshal task payload")
				return err
			}

			if ttl, ok := defaultReg.LookupLockTTL(taskName, job); ok {
				job = WrapOverlappingJobWithLogger(c.log(), job, ttl, c.locker)
			}

			defer func() {
				if r := recover(); r != nil {
					stack := string(debug.Stack())
					c.log().Error().
						Str("task", taskName).
						Interface("panic", r).
						Str("stack", stack).
						Msg("panic recovered in task execution")
					execErr = fmt.Errorf("queue: job panic: %v", r)
				}
			}()

			execErr = job.Handle(ctx)
			if execErr != nil {
				c.log().Error().Str("task", taskName).Err(execErr).Msg("task execution failed")
				return execErr
			}

			return nil
		})
	}

	return mux
}

func (c *Client) newScheduler(cfg *config.Config) (*asynq.Scheduler, error) {
	if c == nil {
		return nil, errors.New("queue: client is nil")
	}

	loc := time.UTC
	if cfg != nil {
		loc = cfg.App.Location()
	}

	scheduler := asynq.NewScheduler(c.redisOpt, &asynq.SchedulerOpts{
		Location: loc,
	})

	cronEntries := GetCronEntries()
	for _, entry := range cronEntries {
		if entry.Spec == "" || entry.Job == nil {
			continue
		}

		payload, err := json.Marshal(entry.Job)
		if err != nil {
			c.log().Error().Str("task", entry.Job.TaskName()).Err(err).Msg("failed to marshal periodic task payload")
			continue
		}

		queueName := ResolveQueueName(entry.Job)
		task := asynq.NewTask(entry.Job.TaskName(), payload)
		opts := append([]asynq.Option{asynq.Queue(queueName)}, entry.Opts...)

		entryID, err := scheduler.Register(entry.Spec, task, opts...)
		if err != nil {
			c.log().Error().
				Str("task", entry.Job.TaskName()).
				Str("spec", entry.Spec).
				Err(err).
				Msg("failed to register periodic task")
			continue
		}

		c.log().Info().
			Str("task", entry.Job.TaskName()).
			Str("spec", entry.Spec).
			Str("queue", queueName).
			Str("entry_id", entryID).
			Msg("periodic task registered successfully")
	}

	return scheduler, nil
}

// StartScheduler 异步启动周期性定时调度器（用于与 Worker 联合共存运行）。
func (c *Client) StartScheduler(cfg *config.Config) error {
	scheduler, err := c.newScheduler(cfg)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.scheduler = scheduler
	c.mu.Unlock()

	go func() {
		if err := scheduler.Run(); err != nil {
			c.log().Error().Err(err).Msg("periodic task scheduler encountered an error")
		}
	}()

	return nil
}

// RunScheduler 阻塞启动并运行周期性定时调度器，直到上下文被取消。
func (c *Client) RunScheduler(ctx context.Context, cfg *config.Config) error {
	scheduler, err := c.newScheduler(cfg)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.scheduler = scheduler
	c.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		errCh <- scheduler.Run()
	}()

	select {
	case <-ctx.Done():
		_ = c.StopScheduler()
		return nil
	case err := <-errCh:
		c.mu.Lock()
		c.scheduler = nil
		c.mu.Unlock()
		return err
	}
}

// StopScheduler 优雅停止底层 asynq.Scheduler 周期调度器实例。
// 该方法是停机钩子，会在独立的信号 goroutine 中被调用，故与写入侧共用同一把锁。
func (c *Client) StopScheduler() error {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	scheduler := c.scheduler
	c.scheduler = nil
	c.mu.Unlock()

	if scheduler != nil {
		scheduler.Shutdown()
		c.log().Info().Msg("periodic task scheduler stopped")
	}

	return nil
}

// NewServer 创建用于承载任务执行的工作端 Asynq Server。
// 并发数来自 SetConcurrency，队列权重来自调用方；只有停机超时这类框架级策略留在模块内。
func (c *Client) NewServer(queues map[string]int) *asynq.Server {
	return asynq.NewServer(
		c.redisOpt,
		asynq.Config{
			Concurrency:     cmp.Or(c.Concurrency(), 10),
			Queues:          queues,
			ShutdownTimeout: c.ShutdownTimeout(),
			ErrorHandler:    asynq.ErrorHandlerFunc(c.handleTaskError),
		},
	)
}

// handleTaskError 统一拦截任务执行失败或耗尽重试事件。
func (c *Client) handleTaskError(ctx context.Context, task *asynq.Task, err error) {
	retried, hasRetried := asynq.GetRetryCount(ctx)
	maxRetry, hasMax := asynq.GetMaxRetry(ctx)
	taskID, _ := asynq.GetTaskID(ctx)

	queueName, _ := asynq.GetQueueName(ctx)

	event := c.log().Error().
		Str("task_id", taskID).
		Str("task", task.Type()).
		Str("queue", queueName).
		Err(err)

	if hasRetried && hasMax {
		event.Int("retried", retried).Int("max_retry", maxRetry)
		if retried >= maxRetry {
			event.Msg("task retries exhausted, moved to dead letter archive")
			return
		}
	}

	event.Msg("task execution failed, retry scheduled")
}

// unmarshalPayload 将原始载荷反序列化到任务实例中。
func unmarshalPayload(raw []byte, job Job) error {
	if len(raw) == 0 {
		return nil
	}

	return json.Unmarshal(raw, job)
}
