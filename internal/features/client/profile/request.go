package profile

import (
	"bgin/internal/platform/optional"
	"bgin/internal/shared/user"
)

// UpdateProfileRequest 是 PATCH /v1/users/me 的请求体。
// 邮箱不在可自助修改之列：改邮箱需要独立的 OTP 验证流程，本期只读（Q27）。
//
// 各字段的 swaggertype 标签用于修正文档：Optional 的三态在线上就是一个普通 JSON 字段，
// 若不覆盖，swag 会把泛型展开成 {present, value}，生成一份照做就会失败的文档。
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

// fieldSet 把请求投影成共有的档案字段集。
// 本请求的字段名与类型与 user.FieldSet 完全一致，因此可以直接转换。
func (r UpdateProfileRequest) fieldSet() user.FieldSet { return user.FieldSet(r) }

// selfPatch 是自助更新的校验投影，复用共有档案补丁的统一校验规则。
// 路由层的校验不直接作用于 UpdateProfileRequest，因为三态容器无法表达 min/max 之类的约束。
type selfPatch = user.ProfilePatch
