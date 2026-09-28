package system

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"bgin/internal/platform/response"
)

// probeTimeout 是就绪探针调度的总超时时限。
const probeTimeout = 2 * time.Second

// Handler 是探针端点的 HTTP 适配层。
type Handler struct {
	service *Service
	logger  *zerolog.Logger
}

// NewHandler 构造系统探针 Handler。
func NewHandler(service *Service, logger *zerolog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

// Index 处理 GET / 根路径请求。
func (h *Handler) Index(c *gin.Context) {
	response.OK(c, HealthResponse{Status: StatusOK})
}

// Liveness 处理 GET /livez 存活探针请求。
func (h *Handler) Liveness(c *gin.Context) {
	response.OK(c, HealthResponse{Status: StatusOK})
}

// Readiness 处理 GET /readyz 就绪探针请求：逐个探测依赖，任一不可达即返回 503。
func (h *Handler) Readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), probeTimeout)
	defer cancel()

	outcomes := h.service.Check(ctx)
	ready := true
	checks := make(map[string]ComponentCheck, len(outcomes))
	for _, outcome := range outcomes {
		if outcome.Err != nil {
			ready = false
			checks[outcome.Name] = ComponentCheck{Status: CheckUnreachable}
			h.logger.Warn().Err(outcome.Err).Str("component", outcome.Name).Msg("readiness probe failed")
			continue
		}
		checks[outcome.Name] = ComponentCheck{Status: CheckHealthy}
	}

	body := ReadinessResponse{Status: StatusReady, Checks: checks}
	if !ready {
		body.Status = StatusNotReady
		c.JSON(http.StatusServiceUnavailable, body)
		return
	}
	response.OK(c, body)
}
