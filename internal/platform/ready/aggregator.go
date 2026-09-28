package ready

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// DefaultCheckTimeout 是单个组件探针的默认超时上限。
const DefaultCheckTimeout = 1 * time.Second

// Aggregator 负责并发调度已注册的就绪探针并聚合整体评估结果。
type Aggregator struct {
	mu             sync.RWMutex
	checks         []Check
	defaultTimeout time.Duration
}

// New 创建就绪探针聚合器实例。
func New(checks ...Check) *Aggregator {
	return &Aggregator{
		checks:         slices.Clone(checks),
		defaultTimeout: DefaultCheckTimeout,
	}
}

// SetDefaultTimeout 配置单项检查默认超时上限。
func (a *Aggregator) SetDefaultTimeout(d time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if d > 0 {
		a.defaultTimeout = d
	}
}

// Register 显式注册一个或多个待测组件规范。
func (a *Aggregator) Register(checks ...Check) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.checks = append(a.checks, checks...)
}

// Check 并发探测所有已注册组件，返回聚合评估结果及失败错误字典（供日志记录）。
func (a *Aggregator) Check(ctx context.Context) (Result, map[string]error) {
	if ctx == nil {
		ctx = context.Background()
	}

	a.mu.RLock()
	checks := slices.Clone(a.checks)
	defTimeout := a.defaultTimeout
	a.mu.RUnlock()

	reducer := newResultReducer()
	var (
		errMu sync.Mutex
		errs  = make(map[string]error)
	)

	var g errgroup.Group
	for _, c := range checks {
		check := c
		if !check.IsEnabled() {
			reducer.addDisabled(check)
			continue
		}
		g.Go(func() error {
			latency, err := runProbe(ctx, check, defTimeout)
			reducer.addChecked(check, err, latency)
			if err != nil {
				errMu.Lock()
				errs[check.Name] = err
				errMu.Unlock()
			}
			return nil
		})
	}

	_ = g.Wait()
	return reducer.snapshot(), errs
}

// runProbe 执行单个探针并带有独立的超时与 Panic 恢复保护。
func runProbe(parentCtx context.Context, check Check, defaultTimeout time.Duration) (time.Duration, error) {
	if check.Ping == nil {
		return 0, ErrProbeNotConfigured
	}

	timeout := check.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	start := time.Now()
	done := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("probe panic: %v", r)
			}
		}()
		done <- check.Ping(ctx)
	}()

	select {
	case err := <-done:
		return time.Since(start), err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return time.Since(start), fmt.Errorf("probe timed out after %s", timeout)
		}
		return time.Since(start), ctx.Err()
	}
}
