// Package realtime 提供基于 Centrifugo 的高可用实时通信基础设施驱动与协议定义。
package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/samber/oops"

	"bgin/internal/platform/identity"
)

// Authenticatable 表示实时频道鉴权可操作的用户实体契约。
type Authenticatable interface {
	GetAuthIdentifier() string
	GetGuardName() string
	GetRole() string
}

// GenericUser 泛用可认证用户结构，方便测试或直接构建。
type GenericUser struct {
	ID        string
	GuardName string
	Role      string
}

// GetAuthIdentifier 返回用户主键标识。
func (u *GenericUser) GetAuthIdentifier() string {
	if u == nil {
		return ""
	}
	return u.ID
}

// GetGuardName 返回认证 Guard 名称。
func (u *GenericUser) GetGuardName() string {
	if u == nil {
		return ""
	}
	return u.GuardName
}

// GetRole 返回用户权限角色。
func (u *GenericUser) GetRole() string {
	if u == nil {
		return ""
	}
	return u.Role
}

// Private 构造标准的私有频道名称。
func Private(format string, args ...any) string {
	name := fmt.Sprintf(format, args...)
	if !strings.HasPrefix(name, "private:") && !strings.HasPrefix(name, "private-") {
		return "private:" + name
	}
	return name
}

// Presence 构造标准的成员状态频道名称。
func Presence(format string, args ...any) string {
	name := fmt.Sprintf(format, args...)
	if !strings.HasPrefix(name, "presence:") && !strings.HasPrefix(name, "presence-") {
		return "presence:" + name
	}
	return name
}

// Public 构造标准的公共广播频道名称。
func Public(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

// Channel 注册基于目标 ID 谓词匹配的声明式频道策略。
func Channel(r *Registry, pattern string, policy func(ctx context.Context, actor Authenticatable, id string) bool) {
	if r == nil || policy == nil {
		return
	}
	r.Register(pattern, func(ctx context.Context, user User, channel string, params map[string]string) error {
		actor := resolveActor(ctx, user)
		targetID := params["id"]
		if !policy(ctx, actor, targetID) {
			return oops.
				Code("FORBIDDEN").
				In("channels."+pattern).
				With("channel", channel).
				Errorf("unauthorized channel subscription")
		}
		return nil
	})
}

// ChannelGuard 注册限定 Guard 名称访问的声明式频道策略。
func ChannelGuard(r *Registry, pattern, allowedGuard string) {
	if r == nil {
		return
	}
	r.Register(pattern, func(ctx context.Context, user User, channel string, _ map[string]string) error {
		guard := resolveGuard(ctx, user)
		if guard == allowedGuard {
			return nil
		}

		return oops.
			Code("FORBIDDEN").
			In("channels."+pattern).
			With("channel", channel).
			Errorf("%s guard required for channel", allowedGuard)
	})
}

// ChannelPolicy 注册通用纯布尔谓词策略。
func ChannelPolicy(r *Registry, pattern string, policy func(ctx context.Context, actor Authenticatable) bool) {
	if r == nil || policy == nil {
		return
	}
	r.Register(pattern, func(ctx context.Context, user User, channel string, _ map[string]string) error {
		actor := resolveActor(ctx, user)
		if !policy(ctx, actor) {
			return oops.
				Code("FORBIDDEN").
				In("channels."+pattern).
				With("channel", channel).
				Errorf("unauthorized channel subscription")
		}
		return nil
	})
}

// ChannelPublish 注册客户端直接发布消息的声明式策略。
func ChannelPublish(r *Registry, pattern string, policy func(ctx context.Context, actor Authenticatable, channel string, data json.RawMessage) bool) {
	if r == nil || policy == nil {
		return
	}
	r.RegisterPublish(pattern, func(ctx context.Context, user User, channel string, data json.RawMessage, _ map[string]string) error {
		actor := resolveActor(ctx, user)
		if !policy(ctx, actor, channel, data) {
			return oops.
				Code("FORBIDDEN").
				In("publish."+pattern).
				With("channel", channel).
				Errorf("unauthorized client publish to channel")
		}
		return nil
	})
}

// RPC 快捷注册远程调用方法处理器。
func RPC(r *Registry, method string, handler RPCHandler) {
	if r == nil || handler == nil {
		return
	}
	r.RegisterRPC(method, handler)
}

func resolveActor(ctx context.Context, user User) Authenticatable {
	if id, ok := identity.FromContext(ctx); ok {
		return &GenericUser{
			ID:        id.UserID,
			Role:      id.Role,
			GuardName: "api",
		}
	}
	if user.ID != "" {
		return &GenericUser{
			ID:        user.ID,
			GuardName: user.GuardName,
		}
	}
	return nil
}

func resolveGuard(ctx context.Context, user User) string {
	if actor := resolveActor(ctx, user); actor != nil {
		return actor.GetGuardName()
	}
	return user.GuardName
}
