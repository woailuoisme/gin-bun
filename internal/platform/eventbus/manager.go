// Package eventbus 提供基于 Watermill CQRS 的领域事件总线、路由器与中间件生命周期管理。
package eventbus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/components/cqrs"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"

	"bgin/internal/config"
	"bgin/internal/platform/txhook"
)

// Driver 代表支持的消息传输后端驱动类型。
const (
	DriverGoChannel = "go_channel"
)

var (
	_ Bus        = (*Manager)(nil)
	_ Publisher  = (*Manager)(nil)
	_ Subscriber = (*Manager)(nil)
	_ Runner     = (*Manager)(nil)
)

// Option 为 Manager 构造提供函数式配置选项。
type Option func(*Manager)

// WithAfterCommitHandler 允许自定义外部事务提交后的钩子派发器。
func WithAfterCommitHandler(h func(ctx context.Context, fn func())) Option {
	return func(m *Manager) {
		if h != nil {
			m.afterCommit = h
		}
	}
}

// Manager 封装基于 Watermill CQRS 的事件总线、路由器与事件处理器生命周期。
type Manager struct {
	cfg            config.EventBusConfig
	logger         watermill.LoggerAdapter
	pubSub         *gochannel.GoChannel
	router         *message.Router
	eventBus       *cqrs.EventBus
	eventProcessor *cqrs.EventProcessor
	afterCommit    func(ctx context.Context, fn func())

	mu      sync.RWMutex
	running bool
	closed  bool
}

// NewManager 初始化并构建基于 Watermill CQRS 的事件总线管理器。
func NewManager(cfg config.EventBusConfig, zlog zerolog.Logger, opts ...Option) (*Manager, error) {
	// 事件总线子系统支持独立日志级别，默认降级为 warn 以减少启动期内部流水输出。
	if cfg.LogLevel != "" {
		lvl, err := zerolog.ParseLevel(cfg.LogLevel)
		if err != nil {
			lvl = zerolog.WarnLevel
		}
		zlog = zlog.Level(lvl)
	}

	logger := NewZerologLoggerAdapter(zlog)

	pubSub, err := createPubSub(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create pubsub: %w", err)
	}

	router, err := message.NewRouter(message.RouterConfig{}, logger)
	if err != nil {
		_ = pubSub.Close()
		return nil, fmt.Errorf("failed to create router: %w", err)
	}

	// 挂载中间件管道：异常恢复、关联标识、重试机制与超时控制。
	retryMiddleware := middleware.Retry{
		MaxRetries:          3,
		InitialInterval:     100 * time.Millisecond,
		MaxInterval:         1 * time.Second,
		Multiplier:          2.0,
		Logger:              logger,
		ResetContextOnRetry: true,
	}

	router.AddMiddleware(
		middleware.Recoverer,
		middleware.CorrelationID,
		retryMiddleware.Middleware,
		middleware.Timeout(15*time.Second),
	)

	marshaler := defaultJSONMarshaler()

	eventBus, err := cqrs.NewEventBusWithConfig(pubSub, cqrs.EventBusConfig{
		GeneratePublishTopic: func(params cqrs.GenerateEventPublishTopicParams) (string, error) {
			return qualifyTopic(cfg.TopicPrefix, params.EventName), nil
		},
		OnPublish: injectEventMetadata,
		Marshaler: marshaler,
		Logger:    logger,
	})
	if err != nil {
		_ = router.Close()
		_ = pubSub.Close()
		return nil, fmt.Errorf("failed to create event bus: %w", err)
	}

	eventProcessor, err := cqrs.NewEventProcessorWithConfig(router, cqrs.EventProcessorConfig{
		GenerateSubscribeTopic: func(params cqrs.EventProcessorGenerateSubscribeTopicParams) (string, error) {
			return qualifyTopic(cfg.TopicPrefix, params.EventName), nil
		},
		SubscriberConstructor: func(params cqrs.EventProcessorSubscriberConstructorParams) (message.Subscriber, error) {
			return pubSub, nil
		},
		Marshaler: marshaler,
		Logger:    logger,
	})
	if err != nil {
		_ = router.Close()
		_ = pubSub.Close()
		return nil, fmt.Errorf("failed to create event processor: %w", err)
	}

	m := &Manager{
		cfg:            cfg,
		logger:         logger,
		pubSub:         pubSub,
		router:         router,
		eventBus:       eventBus,
		eventProcessor: eventProcessor,
		afterCommit:    txhook.AfterCommit,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m, nil
}

// NewTestManager 创建一个基于内存 GoChannel 的轻量测试事件管理器实例。
func NewTestManager(opts ...Option) *Manager {
	mgr, err := NewManager(config.EventBusConfig{
		Driver:      DriverGoChannel,
		TopicPrefix: "test",
		LogLevel:    "error",
	}, zerolog.Nop(), opts...)
	if err != nil {
		panic(fmt.Sprintf("failed to create test event bus manager: %v", err))
	}
	return mgr
}

// EventBus 返回内部的 CQRS EventBus 实例。
func (m *Manager) EventBus() *cqrs.EventBus {
	if m == nil {
		return nil
	}
	return m.eventBus
}

// EventProcessor 返回内部的 CQRS EventProcessor 实例。
func (m *Manager) EventProcessor() *cqrs.EventProcessor {
	if m == nil {
		return nil
	}
	return m.eventProcessor
}

// Router 返回内部维护的 Watermill 消息路由器。
func (m *Manager) Router() *message.Router {
	if m == nil {
		return nil
	}
	return m.router
}

// Publisher 返回底层消息发布者。
func (m *Manager) Publisher() message.Publisher {
	if m == nil {
		return nil
	}
	return m.pubSub
}

// Subscriber 返回底层消息订阅者。
func (m *Manager) Subscriber() message.Subscriber {
	if m == nil {
		return nil
	}
	return m.pubSub
}

// RegisterHandlers 向 EventProcessor 注册一个或多个领域事件处理器。
func (m *Manager) RegisterHandlers(handlers ...cqrs.EventHandler) error {
	if m == nil || m.eventProcessor == nil {
		return errors.New("event processor is not initialized")
	}
	return m.eventProcessor.AddHandlers(handlers...)
}

// RegisterHandler 向 EventProcessor 注册单个或批量事件处理器，满足 Subscriber 接口契约。
// 参数可为 cqrs.EventHandler 或 []cqrs.EventHandler。
func (m *Manager) RegisterHandler(handler any) error {
	if m == nil || m.eventProcessor == nil {
		return errors.New("event processor is not initialized")
	}
	switch h := handler.(type) {
	case cqrs.EventHandler:
		return m.eventProcessor.AddHandlers(h)
	case []cqrs.EventHandler:
		return m.eventProcessor.AddHandlers(h...)
	default:
		return fmt.Errorf("unsupported handler type: %T, expected cqrs.EventHandler", handler)
	}
}

// Publish 将领域事件通过 EventBus 发布至对应的 Topic。
func (m *Manager) Publish(ctx context.Context, event any) error {
	if m == nil || m.eventBus == nil {
		return errors.New("event bus is not initialized")
	}
	return m.eventBus.Publish(ctx, event)
}

// PublishAfterCommit 在数据库事务成功提交后发布领域事件。若当前上下文未处于事务中，则立即发布。
// 发布时自动剥离上下文取消信号，避免 HTTP 请求生命周期结束中断异步发布。
func (m *Manager) PublishAfterCommit(ctx context.Context, event any) error {
	if m == nil {
		return errors.New("event bus is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	safeCtx := context.WithoutCancel(ctx)
	afterCommit := m.afterCommit
	if afterCommit == nil {
		afterCommit = txhook.AfterCommit
	}

	afterCommit(ctx, func() {
		if err := m.Publish(safeCtx, event); err != nil {
			m.logger.Error("failed to publish event after commit", err, watermill.LogFields{
				"event": fmt.Sprintf("%T", event),
			})
		}
	})
	return nil
}

// Run 启动消息路由器并开始消费事件。此方法为阻塞调用，直到上下文取消或出现错误。
func (m *Manager) Run(ctx context.Context) error {
	if m == nil || m.router == nil {
		return errors.New("event bus router is not initialized")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("event bus manager is already closed")
	}
	if m.running {
		m.mu.Unlock()
		return errors.New("event bus manager is already running")
	}
	m.running = true
	m.mu.Unlock()

	// 复位运行标记，保证退出后实例可安全重启。
	defer func() {
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
	}()

	return m.router.Run(ctx)
}

// Running 返回用于等待路由器就绪并开始消费的通知通道。
func (m *Manager) Running() <-chan struct{} {
	if m == nil || m.router == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return m.router.Running()
}

// Close 优雅停止消息路由器并释放底层 PubSub 资源。
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()

	var errs []error
	if m.router != nil {
		errs = append(errs, m.router.Close())
	}
	if m.pubSub != nil {
		errs = append(errs, m.pubSub.Close())
	}

	return errors.Join(errs...)
}

// Shutdown 确保应用停止时自动回收总线资源。
func (m *Manager) Shutdown() error {
	return m.Close()
}

func defaultJSONMarshaler() cqrs.JSONMarshaler {
	return cqrs.JSONMarshaler{
		GenerateName: func(v any) string {
			if n, ok := v.(Event); ok {
				return n.EventName()
			}
			if n, ok := v.(interface{ Name() string }); ok {
				return n.Name()
			}
			return cqrs.StructName(v)
		},
	}
}

// injectEventMetadata 自动注入 OpenTelemetry TraceID 与 Watermill CorrelationID。
func injectEventMetadata(params cqrs.OnEventSendParams) error {
	if span := trace.SpanFromContext(params.Message.Context()); span.SpanContext().IsValid() {
		params.Message.Metadata.Set("trace_id", span.SpanContext().TraceID().String())
	}
	middleware.SetCorrelationID(watermill.NewUUID(), params.Message)
	return nil
}

func createPubSub(cfg config.EventBusConfig, logger watermill.LoggerAdapter) (*gochannel.GoChannel, error) {
	switch cfg.Driver {
	case DriverGoChannel, "":
		// 采用非阻塞发布，保证发布端不会因消费端耗时或阻塞而被拖垮。
		return gochannel.NewGoChannel(gochannel.Config{
			BlockPublishUntilSubscriberAck: false,
		}, logger), nil

	default:
		return nil, fmt.Errorf("unsupported event bus driver: %s", cfg.Driver)
	}
}

func qualifyTopic(prefix, eventName string) string {
	if prefix == "" {
		return eventName
	}
	return prefix + "." + eventName
}
