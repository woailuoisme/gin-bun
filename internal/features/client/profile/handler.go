package profile

import (
	"cmp"
	"errors"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"

	"github.com/aarondl/opt/omitnull"
	"github.com/gabriel-vasile/mimetype"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"bgin/internal/platform/authentication"
	"bgin/internal/platform/errs"
	"bgin/internal/platform/response"
	"bgin/internal/shared/media"
	"bgin/internal/shared/user"
)

// Handler 是本人档案端点的 HTTP 适配层。
type Handler struct {
	userService  *user.Service
	mediaManager *media.Manager
}

// NewHandler 构造 Handler。
func NewHandler(userService *user.Service, mediaManager *media.Manager) *Handler {
	return &Handler{
		userService:  userService,
		mediaManager: mediaManager,
	}
}

// GetProfile 处理 GET /v1/users/me。
//
// @Summary      读取本人档案
// @Description  获取当前登录用户的完整个人档案。
// @Tags         个人资料
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} ProfileResource "本人档案"
// @x-order      1
// @Router       /users/me [get]
func (h *Handler) GetProfile(c *gin.Context) {
	id, ok := authentication.FromContext(c.Request.Context())
	if !ok {
		_ = c.Error(errs.Unauthorized(errs.CodeAuthTokenInvalid, "Authentication is required"))
		return
	}

	profile, err := h.userService.GetProfile(c.Request.Context(), id.UserID)
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
// @Param        payload body UpdateProfileBody true "待更新字段，可只提交其中一部分"
// @Success      200 {object} ProfileResource "更新后的档案"
// @x-order      2
// @Router       /users/me [patch]
func (h *Handler) UpdateProfile(c *gin.Context) {
	id, ok := authentication.FromContext(c.Request.Context())
	if !ok {
		_ = c.Error(errs.Unauthorized(errs.CodeAuthTokenInvalid, "Authentication is required"))
		return
	}

	var body UpdateProfileBody
	if err := c.ShouldBindJSON(&body); err != nil {
		_ = c.Error(err)
		return
	}
	if err := binding.Validator.ValidateStruct(toSelfPatch(body)); err != nil {
		_ = c.Error(err)
		return
	}

	profile, err := h.userService.UpdateProfile(c.Request.Context(), id.UserID, body.fieldSet(), user.ScopeClient)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OK(c, profile)
}

// toSelfPatch 把三态请求投影为可复用的校验结构体。
func toSelfPatch(body UpdateProfileBody) *selfPatch {
	patch := body.fieldSet().Patch()
	return &patch
}

// UploadAvatar 处理 POST /v1/users/me/avatar。
//
// @Summary      上传本人头像
// @Description  上传并更新当前登录用户的个人头像。仅允许上传不超过 2MB 的 JPEG、PNG 或 WebP 格式图片，自动按魔数头校验真实图像并清除历史旧头像。
// @Tags         个人资料
// @Accept       multipart/form-data
// @Produce      json
// @Security     BearerAuth
// @Param        avatar formData file true "头像图片文件 (最大 2MB，支持 JPEG/PNG/WebP)"
// @Success      200 {object} ProfileResource "更新头像后的本人档案"
// @x-order      3
// @Router       /users/me/avatar [post]
func (h *Handler) UploadAvatar(c *gin.Context) {
	id, ok := authentication.FromContext(c.Request.Context())
	if !ok {
		_ = c.Error(errs.Unauthorized(errs.CodeAuthTokenInvalid, "Authentication is required"))
		return
	}

	file, err := c.FormFile("avatar")
	if err != nil {
		file, err = c.FormFile("file")
	}
	if err != nil {
		_ = c.Error(errs.BadRequest("Avatar file is required in 'avatar' or 'file' field"))
		return
	}

	if err := validateAvatarFile(file); err != nil {
		_ = c.Error(err)
		return
	}

	if h.mediaManager == nil {
		_ = c.Error(errs.Internal(errors.New("media manager is not configured")))
		return
	}

	mediaRecord, err := h.mediaManager.Attach(
		c.Request.Context(),
		media.Model("user", id.UserID),
		"avatar",
		file,
		media.WithSingleFile(),
	)
	if err != nil {
		_ = c.Error(err)
		return
	}

	avatarURL := cmp.Or(h.mediaManager.Avatar(mediaRecord, 256), h.mediaManager.URL(mediaRecord), mediaRecord.FileName)

	profile, err := h.userService.UpdateProfile(
		c.Request.Context(),
		id.UserID,
		user.FieldSet{
			Image: omitnull.From(avatarURL),
		},
		user.ScopeClient,
	)
	if err != nil {
		_ = c.Error(err)
		return
	}

	response.OK(c, profile)
}

const maxAvatarSize = 2 * 1024 * 1024 // 2MB

// validateAvatarFile 校验头像文件的大小、扩展名与真实魔数。
func validateAvatarFile(file *multipart.FileHeader) error {
	if file == nil || file.Size <= 0 {
		return errs.BadRequest("Avatar file cannot be empty")
	}
	if file.Size > maxAvatarSize {
		return errs.BadRequest("Avatar file size must not exceed 2MB")
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(file.Filename), "."))
	switch ext {
	case "jpg", "jpeg", "png", "webp":
	default:
		return errs.BadRequest("Invalid image extension; only jpg, jpeg, png, and webp are allowed")
	}

	src, err := file.Open()
	if err != nil {
		return errs.BadRequest("Failed to open uploaded file")
	}
	defer func() { _ = src.Close() }()

	header := make([]byte, 512)
	n, err := io.ReadFull(src, header)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return errs.BadRequest("Failed to read uploaded file")
	}

	mtype := mimetype.Detect(header[:n])
	detectedMime := mtype.String()
	switch detectedMime {
	case "image/jpeg", "image/png", "image/webp":
		return nil
	default:
		return errs.BadRequest("Invalid image format; only JPEG, PNG, and WebP are allowed")
	}
}
