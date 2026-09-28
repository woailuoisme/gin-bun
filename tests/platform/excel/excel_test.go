// Package excel_test 验证 Excel 与 CSV 流式导入导出与 Concern 契约。
package excel_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"bgin/internal/platform/excel"
)

type sampleUser struct {
	ID    string `excel:"用户编号"`
	Name  string `excel:"姓名"`
	Email string `excel:"邮箱"`
}

type userExport struct {
	users []sampleUser
}

func (u *userExport) FromSlice() any {
	return u.users
}

func (u *userExport) Headings() []string {
	return []string{"ID", "姓名", "邮箱"}
}

func (u *userExport) Mapping(row any) []any {
	user, ok := row.(sampleUser)
	if !ok {
		return nil
	}
	return []any{user.ID, user.Name, user.Email}
}

func (u *userExport) Title() string {
	return "用户列表"
}

func TestExportAndImportMemorySlice(t *testing.T) {
	ctx := t.Context()

	users := []sampleUser{
		{ID: "1", Name: "Alex", Email: "alex@example.com"},
		{ID: "2", Name: "Bob", Email: "bob@example.com"},
	}

	// 1. 测试导出
	var buf bytes.Buffer
	exportObj := &userExport{users: users}
	err := excel.Export(ctx, exportObj, &buf)
	require.NoError(t, err)
	assert.NotEmpty(t, buf.Bytes())

	// 2. 验证导出的 Excel 文件结构
	f, err := excelize.OpenReader(&buf)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	sheetName := "用户列表"
	rows, err := f.GetRows(sheetName)
	require.NoError(t, err)
	require.Len(t, rows, 3) // 表头 + 2行数据

	assert.Equal(t, []string{"ID", "姓名", "邮箱"}, rows[0])
	assert.Equal(t, []string{"1", "Alex", "alex@example.com"}, rows[1])
	assert.Equal(t, []string{"2", "Bob", "bob@example.com"}, rows[2])
}

type userImportRowCollector struct {
	rows [][]string
}

func (c *userImportRowCollector) OnRow(row []string) error {
	c.rows = append(c.rows, row)
	return nil
}

func TestImportRows(t *testing.T) {
	ctx := t.Context()

	// 构造待导入的 Excel 文件
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet := "Sheet1"
	require.NoError(t, f.SetSheetRow(sheet, "A1", &[]any{"ID", "姓名", "邮箱"}))
	require.NoError(t, f.SetSheetRow(sheet, "A2", &[]any{"101", "Charlie", "charlie@example.com"}))
	require.NoError(t, f.SetSheetRow(sheet, "A3", &[]any{"102", "David", "david@example.com"}))

	var buf bytes.Buffer
	require.NoError(t, f.Write(&buf))

	// 执行导入
	collector := &userImportRowCollector{}
	err := excel.Import(ctx, collector, &buf, excel.WithHeadingRowIndex(1))
	require.NoError(t, err)
	require.Len(t, collector.rows, 2)
	assert.Equal(t, []string{"101", "Charlie", "charlie@example.com"}, collector.rows[0])
	assert.Equal(t, []string{"102", "David", "david@example.com"}, collector.rows[1])
}

func TestExportSliceAndCSV(t *testing.T) {
	ctx := t.Context()

	items := []sampleUser{
		{ID: "u_1", Name: "Elena", Email: "elena@example.com"},
	}
	headings := []string{"ID", "Name", "Email"}
	mapper := func(u sampleUser) []any {
		return []any{u.ID, u.Name, u.Email}
	}

	// 1. 流式导出为 Excel
	var excelBuf bytes.Buffer
	err := excel.ExportSlice(ctx, &excelBuf, items, headings, mapper)
	require.NoError(t, err)
	assert.NotEmpty(t, excelBuf.Bytes())

	// 2. 流式导出为 CSV
	var csvBuf bytes.Buffer
	err = excel.ExportSliceCSV(ctx, &csvBuf, items, headings, mapper)
	require.NoError(t, err)

	csvContent := csvBuf.String()
	assert.Contains(t, csvContent, "ID,Name,Email")
	assert.Contains(t, csvContent, "u_1,Elena,elena@example.com")
}

func TestNilGuard(t *testing.T) {
	ctx := t.Context()

	require.Error(t, excel.Export(ctx, nil, nil))
	require.Error(t, excel.Import(ctx, nil, nil))
}
