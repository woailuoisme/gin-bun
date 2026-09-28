package profile

import (
	"bgin/internal/shared/user"
)

// Profile 是共享内核档案视图的类型别名，只为本模块的响应体提供一个可被注解引用的名字。
//
// 之方案需要这层别名：swag 解析注解里的类型名时，只对"同包类型"与少数被它登记过的包可靠，
// 直接写 {object} user.Profile 会报 "cannot find type definition"。别名让注解写成本包的
// Profile，而实际类型仍是共享内核那一份，字段定义不会因此出现第二份。
type Profile = user.Profile
