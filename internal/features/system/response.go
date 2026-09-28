package system

// 本文件只定义探针端点的响应形状。
//
// 两个探针都不进 OpenAPI 文档：它们不在 /v1 之下，属于编排系统的接口而非对外契约
// （见 main.go 的 swagger 注释与 ADR-0005）。

const (
	// StatusOK 是存活探针的状态词。刻意保持既有取值不变：它已有消费方（集成测试与部署脚本）。
	StatusOK = "ok"
	// StatusReady 与 StatusNotReady 是就绪探针的两个状态词。
	StatusReady    = "ready"
	StatusNotReady = "not ready"
	// CheckHealthy 与 CheckUnreachable 是单个依赖的状态词。
	CheckHealthy     = "healthy"
	CheckUnreachable = "unreachable"
)

// HealthResponse 是 GET /healthz 的响应体。
type HealthResponse struct {
	Status string `json:"status"`
}

// ReadinessResponse 是 GET /readyz 的响应体：200 与 503 共用同一形状，只有 status 不同。
//
// 刻意不带探测到的原始错误：那常含目标地址、驱动名甚至库版本，属于内部信息
// （api.md 5.2 的原则同样适用）。具体原因写进服务端日志，排查时按 component 检索。
type ReadinessResponse struct {
	Status string                    `json:"status"`
	Checks map[string]ComponentCheck `json:"checks"`
}

// ComponentCheck 是单个依赖的探测状态。
type ComponentCheck struct {
	Status string `json:"status"`
}
