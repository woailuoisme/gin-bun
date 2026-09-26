package auth

import "bgin/internal/database/model"

// 本文件只负责"模型怎么变成响应体"：形状定义在 dto.go，业务用例在 service.go。
//
// 刻意不引入 Laravel 式的运行时 transformer / Resource 接口：认证端点只有一种用户视图，
// 哪个形状被返回完全由调用哪个投影函数在编译期决定，没有任何字段需要按请求条件出现。

// projectUser 把用户模型投影为认证端点的响应体。
func projectUser(user *model.User) UserProjection {
	return UserProjection{
		ID:              user.ID,
		Name:            user.Name,
		Email:           user.Email,
		EmailVerified:   user.EmailVerified,
		Image:           user.Image,
		Role:            user.Role,
		Username:        user.Username,
		DisplayUsername: user.DisplayUsername,
		PhoneNumber:     user.PhoneNumber,
		// 归一到 UTC：pgx 解出的 time.Local 会让响应体带出服务器本地偏移。
		CreatedAt: user.CreatedAt.UTC(),
		UpdatedAt: user.UpdatedAt.UTC(),
	}
}
