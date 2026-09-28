package profile

import (
	"bgin/internal/platform/optional"
	"bgin/internal/shared/user"
)

// UpdateProfileRequest 是 PATCH /v1/users/me 的局部更新请求体。
type UpdateProfileRequest struct {
	// 用户展示昵称
	Name optional.Optional[string] `json:"name" swaggertype:"string" extensions:"x-nullable" example:"Alex Mercer"`
	// 用户头像 URL
	Image optional.Optional[string] `json:"image" swaggertype:"string" extensions:"x-nullable" example:"https://example.com/avatar.png"`
	// 唯一登录用户名 (3-30位)
	Username optional.Optional[string] `json:"username" swaggertype:"string" extensions:"x-nullable" example:"alexmercer"`
	// 用户显示名称
	DisplayUsername optional.Optional[string] `json:"displayUsername" swaggertype:"string" extensions:"x-nullable" example:"Alex Mercer"`
	// E.164 格式手机号
	PhoneNumber optional.Optional[string] `json:"phoneNumber" swaggertype:"string" extensions:"x-nullable" example:"+8613800138000"`
}

// fieldSet 将请求转换为共享内核档案字段集。
func (r UpdateProfileRequest) fieldSet() user.FieldSet { return user.FieldSet(r) }

// selfPatch 是自助更新的校验投影别名。
type selfPatch = user.ProfilePatch
