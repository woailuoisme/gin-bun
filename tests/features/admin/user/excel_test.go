package admin_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	admin "bgin/internal/features/admin/user"
	"bgin/internal/shared/models"
)

func TestUsersExportContract(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	users := []*models.User{
		{
			Base:   models.Base{ID: "usr_001", CreatedAt: now},
			Name:   "Alice",
			Email:  "alice@example.com",
			Role:   models.RoleAdmin,
			Status: models.StatusActive,
		},
	}

	exp := admin.NewUsersExport(users)

	assert.Equal(t, "Users", exp.Title())
	assert.Equal(t, []string{"ID", "Name", "Email", "Role", "Status", "Created At"}, exp.Headings())
	assert.Equal(t, users, exp.FromSlice())

	row := exp.Mapping(users[0])
	require.Len(t, row, 6)
	assert.Equal(t, "usr_001", row[0])
	assert.Equal(t, "Alice", row[1])
	assert.Equal(t, "alice@example.com", row[2])
	assert.Equal(t, models.RoleAdmin, row[3])
	assert.Equal(t, models.StatusActive, row[4])
	assert.Equal(t, now.Format(time.RFC3339), row[5])

	// 验证流式导出到内存 buffer
	var buf bytes.Buffer
	err := admin.ExportStream(t.Context(), &buf, users)
	require.NoError(t, err)
	assert.Positive(t, buf.Len())
}

func TestUsersImportContract(t *testing.T) {
	t.Parallel()

	imp := admin.NewUsersImport()
	assert.Equal(t, 1, imp.HeadingRow())
	assert.Equal(t, 100, imp.BatchSize())
	assert.Equal(t, 0, imp.ImportedCount())

	// 测试数据列数不足
	_, err := imp.ToModel([]string{"OnlyName"})
	require.Error(t, err)

	// 测试空数据校验失败
	_, err = imp.ToModel([]string{"", "test@example.com"})
	require.Error(t, err)

	// 测试正常行解析映射
	modelAny, err := imp.ToModel([]string{"Bob Builder", "bob@example.com"})
	require.NoError(t, err)

	u, ok := modelAny.(*models.User)
	require.True(t, ok)
	assert.Equal(t, "Bob Builder", u.Name)
	assert.Equal(t, "bob@example.com", u.Email)
	assert.Equal(t, models.RoleUser, u.Role)
	assert.Equal(t, 1, imp.ImportedCount())
}
