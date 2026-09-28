package eventbus

import (
	"context"

	"github.com/ThreeDotsLabs/watermill/components/cqrs"
)

// Listen 使用 Go 泛型向事件总线声明式注册强类型领域事件监听器。
// 底层自动封装适配 Watermill CQRS EventHandler。
func Listen[T any](bus Subscriber, name string, handler func(ctx context.Context, event *T) error) error {
	h := cqrs.NewEventHandler(name, handler)
	return bus.RegisterHandler(h)
}
