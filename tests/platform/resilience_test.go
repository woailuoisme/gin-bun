package platform_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/platform/resilience"
)

func TestResilience_RetrySuccess(t *testing.T) {
	t.Parallel()

	executor := resilience.NewNetworkExecutor(3, 5*time.Millisecond, 20*time.Millisecond)

	var attempts atomic.Int32
	err := resilience.Run(context.Background(), executor, func() error {
		val := attempts.Add(1)
		if val < 3 {
			return errors.New("temporary network error")
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, int32(3), attempts.Load())
}

func TestResilience_RetryExhausted(t *testing.T) {
	t.Parallel()

	executor := resilience.NewNetworkExecutor(2, 5*time.Millisecond, 10*time.Millisecond)

	var attempts atomic.Int32
	expectedErr := errors.New("permanent failure")
	err := resilience.Run(context.Background(), executor, func() error {
		attempts.Add(1)
		return expectedErr
	})

	require.ErrorIs(t, err, expectedErr)
	assert.Equal(t, int32(2), attempts.Load())
}

func TestResilience_CircuitBreaker(t *testing.T) {
	t.Parallel()

	cb := resilience.NewCircuitBreaker[any](resilience.CircuitBreakerConfig{
		FailureThreshold: 2,
		Delay:            50 * time.Millisecond,
	})

	executor := failsafe.With(cb)

	badErr := errors.New("downstream service down")

	// 第 1 次失败
	_ = resilience.Run(context.Background(), executor, func() error {
		return badErr
	})
	// 第 2 次失败触发熔断
	_ = resilience.Run(context.Background(), executor, func() error {
		return badErr
	})

	assert.True(t, cb.IsOpen(), "断路器在达到连续2次失败后应自动开启")

	// 第 3 次调用应被熔断器直接拦截
	var invoked bool
	err := resilience.Run(context.Background(), executor, func() error {
		invoked = true
		return nil
	})

	assert.False(t, invoked, "熔断器开启后不应再穿透执行业务函数")
	require.ErrorIs(t, err, circuitbreaker.ErrOpen)
}

func TestResilience_GetGeneric(t *testing.T) {
	t.Parallel()

	policy := resilience.NewRetryPolicy[string](resilience.RetryPolicyConfig{
		MaxAttempts: 3,
		Delay:       5 * time.Millisecond,
	})
	executor := failsafe.With(policy)

	var attempts atomic.Int32
	result, err := resilience.Get(context.Background(), executor, func() (string, error) {
		if attempts.Add(1) < 2 {
			return "", errors.New("retryable call")
		}
		return "hello failsafe", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "hello failsafe", result)
	assert.Equal(t, int32(2), attempts.Load())
}

func TestResilience_ContextCancellation(t *testing.T) {
	t.Parallel()

	executor := resilience.NewNetworkExecutor(5, 50*time.Millisecond, 100*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	err := resilience.Run(ctx, executor, func() error {
		return errors.New("always fail")
	})

	require.Error(t, err)
}

func TestResilience_RunWithContext(t *testing.T) {
	t.Parallel()

	t.Run("nil executor falls back to direct invocation", func(t *testing.T) {
		t.Parallel()
		var invoked bool
		err := resilience.RunWithContext(context.Background(), nil, func(ctx context.Context) error {
			assert.NotNil(t, ctx)
			invoked = true
			return nil
		})
		require.NoError(t, err)
		assert.True(t, invoked)
	})

	t.Run("propagates timeout context to inner closure", func(t *testing.T) {
		t.Parallel()
		to := resilience.NewTimeout[any](30 * time.Millisecond)
		executor := failsafe.With(to)

		var innerCanceled bool
		err := resilience.RunWithContext(context.Background(), executor, func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				innerCanceled = true
				return ctx.Err()
			case <-time.After(150 * time.Millisecond):
				return nil
			}
		})

		require.Error(t, err)
		assert.True(t, innerCanceled, "inner context should be canceled when timeout triggers")
	})
}

func TestResilience_GetWithContext(t *testing.T) {
	t.Parallel()

	t.Run("nil executor falls back to direct invocation", func(t *testing.T) {
		t.Parallel()
		res, err := resilience.GetWithContext(context.Background(), nil, func(ctx context.Context) (string, error) {
			assert.NotNil(t, ctx)
			return "fallback", nil
		})
		require.NoError(t, err)
		assert.Equal(t, "fallback", res)
	})

	t.Run("propagates timeout context to inner closure with generic result", func(t *testing.T) {
		t.Parallel()
		to := resilience.NewTimeout[string](30 * time.Millisecond)
		executor := failsafe.With(to)

		var innerCanceled bool
		res, err := resilience.GetWithContext(context.Background(), executor, func(ctx context.Context) (string, error) {
			select {
			case <-ctx.Done():
				innerCanceled = true
				return "", ctx.Err()
			case <-time.After(150 * time.Millisecond):
				return "done", nil
			}
		})

		require.Error(t, err)
		assert.Empty(t, res)
		assert.True(t, innerCanceled, "inner context should receive cancellation signal")
	})
}
