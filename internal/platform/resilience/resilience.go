// Package resilience 基于 failsafe-go 封装通用的弹性容错机制（重试、熔断断路、隔离舱与执行超时）。
// 为全系统出站调用（三方支付、短信、邮件、对象存储与实时推送）提供统一的弹性防护底座。
package resilience

import (
	"context"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/bulkhead"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/retrypolicy"
	"github.com/failsafe-go/failsafe-go/timeout"
)

// RetryPolicyConfig 重试策略配置项。
type RetryPolicyConfig struct {
	MaxAttempts int
	Delay       time.Duration
	MaxDelay    time.Duration
	Jitter      time.Duration
}

// CircuitBreakerConfig 熔断断路器配置项。
type CircuitBreakerConfig struct {
	FailureThreshold uint
	SuccessThreshold uint
	Delay            time.Duration
	OnStateChanged   func(circuitbreaker.StateChangedEvent)
}

// BulkheadConfig 并发隔离舱配置项。
type BulkheadConfig struct {
	MaxConcurrency uint
	MaxWaitTime    time.Duration
}

// NewRetryPolicy 构建带指数退避与重试上限的重试策略。
func NewRetryPolicy[R any](cfg RetryPolicyConfig) retrypolicy.RetryPolicy[R] {
	builder := retrypolicy.NewBuilder[R]()

	if cfg.MaxAttempts > 0 {
		builder = builder.WithMaxAttempts(cfg.MaxAttempts)
	}

	if cfg.Delay > 0 {
		if cfg.MaxDelay > cfg.Delay {
			builder = builder.WithBackoff(cfg.Delay, cfg.MaxDelay)
		} else {
			builder = builder.WithDelay(cfg.Delay)
		}
	}

	if cfg.Jitter > 0 {
		builder = builder.WithJitter(cfg.Jitter)
	}

	return builder.Build()
}

// NewCircuitBreaker 构建熔断断路器，用于探测下游故障并主动切断请求防止雪崩。
func NewCircuitBreaker[R any](cfg CircuitBreakerConfig) circuitbreaker.CircuitBreaker[R] {
	builder := circuitbreaker.NewBuilder[R]()

	if cfg.FailureThreshold > 0 {
		builder = builder.WithFailureThreshold(cfg.FailureThreshold)
	}

	if cfg.SuccessThreshold > 0 {
		builder = builder.WithSuccessThreshold(cfg.SuccessThreshold)
	}

	if cfg.Delay > 0 {
		builder = builder.WithDelay(cfg.Delay)
	}

	if cfg.OnStateChanged != nil {
		builder = builder.OnStateChanged(cfg.OnStateChanged)
	}

	return builder.Build()
}

// NewBulkhead 构建并发隔离舱，限制对下游的最大并发请求数并提供排队等待缓冲。
func NewBulkhead[R any](cfg BulkheadConfig) bulkhead.Bulkhead[R] {
	builder := bulkhead.NewBuilder[R](cfg.MaxConcurrency)
	if cfg.MaxWaitTime > 0 {
		builder = builder.WithMaxWaitTime(cfg.MaxWaitTime)
	}
	return builder.Build()
}

// NewTimeout 构建操作级超时控制策略。
func NewTimeout[R any](dur time.Duration) timeout.Timeout[R] {
	return timeout.New[R](dur)
}

// NewExecutor 根据给定的弹性策略列表构建执行器。
func NewExecutor[R any](policies ...failsafe.Policy[R]) failsafe.Executor[R] {
	return failsafe.With(policies...)
}

// NewNetworkExecutor 构建一套面向标准出站网络调用的预设执行器（包含重试策略）。
func NewNetworkExecutor(maxAttempts int, initialDelay, maxDelay time.Duration) failsafe.Executor[any] {
	retry := NewRetryPolicy[any](RetryPolicyConfig{
		MaxAttempts: maxAttempts,
		Delay:       initialDelay,
		MaxDelay:    maxDelay,
	})
	return failsafe.With(retry)
}

// Run 遵循上下文并在执行器保护下运行无返回值的任务。
func Run(ctx context.Context, executor failsafe.Executor[any], fn func() error) error {
	if executor == nil {
		return fn()
	}
	return executor.WithContext(ctx).Run(fn)
}

// RunWithContext 遵循上下文并在执行器保护下运行接收 context 的任务。
// 闭包所接收的 Context 自动集成执行器层级的超时或对冲派生控制。
func RunWithContext(ctx context.Context, executor failsafe.Executor[any], fn func(context.Context) error) error {
	if executor == nil {
		return fn(ctx)
	}
	return executor.WithContext(ctx).RunWithExecution(func(exec failsafe.Execution[any]) error {
		return fn(exec.Context())
	})
}

// Get 遵循上下文并在执行器保护下运行有返回值的任务。
func Get[T any](ctx context.Context, executor failsafe.Executor[T], fn func() (T, error)) (T, error) {
	if executor == nil {
		return fn()
	}
	return executor.WithContext(ctx).Get(fn)
}

// GetWithContext 遵循上下文并在执行器保护下运行接收 context 的任务并返回值。
// 闭包所接收的 Context 自动集成执行器层级的超时或对冲派生控制。
func GetWithContext[T any](ctx context.Context, executor failsafe.Executor[T], fn func(context.Context) (T, error)) (T, error) {
	if executor == nil {
		return fn(ctx)
	}
	return executor.WithContext(ctx).GetWithExecution(func(exec failsafe.Execution[T]) (T, error) {
		return fn(exec.Context())
	})
}
