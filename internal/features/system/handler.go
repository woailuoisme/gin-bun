package system

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/samber/lo"

	"bgin/internal/platform/response"
)

// probeTimeout 是就绪探测的总时限。
//
// 必须有：挂住的数据库连接不该把探针本身一起拖死，否则编排系统等不到答复、只能靠超时判死，
// 那与直接返回未就绪相比既慢又难排查。gin 官方文档也把"给健康检查设超时"列为必须遵守的一条。
const probeTimeout = 2 * time.Second

// Handler 是探针端点的 HTTP 适配层。
type Handler struct {
	service *Service
	logger  *zerolog.Logger
}

// NewHandler 构造 Handler。
// logger 只承载探测失败的原始原因：那些细节进日志，绝不进响应体。
func NewHandler(service *Service, logger *zerolog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

// Liveness 处理 GET /healthz。
//
// 它只证明进程还在，一个字节都不碰外部依赖——存活探针失败会让容器被重启，
// 若它去查库，一次数据库抖动就会演变成全体实例被反复重启。
func (h *Handler) Liveness(c *gin.Context) {
	response.OK(c, HealthResponse{Status: StatusOK})
}

// Readiness 处理 GET /readyz：逐个探测依赖，任一不可达即 503，让编排系统摘掉本实例的流量。
//
// 与 errs 体系的关系：探针不套用对外错误模型——api.md 登记了 503 SERVICE_UNAVAILABLE，
// 但运维需要的是逐个组件的结果，因此这里返回自己的响应体，不经过 errs.Render。
func (h *Handler) Readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), probeTimeout)
	defer cancel()

	outcomes := h.service.Check(ctx)
	ready := lo.EveryBy(outcomes, func(outcome CheckOutcome) bool {
		return outcome.Err == nil
	})

	checks := make(map[string]ComponentCheck, len(outcomes))
	for _, outcome := range outcomes {
		if outcome.Err != nil {
			checks[outcome.Name] = ComponentCheck{Status: CheckUnreachable}
			// 原始错误常含目标地址与驱动名，只进日志——与 errs.Internal 把原因留给日志的处置一致。
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
