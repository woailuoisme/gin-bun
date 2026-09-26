package profile

import (
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"bgin/internal/platform/errs"
	"bgin/internal/platform/identity"
	"bgin/internal/platform/response"
)

// Handler 是本人档案端点的 HTTP 适配层。
type Handler struct {
	service *Service
}

// NewHandler 构造 Handler。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// GetProfile 处理 GET /v1/users/me。
//
// @Summary      读取本人档案
// @Description  获取当前登录用户的完整个人档案。
// @Tags         个人资料
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} Profile "本人档案"
// @x-order      1
// @Router       /users/me [get]
func (h *Handler) GetProfile(c *gin.Context) {
	id, ok := identity.FromContext(c.Request.Context())
	if !ok {
		_ = c.Error(errs.Unauthorized(errs.CodeAuthTokenInvalid, "Authentication is required"))
		return
	}

	profile, err := h.service.GetProfile(c.Request.Context(), id.UserID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OK(c, profile)
}

// UpdateProfile 处理 PATCH /v1/users/me。
//
// @Summary      更新本人档案
// @Description  局部更新个人资料，支持更新昵称、头像、用户名、展示名与手机号。
// @Tags         个人资料
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        payload body UpdateProfileRequest true "待更新字段，可只提交其中一部分"
// @Success      200 {object} Profile "更新后的档案"
// @x-order      2
// @Router       /users/me [patch]
func (h *Handler) UpdateProfile(c *gin.Context) {
	id, ok := identity.FromContext(c.Request.Context())
	if !ok {
		_ = c.Error(errs.Unauthorized(errs.CodeAuthTokenInvalid, "Authentication is required"))
		return
	}

	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		return
	}
	if err := binding.Validator.ValidateStruct(toSelfPatch(req)); err != nil {
		_ = c.Error(err)
		return
	}

	profile, err := h.service.UpdateProfile(c.Request.Context(), id.UserID, req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OK(c, profile)
}

// toSelfPatch 把三态请求投影为可复用的校验结构体。
func toSelfPatch(req UpdateProfileRequest) *selfPatch {
	return &selfPatch{
		Name:            req.Name.Value,
		Image:           req.Image.Value,
		Username:        req.Username.Value,
		DisplayUsername: req.DisplayUsername.Value,
		PhoneNumber:     req.PhoneNumber.Value,
	}
}
