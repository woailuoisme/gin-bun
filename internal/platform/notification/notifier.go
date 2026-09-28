// Package notification 提供统一、模块化、高可扩展的多渠道通知基础设施。
package notification

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/samber/lo"
	"github.com/samber/oops"
	"golang.org/x/sync/errgroup"

	"bgin/internal/platform/logger"
	"bgin/internal/platform/notification/async"
	"bgin/internal/platform/notification/channel/chat"
	"bgin/internal/platform/notification/channel/database"
	"bgin/internal/platform/notification/channel/mail"
	"bgin/internal/platform/notification/channel/realtime"
	"bgin/internal/platform/notification/channel/sms"
)

// defaultParallelTargets 是并行投递单个通知时的最大并发接收者数。
const defaultParallelTargets = 50

// Notifier 多渠道通知核心引擎。
type Notifier struct {
	mu              sync.RWMutex
	channels        map[string]Channel
	asyncPool       *async.Pool
	parallelTargets int
}

// Manager 为 Notifier 提供语义等同的类型别名以保持向后兼容。
type Manager = Notifier

// NewNotifier 创建统一通知引擎实例。
func NewNotifier(workers, queueSize int) *Notifier {
	return &Notifier{
		channels:        make(map[string]Channel),
		asyncPool:       async.New(workers, queueSize),
		parallelTargets: defaultParallelTargets,
	}
}

// Extend 注册自定义渠道。
func (n *Notifier) Extend(name string, ch Channel) *Notifier {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.channels[name] = ch
	return n
}

// WithParallelTargets 设置批量通知时并行投递的最大并发接收者数，非正值将被忽略。
func (n *Notifier) WithParallelTargets(limit int) *Notifier {
	if limit > 0 {
		n.mu.Lock()
		n.parallelTargets = limit
		n.mu.Unlock()
	}
	return n
}

// parallelLimit 返回当前并行上限，未配置时回退默认值。
func (n *Notifier) parallelLimit() int {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.parallelTargets <= 0 {
		return defaultParallelTargets
	}
	return n.parallelTargets
}

// Channel 按名称查找已注册的渠道。
func (n *Notifier) Channel(name string) Channel {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.channels[name]
}

// Mail 返回邮件渠道专用实例。
func (n *Notifier) Mail() *mail.Channel {
	ch, _ := n.Channel("mail").(*mail.Channel)
	return ch
}

// SMS 返回短信渠道专用实例。
func (n *Notifier) SMS() *sms.Channel {
	ch, _ := n.Channel("sms").(*sms.Channel)
	return ch
}

// Chat 返回即时通讯渠道专用实例。
func (n *Notifier) Chat() *chat.Channel {
	ch, _ := n.Channel("chat").(*chat.Channel)
	return ch
}

// Database 返回数据库通知渠道专用实例。
func (n *Notifier) Database() *database.Channel {
	ch, _ := n.Channel("database").(*database.Channel)
	return ch
}

// Realtime 返回实时广播渠道专用实例。
func (n *Notifier) Realtime() *realtime.Channel {
	ch, _ := n.Channel("realtime").(*realtime.Channel)
	return ch
}

// Close 优雅停止通知引擎及其异步 Worker 池。
func (n *Notifier) Close() error {
	if n.asyncPool != nil {
		n.asyncPool.Close()
	}
	return nil
}

// toSlice 将单个实体或切片归一化为 []any。
func toSlice(target any) []any {
	if target == nil {
		return nil
	}
	if s, ok := target.([]any); ok {
		return s
	}
	val := reflect.ValueOf(target)
	if val.Kind() == reflect.Slice || val.Kind() == reflect.Array {
		result := make([]any, val.Len())
		for i := range val.Len() {
			result[i] = val.Index(i).Interface()
		}
		return result
	}
	return []any{target}
}

// parseSendOptions 解析单次发送参数。
func parseSendOptions(opts []SendOption) SendOptions {
	var so SendOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&so)
		}
	}
	return so
}

// shouldRunAsync 判断当前通知是否应当异步入队。
func shouldRunAsync(notif Notification, so *SendOptions) bool {
	if so.SyncSet {
		return !so.Sync
	}
	if sq, ok := notif.(ShouldQueue); ok {
		return sq.ShouldQueue()
	}
	return false
}

// Send 按顺序向指定接收者列表同步或异步投递通知。
func (n *Notifier) Send(ctx context.Context, notifiables any, notif Notification, opts ...SendOption) error {
	if notif == nil {
		return nil
	}

	targets := toSlice(notifiables)
	if len(targets) == 0 {
		return nil
	}

	so := parseSendOptions(opts)

	if shouldRunAsync(notif, &so) && n.tryDispatchAsync(ctx, notif, func(asyncCtx context.Context) error {
		return n.sendSync(asyncCtx, targets, notif, &so)
	}) {
		return nil
	}

	return n.sendSync(ctx, targets, notif, &so)
}

// tryDispatchAsync 尝试提交给异步协程池处理；若成功提交返回 true，否则返回 false 并降级为同步处理。
func (n *Notifier) tryDispatchAsync(ctx context.Context, notif Notification, task func(asyncCtx context.Context) error) bool {
	if n.asyncPool == nil {
		return false
	}
	submitErr := n.asyncPool.Submit(ctx, func(asyncCtx context.Context) {
		if err := task(asyncCtx); err != nil {
			logger.FromContext(asyncCtx).Error().
				Err(err).
				Str("notification", fmt.Sprintf("%T", notif)).
				Msg("failed to deliver async notification")
		}
	})
	if submitErr == nil {
		return true
	}
	logger.FromContext(ctx).Warn().
		Err(submitErr).
		Bool("queue_full", errors.Is(submitErr, async.ErrQueueFull)).
		Msg("async notification pool unavailable, falling back to synchronous delivery")
	return false
}

// resolveChannels 解析当前接收者需要投递的目标渠道列表。
func resolveChannels(notif Notification, target any, so *SendOptions) []string {
	if len(so.Channels) > 0 {
		return so.Channels
	}
	return notif.Via(target)
}

// dispatchToChannel 针对单一渠道格式化并投递通知载荷。
func (n *Notifier) dispatchToChannel(ctx context.Context, target any, notif Notification, chName string) error {
	ch := n.Channel(chName)
	if ch == nil {
		return oops.Code("notification_channel_not_found").
			With("channel", chName).
			Errorf("unsupported notification channel: %s", chName)
	}

	var message any = notif
	if formatter, ok := ch.(Formatter); ok {
		var err error
		message, err = formatter.Format(notif, target)
		if err != nil {
			return oops.Code("notification_format_failed").
				With("channel", chName).
				Wrap(err)
		}
	}

	if err := ch.Send(ctx, target, message); err != nil {
		return oops.Code("notification_send_failed").
			With("channel", chName).
			Wrap(err)
	}
	return nil
}

// sendSync 内部同步执行投递循环。
func (n *Notifier) sendSync(ctx context.Context, targets []any, notif Notification, so *SendOptions) error {
	for _, target := range targets {
		for _, chName := range resolveChannels(notif, target, so) {
			if err := n.dispatchToChannel(ctx, target, notif, chName); err != nil {
				return err
			}
		}
	}
	return nil
}

// SendParallel 在接收者与渠道维度并发投递通知，并限制最大并发数。
func (n *Notifier) SendParallel(ctx context.Context, notifiables any, notif Notification, opts ...SendOption) error {
	if notif == nil {
		return nil
	}

	targets := toSlice(notifiables)
	if len(targets) == 0 {
		return nil
	}

	so := parseSendOptions(opts)

	if shouldRunAsync(notif, &so) && n.tryDispatchAsync(ctx, notif, func(asyncCtx context.Context) error {
		return n.sendParallelSync(asyncCtx, targets, notif, &so)
	}) {
		return nil
	}

	return n.sendParallelSync(ctx, targets, notif, &so)
}

func (n *Notifier) sendParallelSync(ctx context.Context, targets []any, notif Notification, so *SendOptions) error {
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(n.parallelLimit())

	for _, target := range targets {
		t := target
		for _, chName := range resolveChannels(notif, t, so) {
			name := chName
			g.Go(func() error {
				return n.dispatchToChannel(gCtx, t, notif, name)
			})
		}
	}

	return g.Wait()
}

// SendTyped 强类型接收者顺序投递。
func SendTyped[T any](ctx context.Context, notifiables []T, notif Notification, opts ...SendOption) error {
	anySlice := lo.Map(notifiables, func(item T, _ int) any { return item })
	return Send(ctx, anySlice, notif, opts...)
}

// SendParallelTyped 强类型接收者并发投递。
func SendParallelTyped[T any](ctx context.Context, notifiables []T, notif Notification, opts ...SendOption) error {
	anySlice := lo.Map(notifiables, func(item T, _ int) any { return item })
	return SendParallel(ctx, anySlice, notif, opts...)
}
