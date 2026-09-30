// Package database 负责 PostgreSQL 连接池管理、事务工作单元与查询作用域支持。
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/uptrace/bun"
	"go.uber.org/zap"

	"bgin/internal/platform/logger"
)

type uowCtxKey struct{}
type txCtxKey struct{}

// txEventCollector 是与单个事务上下文绑定的领域事件收集器。
type txEventCollector struct {
	mu     sync.Mutex
	events []any
}

func (c *txEventCollector) record(events ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, events...)
}

func (c *txEventCollector) all() []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.events) == 0 {
		return nil
	}
	events := make([]any, len(c.events))
	copy(events, c.events)
	return events
}

// Transactor 抽象支持事务执行的底层数据库契约，*bun.DB 原生满足此接口。
type Transactor interface {
	RunInTx(ctx context.Context, opts *sql.TxOptions, fn func(ctx context.Context, tx bun.Tx) error) error
}

// EventPublisher 定义派发领域事件的接口契约，兼容 platform/eventbus.Publisher。
type EventPublisher interface {
	Publish(ctx context.Context, event any) error
}

// OutboxStore 定义发件箱持久化存储契约，用于在事务内把事件原子落库到发件箱表。
type OutboxStore interface {
	StoreInTx(ctx context.Context, tx bun.Tx, events ...any) error
}

// UnitOfWorkOption 为 UnitOfWork 提供函数式配置。
type UnitOfWorkOption func(*UnitOfWork)

// WithPublisher 为工作单元注入领域事件发布器。
func WithPublisher(pub EventPublisher) UnitOfWorkOption {
	return func(u *UnitOfWork) {
		u.publisher = pub
	}
}

// WithOutboxStore 为工作单元注入发件箱持久化存储。
func WithOutboxStore(store OutboxStore) UnitOfWorkOption {
	return func(u *UnitOfWork) {
		u.outbox = store
	}
}

// WithUowLogger 为工作单元注入兜底日志记录器。
func WithUowLogger(log *zap.Logger) UnitOfWorkOption {
	return func(u *UnitOfWork) {
		u.log = log
	}
}

// UnitOfWork 封装带有事务保证、嵌套事务传播与事件可靠收集的工作单元。
type UnitOfWork struct {
	transactor Transactor
	publisher  EventPublisher
	outbox     OutboxStore
	log        *zap.Logger
}

// NewUnitOfWork 构造事务工作单元实例。
func NewUnitOfWork(transactor Transactor, opts ...UnitOfWorkOption) *UnitOfWork {
	u := &UnitOfWork{
		transactor: transactor,
	}
	for _, opt := range opts {
		opt(u)
	}
	return u
}

// RecordEvent 向当前事务上下文暂存待发布的领域事件。
// 若当前上下文已绑定活跃事务工作单元，则安全追加事件并返回 true；否则返回 false。
func RecordEvent(ctx context.Context, events ...any) bool {
	collector, ok := ctx.Value(uowCtxKey{}).(*txEventCollector)
	if !ok || collector == nil {
		return false
	}
	collector.record(events...)
	return true
}

// TxFromContext 从上下文提取当前活跃的事务实例；若未处于活跃事务中则返回 nil。
func TxFromContext(ctx context.Context) *bun.Tx {
	if tx, ok := ctx.Value(txCtxKey{}).(*bun.Tx); ok {
		return tx
	}
	return nil
}

// RecordEvent 是包级 RecordEvent 的结构体方法，向当前事务上下文追加领域事件。
func (u *UnitOfWork) RecordEvent(ctx context.Context, events ...any) bool {
	return RecordEvent(ctx, events...)
}

// Run 是 RunInTx 的快捷包装（使用默认事务隔离级别）。
func (u *UnitOfWork) Run(ctx context.Context, fn func(ctx context.Context, tx bun.Tx) error) error {
	return u.RunInTx(ctx, nil, fn)
}

// RunInTx 在事务内执行业务逻辑：
// 1. 支持嵌套事务传播（Propagation Required）：若 ctx 已处于活跃工作单元中，直接复用当前事务与事件收集器；
// 2. 事务提交前：若配置了 OutboxStore，将暂存的事件在同一事务内持久化至发件箱；
// 3. 事务成功 Commit 后：收集到的事件立即安全派发至 EventPublisher；
// 4. 若业务闭包返回错误或事务提交失败：事务自动回滚，暂存事件被安全丢弃，绝不派发（杜绝幽灵事件）。
func (u *UnitOfWork) RunInTx(ctx context.Context, opts *sql.TxOptions, fn func(ctx context.Context, tx bun.Tx) error) error {
	if u.transactor == nil {
		return errors.New("unit of work: transactor is nil")
	}

	// 1. 若当前上下文已处于活跃事务中，直接复用既有事务与事件收集器
	if existingTx, ok := ctx.Value(txCtxKey{}).(*bun.Tx); ok && existingTx != nil {
		return fn(ctx, *existingTx)
	}

	collector := &txEventCollector{
		events: make([]any, 0, 4),
	}

	var committedEvents []any

	txErr := u.transactor.RunInTx(ctx, opts, func(txCtx context.Context, tx bun.Tx) error {
		scopedCtx := context.WithValue(txCtx, uowCtxKey{}, collector)
		scopedCtx = context.WithValue(scopedCtx, txCtxKey{}, &tx)

		if err := fn(scopedCtx, tx); err != nil {
			return err
		}

		events := collector.all()

		// 事务提交前：如果有配置 OutboxStore，在事务内部把事件落库
		if u.outbox != nil && len(events) > 0 {
			if err := u.outbox.StoreInTx(scopedCtx, tx, events...); err != nil {
				return fmt.Errorf("unit of work: outbox store failed: %w", err)
			}
		}

		committedEvents = events
		return nil
	})

	if txErr != nil {
		// 事务回滚/失败：暂存事件随 collector 销毁，不会外溢
		return txErr
	}

	// 2. 事务已成功提交：向 EventPublisher 派发事件
	return u.publishCommittedEvents(ctx, committedEvents)
}

// publishCommittedEvents 在事务成功提交后向 EventPublisher 派发事件。
func (u *UnitOfWork) publishCommittedEvents(ctx context.Context, events []any) error {
	if u.publisher == nil || len(events) == 0 {
		return nil
	}

	var publishErrs []error
	for _, evt := range events {
		if err := u.publisher.Publish(ctx, evt); err != nil {
			publishErrs = append(publishErrs, fmt.Errorf("%T: %w", evt, err))
		}
	}

	if len(publishErrs) == 0 {
		return nil
	}

	if u.outbox != nil {
		u.getLogger(ctx).Warn("domain events failed to publish inline after commit, relying on outbox worker",
			zap.Errors("errors", publishErrs))
		return nil
	}

	return fmt.Errorf("unit of work: %d domain events failed to publish after commit: %w",
		len(publishErrs), errors.Join(publishErrs...))
}

func (u *UnitOfWork) getLogger(ctx context.Context) *zap.Logger {
	if log := logger.FromContext(ctx); log != nil {
		return log
	}
	if u.log != nil {
		return u.log
	}
	return zap.NewNop()
}
