package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"bgin/internal/platform/response"
	"bgin/internal/shared/user"
)

// Handler 是管理端用户 CRUD 的 HTTP 适配层。
type Handler struct {
	service *Service
}

// NewHandler 构造 Handler。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// ListUsers 处理 GET /v1/admin/users。
//
// @Summary      用户分页列表
// @Description  根据分页、关键字、状态与角色条件查询系统用户列表。
// @Tags         用户管理
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        query query ListUsersQuery false "查询与分页参数"
// @Success      200 {object} UserPage "用户分页列表"
// @x-order      1
// @Router       /admin/users [get]
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
//
// @Summary      获取用户详情
// @Description  根据用户唯一标识查询用户完整档案与管理审计信息。
// @Tags         用户管理
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "用户唯一标识" example("usr_01J9X4N8Y0ABCD")
// @Success      200 {object} User "用户完整档案"
// @x-order      2
// @Router       /admin/users/{id} [get]
func (h *Handler) GetUser(c *gin.Context) {
	entity, err := h.service.GetUser(c.Request.Context(), c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OK(c, entity)
}

// CreateUser 处理 POST /v1/admin/users。
//
// @Summary      创建系统用户
// @Description  管理员直接创建新用户账号，指定邮箱、口令、昵称与角色等信息。
// @Tags         用户管理
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        payload body CreateUserRequest true "创建用户载荷"
// @Success      201 {object} User "新建成功的用户对象"
// @x-order      3
// @Router       /admin/users [post]
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
//
// @Summary      更新用户信息
// @Description  支持局部字段三态更新，可修改昵称、头像、用户名、手机号、角色、状态及封禁属性。
// @Tags         用户管理
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "用户唯一标识" example("usr_01J9X4N8Y0ABCD")
// @Param        payload body UpdateUserRequest true "局部更新载荷"
// @Success      200 {object} User "更新后的用户对象"
// @x-order      4
// @Router       /admin/users/{id} [patch]
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
//
// @Summary      注销删除用户
// @Description  物理删除指定用户及其关联账号数据，并级联清理权限策略。
// @Tags         用户管理
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "用户唯一标识" example("usr_01J9X4N8Y0ABCD")
// @Success      204 "删除成功，无响应体"
// @x-order      5
// @Router       /admin/users/{id} [delete]
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
