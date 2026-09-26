package admin

import (
	"bgin/internal/database/model"
	"bgin/internal/shared/user"
)

// 本文件只负责"模型怎么变成响应体"：形状定义在 dto.go。
// 档案部分的投影由共享内核提供，本文件只追加管理端特有的审计字段。

// projectUser 把模型投影为管理视角的响应体。
func projectUser(entity *model.User) User {
	return User{
		Profile:         user.ProjectProfile(entity),
		Banned:          entity.Banned,
		BanReason:       entity.BanReason,
		BanExpires:      model.UTCPtr(entity.BanExpires),
		LastLoginAt:     model.UTCPtr(entity.LastLoginAt),
		LastLoginMethod: entity.LastLoginMethod,
	}
}
