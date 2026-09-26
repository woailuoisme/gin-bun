// Package system 实现编排系统需要的两个探针：存活与就绪。
//
// 分工照 gin 官方文档（Server Config → Health Checks）：**存活只证明进程还活着，失败会导致
// 容器被重启；就绪才去探测外部依赖，失败只摘流量、不重启**。因此依赖探测一律不出现在存活
// 探针里——否则一次数据库抖动会演变成全体实例被反复重启。
//
// 两个端点都挂在根路径而非 /v1 之下，也不进 OpenAPI 文档：它们是编排系统的接口，
// 不是对外交付的 API 契约（见 main.go 的 swagger 注释与 ADR-0005）。
package system

import "github.com/gin-gonic/gin"

// RegisterRoutes 注册探针端点。
//
// 不接收 middleware：探针不参与限流与鉴权。它们必须能被编排系统无条件触发——
// 一旦探针自己会因为配额或令牌失败，依赖故障时就会出现"探针先倒"的连锁。
func RegisterRoutes(engine *gin.Engine, handler *Handler) {
	engine.GET("/healthz", handler.Liveness)
	engine.GET("/readyz", handler.Readiness)
}
