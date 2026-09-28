package system

const (
	// StatusOK 是存活探针的健康状态值。
	StatusOK = "ok"
	// StatusReady 与 StatusNotReady 是就绪探针的状态值。
	StatusReady    = "ready"
	StatusNotReady = "not ready"
	// CheckHealthy 与 CheckUnreachable 是依赖组件的状态值。
	CheckHealthy     = "healthy"
	CheckUnreachable = "unreachable"
)

// HealthResponse 是 GET /livez 与 GET / 的响应体。
type HealthResponse struct {
	Status string `json:"status"`
}

// ReadinessResponse 是 GET /readyz 的响应体。
type ReadinessResponse struct {
	Status string                    `json:"status"`
	Checks map[string]ComponentCheck `json:"checks"`
}

// ComponentCheck 是单个依赖的探测状态。
type ComponentCheck struct {
	Status string `json:"status"`
}
