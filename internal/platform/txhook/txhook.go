// Package txhook 提供通用的事务提交后生命周期钩子容器，保证事务提交后安全执行副作用。
package txhook

import (
	"context"
	"sync"

	"bgin/internal/platform/logger"
)

type afterCommitKey struct{}

// hookContainer 线程安全的回调函数收集容器。
type hookContainer struct {
	mu    sync.Mutex
	hooks []func()
}

// AfterCommit 注册在事务提交后执行的钩子；若未处于事务上下文则立即执行。
func AfterCommit(ctx context.Context, fn func()) {
	if fn == nil {
		return
	}
	if ctx == nil {
		fn()
		return
	}
	if c, ok := ctx.Value(afterCommitKey{}).(*hookContainer); ok && c != nil {
		c.mu.Lock()
		c.hooks = append(c.hooks, fn)
		c.mu.Unlock()
		return
	}
	fn()
}

// WithAfterCommit 为 context 注入事务提交后回调收集器，并返回触发器函数。
func WithAfterCommit(ctx context.Context) (context.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	c := &hookContainer{}
	txCtx := context.WithValue(ctx, afterCommitKey{}, c)
	commit := func() {
		c.mu.Lock()
		callbacks := c.hooks
		c.hooks = nil
		c.mu.Unlock()
		for _, fn := range callbacks {
			if fn != nil {
				safelyInvoke(ctx, fn)
			}
		}
	}
	return txCtx, commit
}

// safelyInvoke 安全执行单个事务提交后钩子，捕获 Panic 防止级联中断后续副作用。
func safelyInvoke(ctx context.Context, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.FromContext(ctx).Error().
				Interface("panic", r).
				Msg("transaction after-commit hook panicked")
		}
	}()
	fn()
}
