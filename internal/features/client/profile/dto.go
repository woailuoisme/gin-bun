package profile

import (
	"github.com/aarondl/opt/omitnull"

	"bgin/internal/shared/user"
)

// ProfileResource 为共享内核用户档案视图的类型别名（供 Swagger 注解直接引用）。
type ProfileResource = user.Profile

// UpdateProfileBody 是 PATCH /v1/users/me 的局部更新请求体。
type UpdateProfileBody struct {
	// 用户展示昵称
	Name omitnull.Val[string] `json:"name" swaggertype:"string" extensions:"x-nullable" example:"Alex Mercer"`
	// 用户头像 URL
	Image omitnull.Val[string] `json:"image" swaggertype:"string" extensions:"x-nullable" example:"https://example.com/avatar.png"`
	// 唯一登录用户名 (3-30位)
	Username omitnull.Val[string] `json:"username" swaggertype:"string" extensions:"x-nullable" example:"alexmercer"`
	// 用户显示名称
	DisplayUsername omitnull.Val[string] `json:"displayUsername" swaggertype:"string" extensions:"x-nullable" example:"Alex Mercer"`
	// E.164 格式手机号
	PhoneNumber omitnull.Val[string] `json:"phoneNumber" swaggertype:"string" extensions:"x-nullable" example:"+8613800138000"`
}

// fieldSet 将请求转换为共享内核档案字段集。
func (b UpdateProfileBody) fieldSet() user.FieldSet { return user.FieldSet(b) }

// selfPatch 是自助更新的校验投影别名。
type selfPatch = user.ProfilePatch

type (
	Profile              = ProfileResource
	UpdateProfileRequest = UpdateProfileBody
)
