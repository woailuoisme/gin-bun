package eventbus_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/components/cqrs"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"bgin/internal/config"
	"bgin/internal/platform/eventbus"
	"bgin/internal/platform/realtime"
)

type userRegisteredEvent struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
}

func (e userRegisteredEvent) EventName() string {
	return "user.registered"
}

type plainEvent struct {
	Message string `json:"message"`
}

// TestEventBus_NewAndRegister 验证事件总线的构造、配置默认值与非法驱动校验。
func TestEventBus_NewAndRegister(t *testing.T) {
	log := zerolog.Nop()

	// 1. 默认注册
	mgr, err := eventbus.RegisterEventBus(nil, log)
	require.NoError(t, err)
	require.NotNil(t, mgr)
	require.NotNil(t, mgr.EventBus())
	require.NotNil(t, mgr.EventProcessor())
	require.NotNil(t, mgr.Router())
	require.NotNil(t, mgr.Publisher())
	require.NotNil(t, mgr.Subscriber())
	require.NoError(t, mgr.Close())

	// 2. 显式配置注册
	cfg := &config.Config{
		EventBus: config.EventBusConfig{
			Driver:      eventbus.DriverGoChannel,
			TopicPrefix: "app_test",
			LogLevel:    "debug",
		},
	}
	mgr2, err := eventbus.RegisterEventBus(cfg, log)
	require.NoError(t, err)
	require.NotNil(t, mgr2)
	require.NoError(t, mgr2.Close())

	// 3. 非法驱动返回错误
	invalidCfg := config.EventBusConfig{
		Driver: "unsupported_kafka",
	}
	_, err = eventbus.NewManager(invalidCfg, log)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported event bus driver")
}

// TestEventBus_PublishAndListen 验证强类型领域事件的发布、监听、消费与泛型解析链路。
func TestEventBus_PublishAndListen(t *testing.T) {
	mgr := eventbus.NewTestManager()
	defer func() {
		_ = mgr.Close()
	}()

	var receivedUser userRegisteredEvent
	var userMu sync.Mutex
	userDone := make(chan struct{})

	// 1. 通过泛型 Listen 注册监听器
	err := eventbus.Listen(mgr, "handle_user_registered", func(ctx context.Context, ev *userRegisteredEvent) error {
		userMu.Lock()
		defer userMu.Unlock()
		receivedUser = *ev
		close(userDone)
		return nil
	})
	require.NoError(t, err)

	var receivedPlain plainEvent
	var plainMu sync.Mutex
	plainDone := make(chan struct{})

	// 2. 通过通用 Handler 注册没有实现 Event 契约的普通结构体
	plainHandler := cqrs.NewEventHandler("handle_plain_event", func(ctx context.Context, ev *plainEvent) error {
		plainMu.Lock()
		defer plainMu.Unlock()
		receivedPlain = *ev
		close(plainDone)
		return nil
	})
	err = mgr.RegisterHandlers(plainHandler)
	require.NoError(t, err)

	// 3. 异步启动事件路由器
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = mgr.Run(ctx)
	}()

	// 等待路由器运行就绪
	select {
	case <-mgr.Running():
	case <-time.After(3 * time.Second):
		t.Fatal("router failed to start in time")
	}

	// 4. 发布事件
	testEv := userRegisteredEvent{
		UserID: "u-999",
		Email:  "test@example.com",
	}
	err = mgr.Publish(ctx, testEv)
	require.NoError(t, err)

	plainEv := plainEvent{Message: "hello world"}
	err = mgr.Publish(ctx, plainEv)
	require.NoError(t, err)

	// 5. 校验消费成功
	select {
	case <-userDone:
		userMu.Lock()
		assert.Equal(t, "u-999", receivedUser.UserID)
		assert.Equal(t, "test@example.com", receivedUser.Email)
		userMu.Unlock()
	case <-time.After(3 * time.Second):
		t.Fatal("user event was not handled in time")
	}

	select {
	case <-plainDone:
		plainMu.Lock()
		assert.Equal(t, "hello world", receivedPlain.Message)
		plainMu.Unlock()
	case <-time.After(3 * time.Second):
		t.Fatal("plain event was not handled in time")
	}
}

// TestEventBus_PublishAfterCommit 验证事务提交后发布机制与上下文钩子。
func TestEventBus_PublishAfterCommit(t *testing.T) {
	var publishedEvents []string
	var mu sync.Mutex

	mockAfterCommit := func(ctx context.Context, fn func()) {
		// 模拟由事务提交后调度器触发
		fn()
	}

	mgr := eventbus.NewTestManager(eventbus.WithAfterCommitHandler(mockAfterCommit))
	defer func() {
		_ = mgr.Close()
	}()

	err := eventbus.Listen(mgr, "track_after_commit", func(ctx context.Context, ev *plainEvent) error {
		mu.Lock()
		defer mu.Unlock()
		publishedEvents = append(publishedEvents, ev.Message)
		return nil
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = mgr.Run(ctx)
	}()

	<-mgr.Running()

	// 触发 PublishAfterCommit
	err = mgr.PublishAfterCommit(ctx, plainEvent{Message: "committed_data"})
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(publishedEvents) == 1 && publishedEvents[0] == "committed_data"
	}, 2*time.Second, 50*time.Millisecond)
}

// TestEventBus_RealtimeWithAfterCommit 验证与 realtime.WithAfterCommit 真实事务上下文的集成。
func TestEventBus_RealtimeWithAfterCommit(t *testing.T) {
	mgr := eventbus.NewTestManager()
	defer func() {
		_ = mgr.Close()
	}()

	var received bool
	var mu sync.Mutex
	done := make(chan struct{})

	err := eventbus.Listen(mgr, "realtime_tx_test", func(ctx context.Context, ev *plainEvent) error {
		mu.Lock()
		defer mu.Unlock()
		received = true
		close(done)
		return nil
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = mgr.Run(ctx)
	}()

	<-mgr.Running()

	// 使用上下文事务收集器
	txCtx, commit := realtime.WithAfterCommit(ctx)

	err = mgr.PublishAfterCommit(txCtx, plainEvent{Message: "delayed_by_tx"})
	require.NoError(t, err)

	// commit 之前不应收到事件
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	assert.False(t, received)
	mu.Unlock()

	// 执行提交
	commit()

	select {
	case <-done:
		mu.Lock()
		assert.True(t, received)
		mu.Unlock()
	case <-time.After(3 * time.Second):
		t.Fatal("event not received after commit")
	}
}

// TestEventBus_MetadataInjection 验证 OpenTelemetry TraceID 与 Watermill CorrelationID 的注入。
func TestEventBus_MetadataInjection(t *testing.T) {
	mgr := eventbus.NewTestManager()
	defer func() {
		_ = mgr.Close()
	}()

	// 用原始订阅者直接观察消息元数据，而不是监听处理器上下文：
	// 内存 GoChannel 投递前会 Copy 消息，而 Copy 不携带消息上下文，
	// 因此消费端 ctx 天然拿不到发布方 Span；元数据才是本项目承诺交付的载体。
	messages, err := mgr.Subscriber().Subscribe(t.Context(), "test.user.registered")
	require.NoError(t, err)

	// 手工注入有效 SpanContext：本仓库未引入 OTel SDK（无 TracerProvider），
	// otel.Tracer 返回空实现，其 SpanContext 恒为全零且 IsValid() 为 false，
	// 拿它做期望值会得到永真的断言。
	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	require.NoError(t, err)

	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})

	publishCtx := trace.ContextWithSpanContext(t.Context(), spanCtx)
	require.NoError(t, mgr.Publish(publishCtx, userRegisteredEvent{UserID: "u-meta", Email: "meta@example.com"}))

	select {
	case msg := <-messages:
		assert.Equal(t, spanCtx.TraceID().String(), msg.Metadata.Get("trace_id"))
		assert.NotEmpty(t, msg.Metadata, "元数据应同时承载 CorrelationID 等链路信息")
		msg.Ack()
	case <-time.After(3 * time.Second):
		t.Fatal("event was not delivered in time")
	}
}

// TestEventBus_LifecycleAndErrors 验证生命周期边界与错误分支。
func TestEventBus_LifecycleAndErrors(t *testing.T) {
	mgr := eventbus.NewTestManager()

	// 1. 非法处理器注册
	err := mgr.RegisterHandler(12345)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported handler type")

	// 批量处理器注册
	err = mgr.RegisterHandler([]cqrs.EventHandler{})
	require.NoError(t, err)

	// 2. 正常运行与重复运行检测
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = mgr.Run(ctx)
	}()

	<-mgr.Running()

	err = mgr.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// 3. 优雅停止与幂等关闭
	require.NoError(t, mgr.Shutdown())
	require.NoError(t, mgr.Close())

	// 4. 未初始化对象调用安全
	var nilMgr *eventbus.Manager
	require.Error(t, nilMgr.Publish(context.Background(), plainEvent{}))
	require.Error(t, nilMgr.RegisterHandlers())
	require.Error(t, nilMgr.RegisterHandler(nil))
	require.NotNil(t, nilMgr.Running())
	<-nilMgr.Running()
}

// TestZerologLoggerAdapter 验证 Zerolog 适配器的日志记录与字段拷贝能力。
func TestZerologLoggerAdapter(t *testing.T) {
	adapter := eventbus.NewZerologLoggerAdapter(zerolog.Nop())

	adapter.Info("test info", watermill.LogFields{"key": "val"})
	adapter.Debug("test debug", nil)
	adapter.Trace("test trace", nil)
	adapter.Error("test error", errors.New("sample error"), watermill.LogFields{"err_key": 1})

	// With 字段扩展
	withEmpty := adapter.With(nil)
	assert.Equal(t, adapter, withEmpty)

	withFields := adapter.With(watermill.LogFields{"scope": "bus"})
	assert.NotNil(t, withFields)
}
