package system

import "bgin/internal/platform/ready"

const (
	// StatusOK 是存活探针的健康状态值。
	StatusOK = "ok"

	// StatusReady、StatusDegraded 与 StatusNotReady 是就绪探针的状态值。
	StatusReady    = ready.StatusReady
	StatusDegraded = ready.StatusDegraded
	StatusNotReady = ready.StatusNotReady

	// CheckHealthy、CheckDegraded、CheckUnreachable 与 CheckDisabled 是依赖组件的状态值。
	CheckHealthy     = ready.CheckHealthy
	CheckDegraded    = ready.CheckDegraded
	CheckUnreachable = ready.CheckUnreachable
	CheckDisabled    = ready.CheckDisabled
)

// HealthResponse 是 GET /livez 与 GET / 的响应体。
type HealthResponse struct {
	Status string `json:"status"`
}

// ReadinessResponse 是 GET /readyz 的响应体，完全兼容 ready.Result。
type ReadinessResponse = ready.Result

// ComponentCheck 是单个依赖的探测状态元信息，完全兼容 ready.ComponentStatus。
type ComponentCheck = ready.ComponentStatus
