// Package realtime 提供基于 Centrifugo 的高可用实时通信基础设施驱动。
package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/centrifugal/gocent/v3"
	"github.com/rs/zerolog"
	"github.com/samber/oops"

	"bgin/internal/config"
	"bgin/internal/platform/logger"
	"bgin/internal/platform/txhook"
)

// Config 为 config.CentrifugoConfig 的类型别名，消除重复定义并直接复用统一配置结构。
type Config = config.CentrifugoConfig

// Manager 定义实时通信完整驱动接口（涵盖发布、广播、连接控制、频道策略及状态查询）。
type Manager interface {
	Dispatch(channel, event string, data any) error
	DispatchContext(ctx context.Context, channel, event string, data any) error
	DispatchAfterCommit(ctx context.Context, channel, event string, data any) error
	Broadcast(channels []string, event string, data any) error
	BroadcastContext(ctx context.Context, channels []string, event string, data any) error
	BroadcastAfterCommit(ctx context.Context, channels []string, event string, data any) error
	Disconnect(ctx context.Context, userID string) error
	Unsubscribe(ctx context.Context, userID, channel string) error
	History(ctx context.Context, channel string, limit int) ([]gocent.Publication, error)
	Presence(ctx context.Context, channel string) (map[string]gocent.ClientInfo, error)
	PresenceStats(ctx context.Context, channel string) (gocent.PresenceStatsResult, error)
	GenerateConnectionToken(userID string, expireAt time.Time, info, meta map[string]any, channels ...string) (string, error)
	Token(userID string, expireAt time.Time, info, meta map[string]any, channels ...string) (string, error)
	Registry() *Registry
	GetNodeID() string
	Client() *gocent.Client
	Config() *Config
}

// AfterCommit 注册在事务提交后执行的钩子；若上下文中未注入事务钩子机制，则立即执行。
func AfterCommit(ctx context.Context, fn func()) {
	txhook.AfterCommit(ctx, fn)
}

// WithAfterCommit 为 context 注入事务提交后回调收集器，并返回触发器函数。
func WithAfterCommit(ctx context.Context) (context.Context, func()) {
	return txhook.WithAfterCommit(ctx)
}

// Option 定义 Manager 配置选项函数。
type Option func(*ManagerImpl)

// WithRegistry 注入频道策略注册表。
func WithRegistry(reg *Registry) Option {
	return func(m *ManagerImpl) {
		if reg != nil {
			m.registry = reg
		}
	}
}

// WithLogger 注入结构化日志器。
func WithLogger(l *zerolog.Logger) Option {
	return func(m *ManagerImpl) {
		if l != nil {
			m.logger = l
		}
	}
}

// WithAfterCommitHandler 注入外部事务提交后钩子派发器。
func WithAfterCommitHandler(h func(ctx context.Context, fn func())) Option {
	return func(m *ManagerImpl) {
		m.afterCommit = h
	}
}

// ManagerImpl 实现 Centrifugo 实时通信 Manager 接口。
type ManagerImpl struct {
	cfg         *Config
	client      *gocent.Client
	registry    *Registry
	logger      *zerolog.Logger
	afterCommit func(ctx context.Context, fn func())
}

// NewManager 创建 Centrifugo 实时通信管理器实例。
func NewManager(cfg *Config, opts ...Option) *ManagerImpl {
	m := &ManagerImpl{
		cfg: cfg,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}

	if m.registry == nil {
		m.registry = NewRegistry()
	}

	if cfg != nil && cfg.Enabled && cfg.URL != "" {
		m.client = gocent.New(gocent.Config{
			Addr: cfg.URL,
			Key:  cfg.APIKey,
		})
	}

	return m
}

func (m *ManagerImpl) log(ctx context.Context) *zerolog.Logger {
	// 必须用 FromContextOK 判断：FromContext 恒返回非 nil（缺省退回包级兜底日志器），
	// 用它做 nil 判断会让这里永远走第一条分支，注入的 m.logger 变成死代码。
	if l, ok := logger.FromContextOK(ctx); ok {
		return l
	}
	if m.logger != nil {
		return m.logger
	}
	return logger.FromContext(ctx)
}

// Config 返回当前实时模块基础设施配置。
func (m *ManagerImpl) Config() *Config {
	return m.cfg
}

// Client 导出底层 gocent 客户端指针。
func (m *ManagerImpl) Client() *gocent.Client {
	return m.client
}

// Registry 返回频道策略注册表。
func (m *ManagerImpl) Registry() *Registry {
	if m.registry == nil {
		m.registry = NewRegistry()
	}
	return m.registry
}

// GetNodeID 返回 Centrifugo 节点标识。
func (m *ManagerImpl) GetNodeID() string {
	return "centrifugo"
}

// disabled 判断实时网关是否不可用（未配置、未启用或客户端未建立）。
func (m *ManagerImpl) disabled() bool {
	return m.cfg == nil || !m.cfg.Enabled || m.client == nil
}

// retryCall 以统一重试策略（3 次、50ms 起指数退避）执行 Centrifugo 出站调用。
func retryCall(ctx context.Context, fn func() error) error {
	return retry.Do(
		fn,
		retry.Context(ctx),
		retry.Attempts(3),
		retry.Delay(50*time.Millisecond),
		retry.DelayType(retry.BackOffDelay),
	)
}

// Dispatch 向单个频道推送事件消息。
//
// 便捷重载：刻意使用后台上下文，使推送与调用方请求的生命周期解耦；
// 需要取消、超时或链路追踪传播时，请改用 DispatchContext。
func (m *ManagerImpl) Dispatch(channelName, event string, data any) error {
	return m.DispatchContext(context.Background(), channelName, event, data)
}

// DispatchContext 向单个频道推送事件消息（携带上下文）。
func (m *ManagerImpl) DispatchContext(ctx context.Context, channelName, event string, data any) error {
	if m.disabled() {
		return nil
	}

	if channelName == "" || event == "" {
		return oops.
			Code("BAD_REQUEST").
			In("ManagerImpl.Dispatch").
			Errorf("missing channel or event")
	}

	payload, err := json.Marshal(map[string]any{
		"event": event,
		"data":  data,
	})
	if err != nil {
		return oops.
			Code("INTERNAL_SERVER_ERROR").
			In("ManagerImpl.Dispatch.Marshal").
			Wrap(err)
	}

	publishErr := retryCall(ctx, func() error {
		_, err := m.client.Publish(ctx, channelName, payload)
		return err
	})
	if publishErr != nil {
		return oops.
			Code("INTERNAL_SERVER_ERROR").
			In("ManagerImpl.Dispatch.Publish").
			With("channel", channelName).
			With("event", event).
			Wrap(publishErr)
	}

	return nil
}

// Broadcast 批量向多个频道推送同一事件消息。
func (m *ManagerImpl) Broadcast(channels []string, event string, data any) error {
	return m.BroadcastContext(context.Background(), channels, event, data)
}

// BroadcastContext 批量向多个频道推送同一事件消息（携带上下文）。
func (m *ManagerImpl) BroadcastContext(ctx context.Context, channels []string, event string, data any) error {
	if m.disabled() || len(channels) == 0 {
		return nil
	}

	if event == "" {
		return oops.
			Code("BAD_REQUEST").
			In("ManagerImpl.Broadcast").
			Errorf("missing event")
	}

	payload, err := json.Marshal(map[string]any{
		"event": event,
		"data":  data,
	})
	if err != nil {
		return oops.
			Code("INTERNAL_SERVER_ERROR").
			In("ManagerImpl.Broadcast.Marshal").
			Wrap(err)
	}

	publishErr := retryCall(ctx, func() error {
		_, err := m.client.Broadcast(ctx, channels, payload)
		return err
	})
	if publishErr != nil {
		return oops.
			Code("INTERNAL_SERVER_ERROR").
			In("ManagerImpl.Broadcast.Execute").
			With("event", event).
			Wrap(publishErr)
	}

	return nil
}

// Disconnect 断开指定用户的 Centrifugo 活跃连接。
func (m *ManagerImpl) Disconnect(ctx context.Context, userID string) error {
	if m.disabled() || userID == "" {
		return nil
	}

	return retryCall(ctx, func() error {
		return m.client.Disconnect(ctx, userID)
	})
}

// Unsubscribe 取消指定用户在特定频道的订阅。
func (m *ManagerImpl) Unsubscribe(ctx context.Context, userID, channel string) error {
	if m.disabled() || userID == "" || channel == "" {
		return nil
	}

	return retryCall(ctx, func() error {
		return m.client.Unsubscribe(ctx, channel, userID)
	})
}

// History 查询指定频道的历史消息记录。
func (m *ManagerImpl) History(ctx context.Context, channel string, limit int) ([]gocent.Publication, error) {
	if m.disabled() || channel == "" {
		return nil, nil
	}

	var opts []gocent.HistoryOption
	if limit > 0 {
		opts = append(opts, gocent.WithLimit(limit))
	}

	var res gocent.HistoryResult
	err := retryCall(ctx, func() error {
		var hErr error
		res, hErr = m.client.History(ctx, channel, opts...)
		return hErr
	})
	if err != nil {
		return nil, oops.
			Code("INTERNAL_SERVER_ERROR").
			In("ManagerImpl.History").
			With("channel", channel).
			Wrap(err)
	}

	return res.Publications, nil
}

// Presence 查询指定频道的在线成员信息列表。
func (m *ManagerImpl) Presence(ctx context.Context, channel string) (map[string]gocent.ClientInfo, error) {
	if m.disabled() || channel == "" {
		return make(map[string]gocent.ClientInfo), nil
	}

	var res gocent.PresenceResult
	err := retryCall(ctx, func() error {
		var pErr error
		res, pErr = m.client.Presence(ctx, channel)
		return pErr
	})
	if err != nil {
		return nil, oops.
			Code("INTERNAL_SERVER_ERROR").
			In("ManagerImpl.Presence").
			With("channel", channel).
			Wrap(err)
	}

	return res.Presence, nil
}

// PresenceStats 查询指定频道的成员在线统计数据（客户端数与独立用户数）。
func (m *ManagerImpl) PresenceStats(ctx context.Context, channel string) (gocent.PresenceStatsResult, error) {
	if m.disabled() || channel == "" {
		return gocent.PresenceStatsResult{}, nil
	}

	var res gocent.PresenceStatsResult
	err := retryCall(ctx, func() error {
		var sErr error
		res, sErr = m.client.PresenceStats(ctx, channel)
		return sErr
	})
	if err != nil {
		return gocent.PresenceStatsResult{}, oops.
			Code("INTERNAL_SERVER_ERROR").
			In("ManagerImpl.PresenceStats").
			With("channel", channel).
			Wrap(err)
	}

	return res, nil
}

// DispatchAfterCommit 在当前数据库事务提交后向单个频道推送事件。
// 若无活动事务，则立即执行。使用 context.WithoutCancel 防止 HTTP 请求取消中断推送。
func (m *ManagerImpl) DispatchAfterCommit(ctx context.Context, channelName, event string, data any) error {
	if m.disabled() {
		return nil
	}
	if channelName == "" || event == "" {
		return oops.
			Code("BAD_REQUEST").
			In("ManagerImpl.DispatchAfterCommit").
			Errorf("missing channel or event")
	}

	bgCtx := context.WithoutCancel(ctx)
	dispatch := func() {
		if err := m.DispatchContext(bgCtx, channelName, event, data); err != nil {
			m.log(bgCtx).Error().
				Err(err).
				Str("channel", channelName).
				Str("event", event).
				Msg("failed to asynchronously dispatch realtime event after commit")
		}
	}

	if m.afterCommit != nil {
		m.afterCommit(ctx, dispatch)
		return nil
	}

	AfterCommit(ctx, dispatch)
	return nil
}

// BroadcastAfterCommit 在当前数据库事务提交后批量向多个频道推送事件。
// 若无活动事务，则立即执行。使用 context.WithoutCancel 防止 HTTP 请求取消中断推送。
func (m *ManagerImpl) BroadcastAfterCommit(ctx context.Context, channels []string, event string, data any) error {
	if m.disabled() || len(channels) == 0 {
		return nil
	}
	if event == "" {
		return oops.
			Code("BAD_REQUEST").
			In("ManagerImpl.BroadcastAfterCommit").
			Errorf("missing event")
	}

	bgCtx := context.WithoutCancel(ctx)
	dispatch := func() {
		if err := m.BroadcastContext(bgCtx, channels, event, data); err != nil {
			m.log(bgCtx).Error().
				Err(err).
				Str("event", event).
				Msg("failed to asynchronously broadcast realtime event after commit")
		}
	}

	if m.afterCommit != nil {
		m.afterCommit(ctx, dispatch)
		return nil
	}

	AfterCommit(ctx, dispatch)
	return nil
}

// GenerateConnectionToken 为指定用户签发 Centrifugo 客户端连接凭证。
func (m *ManagerImpl) GenerateConnectionToken(
	userID string,
	expireAt time.Time,
	info map[string]any,
	meta map[string]any,
	channels ...string,
) (string, error) {
	if m.cfg == nil {
		return "", errors.New("realtime config is nil")
	}
	return GenerateConnectionToken(m.cfg.Secret, userID, expireAt, info, meta, channels...)
}

// Token 快捷签发用于客户端认证的连接凭证（GenerateConnectionToken 的便捷别名）。
func (m *ManagerImpl) Token(
	userID string,
	expireAt time.Time,
	info map[string]any,
	meta map[string]any,
	channels ...string,
) (string, error) {
	return m.GenerateConnectionToken(userID, expireAt, info, meta, channels...)
}
