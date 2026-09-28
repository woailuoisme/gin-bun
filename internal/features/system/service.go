package system

import (
	"context"
	"errors"

	"github.com/samber/lo"
)

// 组件名定义，对应就绪响应 checks 键名与日志组件标签。
const (
	ComponentDatabase = "database"
	ComponentCache    = "cache"
)

// errProbeNotConfigured 表示组件探测实例未注入或未初始化。
var errProbeNotConfigured = errors.New("probe not configured")

// Pinger 定义组件可达性探测接口。
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps 是探针所需的依赖。
type Deps struct {
	Database Pinger
	Cache    Pinger
}

// Service 逐个探测依赖并汇总实例能否接流量。
type Service struct {
	database Pinger
	cache    Pinger
}

// NewService 构造探针服务。
func NewService(deps Deps) *Service {
	return &Service{database: deps.Database, cache: deps.Cache}
}

// CheckOutcome 是单个组件的探测结果，Err 为 nil 表示可达。
type CheckOutcome struct {
	Name string
	Err  error
}

// Check 按固定顺序探测全部依赖的可达性状态。
func (s *Service) Check(ctx context.Context) []CheckOutcome {
	type probe struct {
		name   string
		pinger Pinger
	}
	probes := []probe{
		{ComponentDatabase, s.database},
		{ComponentCache, s.cache},
	}

	return lo.Map(probes, func(p probe, _ int) CheckOutcome {
		if p.pinger == nil {
			return CheckOutcome{Name: p.name, Err: errProbeNotConfigured}
		}
		return CheckOutcome{Name: p.name, Err: p.pinger.Ping(ctx)}
	})
}
