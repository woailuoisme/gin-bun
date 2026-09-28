// Package realtime 提供基于 Centrifugo 的高可用实时通信基础设施驱动与统一网关门面。
package realtime

// Gateway 为 Manager 接口的类型别名，用于保持现有依赖注入与调用方的向后兼容。
type Gateway = Manager

// NewGateway 创建统一实时网关实例（保持向后兼容的工厂构造函数）。
func NewGateway(mgr Manager, reg *Registry, secret string) Gateway {
	if mgr == nil {
		var cfg *Config
		if secret != "" {
			cfg = &Config{Secret: secret}
		}
		return NewManager(cfg, WithRegistry(reg))
	}

	if m, ok := mgr.(*ManagerImpl); ok {
		if reg != nil {
			m.registry = reg
		}
		if secret != "" && m.cfg != nil && m.cfg.Secret == "" {
			m.cfg.Secret = secret
		}
	}

	return mgr
}
