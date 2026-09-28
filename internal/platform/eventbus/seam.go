package eventbus

import (
	"context"
)

// Event 是领域事件的统一契约接口。
type Event interface {
	EventName() string
}

// Publisher 是业务层发布领域事件的统一契约接口。
type Publisher interface {
	// Publish 立即将领域事件发布至总线。
	Publish(ctx context.Context, event any) error
	// PublishAfterCommit 在当前数据库事务成功提交后发布领域事件；若当前无事务则立即发布。
	PublishAfterCommit(ctx context.Context, event any) error
}

// Subscriber 是事件监听器注册与声明的契约接口。
type Subscriber interface {
	// RegisterHandler 注册一个事件处理器实例。
	RegisterHandler(handler any) error
}

// Runner 是消息路由器与总线生命周期管理契约。
type Runner interface {
	// Run 启动事件路由并开始消费，阻塞直到上下文取消或发生致命错误。
	Run(ctx context.Context) error
	// Running 返回用于等待路由器就绪的通知通道。
	Running() <-chan struct{}
	// Close 优雅停止事件路由器并释放底层 PubSub 资源。
	Close() error
}

// Bus 聚合发布、订阅与生命周期控制，是事件总线基础设施的完整契约。
type Bus interface {
	Publisher
	Subscriber
	Runner
}
