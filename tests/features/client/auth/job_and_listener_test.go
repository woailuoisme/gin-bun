package auth_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/config"
	"bgin/internal/features/client/auth"
	"bgin/internal/platform/eventbus"
	"bgin/internal/platform/mailer"
	"bgin/internal/platform/queue"
	"bgin/internal/shared/events"
)

type mockMailer struct {
	mu   sync.Mutex
	sent []mailer.Message
}

func (m *mockMailer) Send(_ context.Context, msg mailer.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mockMailer) SendMailable(_ context.Context, _ string, _ mailer.Mailable) error {
	return nil
}

func (m *mockMailer) Close() error {
	return nil
}

type mockDispatcher struct {
	mu         sync.Mutex
	dispatched []queue.Job
}

func (d *mockDispatcher) Dispatch(_ context.Context, job queue.Job, _ ...asynq.Option) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dispatched = append(d.dispatched, job)
	return nil
}

func TestWelcomeEmailJobExecution(t *testing.T) {
	t.Parallel()

	mailMock := &mockMailer{}
	job := auth.NewWelcomeEmailJob("alex@example.com", "Alex")

	assert.Equal(t, auth.TaskSendWelcomeEmail, job.TaskName())
	assert.Equal(t, queue.DefaultQueueName, job.QueueName())
	assert.Equal(t, 3, job.MaxRetries())

	reg := queue.DefaultRegistry()
	auth.RegisterJobs(reg, mailMock)

	err := job.Handle(t.Context())
	assert.NoError(t, err) // 未注入 mailer 时优雅跳过
}

func TestAuthEventListener(t *testing.T) {
	t.Parallel()

	dispatcher := &mockDispatcher{}

	cfg := config.EventBusConfig{
		Driver:   eventbus.DriverGoChannel,
		LogLevel: "error",
	}
	bus, err := eventbus.NewManager(cfg, zerolog.Nop())
	require.NoError(t, err)
	defer func() { _ = bus.Close() }()

	err = auth.RegisterListeners(bus, dispatcher)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- bus.Run(ctx)
	}()

	// 等待事件路由器准备就绪
	select {
	case <-bus.Running():
	case <-time.After(3 * time.Second):
		t.Fatal("eventbus router did not become ready in time")
	}

	evt := &events.UserRegistered{
		UserID:       "usr_123",
		Email:        "user@example.com",
		Name:         "John Doe",
		RegisteredAt: time.Now(),
	}

	err = bus.Publish(t.Context(), evt)
	require.NoError(t, err)

	// 轮询验证 Dispatcher 收到派发的任务
	require.Eventually(t, func() bool {
		dispatcher.mu.Lock()
		defer dispatcher.mu.Unlock()
		return len(dispatcher.dispatched) > 0
	}, 3*time.Second, 50*time.Millisecond)

	dispatcher.mu.Lock()
	require.Len(t, dispatcher.dispatched, 1)
	dispatchedJob, ok := dispatcher.dispatched[0].(*auth.WelcomeEmailJob)
	dispatcher.mu.Unlock()

	require.True(t, ok)
	assert.Equal(t, "user@example.com", dispatchedJob.Email)
	assert.Equal(t, "John Doe", dispatchedJob.Name)
}
