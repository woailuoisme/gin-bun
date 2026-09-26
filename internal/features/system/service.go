package system

import (
	"context"
	"errors"

	"github.com/samber/lo"
)

// 组件名既是就绪响应里 checks 的键，也是日志里标注失败来源的标签。
// 它们不属于对外 API 的词汇表，改动只需同步本包与 tests/features/system。
const (
	ComponentDatabase = "database"
	ComponentCache    = "cache"
)

// errProbeNotConfigured 用于"依赖根本没接进来"这种情况：与其让探针 panic，
// 不如让该组件直接报未就绪——装配缺漏应该在就绪状态上暴露出来。
var errProbeNotConfigured = errors.New("probe not configured")

// Pinger 是就绪探测对依赖的最低要求：能报告自己是否可达。
//
// 接口定义在消费方而不是各基础设施包里：bun.DB 与 cache.Client 都不必为本包改动，
// 而本服务在测试里可以用替身替换（适配器见 bootstrap 的装配处）。
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

// Check 按固定顺序探测全部依赖，顺序即响应里 checks 的展示顺序。
//
// 它只如实报告，不做超时控制：时限由调用方通过 ctx 施加（见 handler.go 的 probeTimeout），
// 这样"探针最多等多久"是一处可调的策略，而不是散在探测逻辑里的常量。
func (s *Service) Check(ctx context.Context) []CheckOutcome {
	probes := []struct {
		name   string
		pinger Pinger
	}{
		{ComponentDatabase, s.database},
		{ComponentCache, s.cache},
	}

	return lo.Map(probes, func(probe struct {
		name   string
		pinger Pinger
	}, _ int) CheckOutcome {
		if probe.pinger == nil {
			return CheckOutcome{Name: probe.name, Err: errProbeNotConfigured}
		}
		return CheckOutcome{Name: probe.name, Err: probe.pinger.Ping(ctx)}
	})
}
