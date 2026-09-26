package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"bgin/internal/platform/response"
	"bgin/internal/shared/user"
)

// Handler 是管理端用户 CRUD 的 HTTP 适配层。
//
// 本包不参与 API 文档生成，因此这里不写任何 swag 注解（见 ADR-0005）。
type Handler struct {
	service *Service
}

// NewHandler 构造 Handler。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// ListUsers 处理 GET /v1/admin/users。
func (h *Handler) ListUsers(c *gin.Context) {
	var query ListUsersQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		_ = c.Error(err)
		return
	}

	items, meta, err := h.service.ListUsers(c.Request.Context(), query)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Paginated(c, items, meta)
}

// GetUser 处理 GET /v1/admin/users/:id。
func (h *Handler) GetUser(c *gin.Context) {
	entity, err := h.service.GetUser(c.Request.Context(), c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OK(c, entity)
}

// CreateUser 处理 POST /v1/admin/users。
func (h *Handler) CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		return
	}

	created, err := h.service.CreateUser(c.Request.Context(), req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Created(c, created)
}

// UpdateUser 处理 PATCH /v1/admin/users/:id。
func (h *Handler) UpdateUser(c *gin.Context) {
	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		return
	}
	if err := binding.Validator.ValidateStruct(toManagementPatch(req)); err != nil {
		_ = c.Error(err)
		return
	}

	updated, err := h.service.UpdateUser(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OK(c, updated)
}

// DeleteUser 处理 DELETE /v1/admin/users/:id，返回 204。
func (h *Handler) DeleteUser(c *gin.Context) {
	if err := h.service.DeleteUser(c.Request.Context(), c.Param("id")); err != nil {
		_ = c.Error(err)
		return
	}
	response.NoContent(c)
}

// toManagementPatch 把管理端三态请求投影为可复用的校验结构体。
func toManagementPatch(req UpdateUserRequest) *managementPatch {
	return &managementPatch{
		ProfilePatch: user.ProfilePatch{
			Name:            req.Name.Value,
			Image:           req.Image.Value,
			Username:        req.Username.Value,
			DisplayUsername: req.DisplayUsername.Value,
			PhoneNumber:     req.PhoneNumber.Value,
		},
		Role:       req.Role.Value,
		Status:     req.Status.Value,
		BanReason:  req.BanReason.Value,
		BanExpires: req.BanExpires.Value,
	}
}
