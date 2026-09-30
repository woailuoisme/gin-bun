package admin

import (
	"context"
	"io"
	"slices"
	"time"

	"bgin/internal/platform/excel"
	"bgin/internal/shared/models"
	"bgin/internal/shared/user"
)

// UserFetcher 声明按条件分页物理拉取用户实体的抽象契约（ISP），由 *user.Repository 满足。
type UserFetcher interface {
	List(ctx context.Context, filter user.ListFilter, offset, limit int) ([]models.User, error)
}

// UsersExport 封装用户数据列表导出为 Excel 文件的声明与格式化映射。
type UsersExport struct {
	users []*models.User
}

// NewUsersExport 创建用户数据导出定义实例。
// 采用防御性拷贝隔离输入切片，遵循 Uber Go 边界规范。
func NewUsersExport(users []*models.User) *UsersExport {
	return &UsersExport{users: slices.Clone(users)}
}

// FromSlice 返回待导出的切片数据源。
// 返回防御性拷贝，防止外部消费方修改切片底层元素。
func (e *UsersExport) FromSlice() any {
	return slices.Clone(e.users)
}

// Headings 定义 Excel 导出表头。
func (e *UsersExport) Headings() []string {
	return []string{"ID", "Name", "Email", "Role", "Status", "Created At"}
}

// Mapping 将单条 User 实体映射为单元格数据行。
func (e *UsersExport) Mapping(row any) []any {
	return mapUserRow(row)
}

// Title 定义 Excel 工作表名称。
func (e *UsersExport) Title() string {
	return "Users"
}

// ExportStream 辅助函数：将用户切片直接流式序列化并写入指定的 io.Writer。
func ExportStream(ctx context.Context, w io.Writer, users []*models.User, opts ...excel.ExportOption) error {
	return excel.Export(ctx, NewUsersExport(users), w, opts...)
}

const defaultUserChunkSize = 500

// UsersChunkedExport 封装基于物理分页查询的用户批量流式导出器，严格实现 excel.FromChunkedQuery。
// 借助 StreamWriter 与分批数据库拉取，保证在海量用户导出场景下内存维持常数阶 O(1)，杜绝 OOM。
type UsersChunkedExport struct {
	fetcher   UserFetcher
	filter    user.ListFilter
	chunkSize int
}

// NewUsersChunkedExport 创建基于物理分块查询的用户导出器。
func NewUsersChunkedExport(fetcher UserFetcher, filter user.ListFilter, chunkSize ...int) *UsersChunkedExport {
	size := defaultUserChunkSize
	if len(chunkSize) > 0 && chunkSize[0] > 0 {
		size = chunkSize[0]
	}
	return &UsersChunkedExport{
		fetcher:   fetcher,
		filter:    filter,
		chunkSize: size,
	}
}

// ChunkSize 返回单批次数据库物理分页拉取的记录条数。
func (e *UsersChunkedExport) ChunkSize() int {
	return e.chunkSize
}

// FetchChunk 按偏移量分批物理拉取用户数据，供 Excel 流式写入器消费。
func (e *UsersChunkedExport) FetchChunk(ctx context.Context, offset int, limit int) ([]any, error) {
	if e.fetcher == nil {
		return nil, nil
	}
	users, err := e.fetcher.List(ctx, e.filter, offset, limit)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}

	rows := make([]any, len(users))
	for i := range users {
		rows[i] = &users[i]
	}
	return rows, nil
}

// Headings 定义 Excel 导出表头。
func (e *UsersChunkedExport) Headings() []string {
	return []string{"ID", "Name", "Email", "Role", "Status", "Created At"}
}

// Mapping 将单条 User 实体映射为单元格数据行。
func (e *UsersChunkedExport) Mapping(row any) []any {
	return mapUserRow(row)
}

// Title 定义 Excel 工作表名称。
func (e *UsersChunkedExport) Title() string {
	return "Users"
}

// ExportChunkedStream 辅助函数：将查询条件分块物理拉取并直接流式序列化写入指定的 io.Writer。
func ExportChunkedStream(
	ctx context.Context,
	w io.Writer,
	fetcher UserFetcher,
	filter user.ListFilter,
	opts ...excel.ExportOption,
) error {
	return excel.Export(ctx, NewUsersChunkedExport(fetcher, filter), w, opts...)
}

func mapUserRow(row any) []any {
	var target *models.User
	switch u := row.(type) {
	case *models.User:
		target = u
	case models.User:
		target = &u
	default:
		return nil
	}

	if target == nil {
		return nil
	}

	createdAtStr := ""
	if !target.CreatedAt.IsZero() {
		createdAtStr = target.CreatedAt.Format(time.RFC3339)
	}

	return []any{
		target.ID,
		target.Name,
		target.Email,
		target.Role,
		target.Status,
		createdAtStr,
	}
}

// 确保 UsersExport 与 UsersChunkedExport 实现了标准 Excel 导出协议的接口规范。
var (
	_ excel.FromSlice        = (*UsersExport)(nil)
	_ excel.WithHeadings     = (*UsersExport)(nil)
	_ excel.WithMapping      = (*UsersExport)(nil)
	_ excel.WithTitle        = (*UsersExport)(nil)
	_ excel.FromChunkedQuery = (*UsersChunkedExport)(nil)
	_ excel.WithHeadings     = (*UsersChunkedExport)(nil)
	_ excel.WithMapping      = (*UsersChunkedExport)(nil)
	_ excel.WithTitle        = (*UsersChunkedExport)(nil)
)
