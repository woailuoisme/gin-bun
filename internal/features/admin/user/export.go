package admin

import (
	"context"
	"io"
	"time"

	"bgin/internal/platform/excel"
	"bgin/internal/shared/models"
)

// UsersExport 封装用户数据列表导出为 Excel 文件的声明与格式化映射。
type UsersExport struct {
	users []*models.User
}

// NewUsersExport 创建用户数据导出定义实例。
func NewUsersExport(users []*models.User) *UsersExport {
	return &UsersExport{users: users}
}

// FromSlice 返回待导出的切片数据源。
func (e *UsersExport) FromSlice() any {
	return e.users
}

// Headings 定义 Excel 导出表头。
func (e *UsersExport) Headings() []string {
	return []string{"ID", "Name", "Email", "Role", "Status", "Created At"}
}

// Mapping 将单条 User 实体映射为单元格数据行。
func (e *UsersExport) Mapping(row any) []any {
	var user *models.User
	switch u := row.(type) {
	case *models.User:
		user = u
	case models.User:
		user = &u
	default:
		return nil
	}

	if user == nil {
		return nil
	}

	createdAtStr := ""
	if !user.CreatedAt.IsZero() {
		createdAtStr = user.CreatedAt.Format(time.RFC3339)
	}

	return []any{
		user.ID,
		user.Name,
		user.Email,
		user.Role,
		user.Status,
		createdAtStr,
	}
}

// Title 定义 Excel 工作表名称。
func (e *UsersExport) Title() string {
	return "Users"
}

// ExportStream 辅助函数：将用户切片直接流式序列化并写入指定的 io.Writer。
func ExportStream(ctx context.Context, w io.Writer, users []*models.User) error {
	return excel.Export(ctx, NewUsersExport(users), w)
}

// 确保 UsersExport 实现了标准 Excel 导出协议的接口规范。
var (
	_ excel.FromSlice    = (*UsersExport)(nil)
	_ excel.WithHeadings = (*UsersExport)(nil)
	_ excel.WithMapping  = (*UsersExport)(nil)
	_ excel.WithTitle    = (*UsersExport)(nil)
)
