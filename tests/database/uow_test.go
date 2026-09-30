package database_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"bgin/internal/database"
)

// mockTransactor 用于内存单元测试中模拟事务执行器。
type mockTransactor struct {
	runInTxFunc func(ctx context.Context, opts *sql.TxOptions, fn func(ctx context.Context, tx bun.Tx) error) error
}

func (m *mockTransactor) RunInTx(ctx context.Context, opts *sql.TxOptions, fn func(ctx context.Context, tx bun.Tx) error) error {
	if m.runInTxFunc != nil {
		return m.runInTxFunc(ctx, opts, fn)
	}
	return fn(ctx, bun.Tx{})
}

// mockPublisher 用于记录派发的领域事件。
type mockPublisher struct {
	mu     sync.Mutex
	events []any
	err    error
}

func (p *mockPublisher) Publish(_ context.Context, event any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.events = append(p.events, event)
	return nil
}

func (p *mockPublisher) publishedEvents() []any {
	p.mu.Lock()
	defer p.mu.Unlock()
	ret := make([]any, len(p.events))
	copy(ret, p.events)
	return ret
}

// mockOutbox 用于测试发件箱落库。
type mockOutbox struct {
	mu     sync.Mutex
	stored []any
	err    error
}

func (o *mockOutbox) StoreInTx(_ context.Context, _ bun.Tx, events ...any) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil {
		return o.err
	}
	o.stored = append(o.stored, events...)
	return nil
}

type testEvent struct {
	Name string
}

func TestUnitOfWork_CommitAndPublishEvents(t *testing.T) {
	ctx := context.Background()
	transactor := &mockTransactor{}
	pub := &mockPublisher{}

	uow := database.NewUnitOfWork(transactor, database.WithPublisher(pub))

	err := uow.Run(ctx, func(txCtx context.Context, _ bun.Tx) error {
		// 校验上下文内能正确提取 Tx
		tx := database.TxFromContext(txCtx)
		require.NotNil(t, tx)

		// 记录两个领域事件
		ok1 := database.RecordEvent(txCtx, testEvent{Name: "order.created"})
		assert.True(t, ok1)
		ok2 := uow.RecordEvent(txCtx, testEvent{Name: "inventory.reserved"})
		assert.True(t, ok2)

		return nil
	})

	require.NoError(t, err)

	// 断言两个事件在事务提交后被正确派发
	events := pub.publishedEvents()
	require.Len(t, events, 2)
	assert.Equal(t, testEvent{Name: "order.created"}, events[0])
	assert.Equal(t, testEvent{Name: "inventory.reserved"}, events[1])
}

func TestUnitOfWork_RollbackDiscardsEvents(t *testing.T) {
	ctx := context.Background()
	transactor := &mockTransactor{}
	pub := &mockPublisher{}

	uow := database.NewUnitOfWork(transactor, database.WithPublisher(pub))

	expectedErr := errors.New("business validation failed")

	err := uow.Run(ctx, func(txCtx context.Context, _ bun.Tx) error {
		database.RecordEvent(txCtx, testEvent{Name: "phantom.event"})
		return expectedErr
	})

	require.ErrorIs(t, err, expectedErr)

	// 断言事务回滚后，暂存的事件绝不会被派发（杜绝幽灵事件）
	assert.Empty(t, pub.publishedEvents())
}

func TestUnitOfWork_CommitFailureDiscardsEvents(t *testing.T) {
	ctx := context.Background()
	commitErr := errors.New("database connection lost during commit")

	transactor := &mockTransactor{
		runInTxFunc: func(_ context.Context, _ *sql.TxOptions, fn func(ctx context.Context, tx bun.Tx) error) error {
			_ = fn(context.Background(), bun.Tx{})
			return commitErr
		},
	}
	pub := &mockPublisher{}

	uow := database.NewUnitOfWork(transactor, database.WithPublisher(pub))

	err := uow.Run(ctx, func(txCtx context.Context, _ bun.Tx) error {
		database.RecordEvent(txCtx, testEvent{Name: "order.created"})
		return nil
	})

	require.ErrorIs(t, err, commitErr)
	assert.Empty(t, pub.publishedEvents(), "events must not be published if commit fails")
}

func TestUnitOfWork_OutboxStore(t *testing.T) {
	ctx := context.Background()
	transactor := &mockTransactor{}
	pub := &mockPublisher{}
	outbox := &mockOutbox{}

	uow := database.NewUnitOfWork(transactor,
		database.WithPublisher(pub),
		database.WithOutboxStore(outbox),
	)

	err := uow.Run(ctx, func(txCtx context.Context, _ bun.Tx) error {
		database.RecordEvent(txCtx, testEvent{Name: "outbox.event"})
		return nil
	})

	require.NoError(t, err)

	// 验证 Outbox 持久化与 Publisher 均收到事件
	assert.Len(t, outbox.stored, 1)
	assert.Equal(t, testEvent{Name: "outbox.event"}, outbox.stored[0])

	assert.Len(t, pub.publishedEvents(), 1)
}

func TestUnitOfWork_OutboxStoreFailureAbortsTx(t *testing.T) {
	ctx := context.Background()
	transactor := &mockTransactor{}
	pub := &mockPublisher{}
	outbox := &mockOutbox{
		err: errors.New("outbox table locked"),
	}

	uow := database.NewUnitOfWork(transactor,
		database.WithPublisher(pub),
		database.WithOutboxStore(outbox),
	)

	err := uow.Run(ctx, func(txCtx context.Context, _ bun.Tx) error {
		database.RecordEvent(txCtx, testEvent{Name: "outbox.event"})
		return nil
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "outbox store failed")
	assert.Empty(t, pub.publishedEvents())
}

func TestUnitOfWork_NestedPropagation(t *testing.T) {
	ctx := context.Background()
	transactor := &mockTransactor{}
	pub := &mockPublisher{}

	uow := database.NewUnitOfWork(transactor, database.WithPublisher(pub))

	err := uow.Run(ctx, func(txCtx context.Context, _ bun.Tx) error {
		database.RecordEvent(txCtx, testEvent{Name: "outer.event"})

		// 模拟嵌套调用另一个 Service 的 Run (嵌套事务复用)
		innerErr := uow.Run(txCtx, func(innerCtx context.Context, _ bun.Tx) error {
			database.RecordEvent(innerCtx, testEvent{Name: "inner.event"})
			return nil
		})
		require.NoError(t, innerErr)

		return nil
	})

	require.NoError(t, err)

	events := pub.publishedEvents()
	require.Len(t, events, 2)
	assert.Equal(t, testEvent{Name: "outer.event"}, events[0])
	assert.Equal(t, testEvent{Name: "inner.event"}, events[1])
}

func TestUnitOfWork_RecordEventOutsideTx(t *testing.T) {
	ctx := context.Background()
	ok := database.RecordEvent(ctx, testEvent{Name: "unbound.event"})
	assert.False(t, ok, "recording event outside tx must return false")

	tx := database.TxFromContext(ctx)
	assert.Nil(t, tx, "tx outside tx context must be nil")
}

func TestUnitOfWork_NilTransactor(t *testing.T) {
	uow := database.NewUnitOfWork(nil)
	err := uow.Run(context.Background(), func(_ context.Context, _ bun.Tx) error {
		return nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transactor is nil")
}

func TestUnitOfWork_PublishFailureWithoutOutbox_ReturnsError(t *testing.T) {
	ctx := context.Background()
	transactor := &mockTransactor{}
	pubErr := errors.New("network timeout publishing event")
	pub := &mockPublisher{err: pubErr}

	uow := database.NewUnitOfWork(transactor, database.WithPublisher(pub))

	err := uow.Run(ctx, func(txCtx context.Context, _ bun.Tx) error {
		database.RecordEvent(txCtx, testEvent{Name: "important.event"})
		return nil
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain events failed to publish after commit")
	assert.ErrorIs(t, err, pubErr)
}

func TestUnitOfWork_PublishFailureWithOutbox_GracefulDegradation(t *testing.T) {
	ctx := context.Background()
	transactor := &mockTransactor{}
	pubErr := errors.New("network timeout publishing event")
	pub := &mockPublisher{err: pubErr}
	outbox := &mockOutbox{}

	uow := database.NewUnitOfWork(transactor,
		database.WithPublisher(pub),
		database.WithOutboxStore(outbox),
	)

	err := uow.Run(ctx, func(txCtx context.Context, _ bun.Tx) error {
		database.RecordEvent(txCtx, testEvent{Name: "resilient.event"})
		return nil
	})

	// 事务已提交且事件已持久化至 Outbox，即使行内推送失败，也优雅降级返回成功并由 worker 兜底
	require.NoError(t, err)
	assert.Len(t, outbox.stored, 1)
	assert.Equal(t, testEvent{Name: "resilient.event"}, outbox.stored[0])
}
