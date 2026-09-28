package system

import (
	"context"

	"bgin/internal/platform/ready"
)

// 组件名定义，对齐 ready 标准组件常量，对应就绪响应 checks 键名与链路日志。
const (
	ComponentDatabase = ready.ComponentDatabase
	ComponentCache    = ready.ComponentCache
	ComponentStorage  = ready.ComponentStorage
	ComponentSearch   = ready.ComponentSearch
	ComponentRealtime = ready.ComponentRealtime
	ComponentImgproxy = ready.ComponentImgproxy
	ComponentMailer   = ready.ComponentMailer
)

// Pinger 定义组件可达性探测接口。
type Pinger = ready.Pinger

// Deps 是探针所需的依赖。
type Deps struct {
	Database Pinger
	Cache    Pinger
	Storage  Pinger
	Search   Pinger
	Realtime Pinger
	Imgproxy Pinger
	Mailer   Pinger
}

// Service 协调调度底层 ready.Aggregator 评估实例能否接流量。
type Service struct {
	aggregator *ready.Aggregator
}

// buildCheck 统一为基础组件装配具备 Driver 与 Enabled 自动感知能力的探测规范。
func buildCheck(name string, pinger Pinger, critical bool, defaultDriver ...string) ready.Check {
	opts := []ready.CheckOption{
		ready.WithCritical(critical),
	}
	if len(defaultDriver) > 0 && defaultDriver[0] != "" {
		opts = append(opts, ready.WithDriver(defaultDriver[0]))
	}
	if da, ok := pinger.(interface{ Driver() string }); ok {
		opts = append(opts, ready.WithDriver(da.Driver()))
	}
	if ea, ok := pinger.(interface{ Enabled() bool }); ok {
		opts = append(opts, ready.WithEnabled(ea.Enabled))
	}
	return ready.NewCheck(name, ready.PingerFunc(pinger), opts...)
}

// NewService 构造探针服务，注册默认的核心与可选组件探针。
func NewService(deps Deps) *Service {
	agg := ready.New(
		buildCheck(ComponentDatabase, deps.Database, true, "postgres"),
		buildCheck(ComponentCache, deps.Cache, false, "valkey"),
	)

	if deps.Storage != nil {
		agg.Register(buildCheck(ComponentStorage, deps.Storage, false))
	}

	if deps.Search != nil {
		agg.Register(buildCheck(ComponentSearch, deps.Search, false))
	}

	if deps.Realtime != nil {
		agg.Register(buildCheck(ComponentRealtime, deps.Realtime, false))
	}

	if deps.Imgproxy != nil {
		agg.Register(buildCheck(ComponentImgproxy, deps.Imgproxy, false))
	}

	if deps.Mailer != nil {
		agg.Register(buildCheck(ComponentMailer, deps.Mailer, false))
	}

	return &Service{aggregator: agg}
}

// Check 并发探测全部依赖的可达性状态，返回评估结果及失败错误字典。
func (s *Service) Check(ctx context.Context) (ready.Result, map[string]error) {
	return s.aggregator.Check(ctx)
}
