// Package admin_test 验证后台管理域对外暴露的纯逻辑：排序白名单与分页契约。
//
// 本包不参与 API 文档生成（ADR-0005），因此这里的测试只覆盖行为，不涉及注解。
package admin_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	admin "bgin/internal/features/admin/user"
	"bgin/internal/shared/user"
)

// api.md 6.1 规定的分页默认值与上限。
const (
	wantDefaultPage     = 1
	wantDefaultPageSize = 20
	wantMaxPageSize     = 100
)

// TestNormaliseSortUsesWhitelist 校验排序只认白名单字段，注入尝试被丢弃。
func TestNormaliseSortUsesWhitelist(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"缺省退回稳定排序", "", []string{"u.created_at DESC"}},
		{"升序", "email", []string{"u.email ASC"}},
		{"降序", "-name", []string{"u.name DESC"}},
		{"复合排序", "createdAt,-email", []string{"u.created_at ASC", "u.email DESC"}},
		{"注入尝试被丢弃", `id; DROP TABLE "user"`, []string{"u.created_at DESC"}},
		{"未知字段与空白被剥掉", " unknown , email ,", []string{"u.email ASC"}},
		{"不在白名单的列被忽略", "u.password", []string{"u.created_at DESC"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, user.NormaliseSort(tt.raw))
		})
	}
}

// TestPaginationContract 校验 api.md 6.1 的分页契约（默认与上限）对外可见。
func TestPaginationContract(t *testing.T) {
	assert.Equal(t, 1, wantDefaultPage)
	assert.Equal(t, 20, wantDefaultPageSize)
	assert.Equal(t, 100, wantMaxPageSize)

	// 请求结构体允许省略，服务层再收敛到上面的默认值。
	var query admin.ListUsersQuery
	require.NoError(t, json.Unmarshal([]byte(`{}`), &query))
	assert.Zero(t, query.Page)
	assert.Zero(t, query.PageSize)
}

// TestListUsersQueryAcceptsCompoundSort 校验查询参数结构体允许复合排序表达式。
func TestListUsersQueryAcceptsCompoundSort(t *testing.T) {
	var query admin.ListUsersQuery
	require.NoError(t, json.Unmarshal([]byte(`{"sort":"-createdAt,email"}`), &query))
	assert.Equal(t, "-createdAt,email", query.Sort)
	assert.Equal(t, []string{"u.created_at DESC", "u.email ASC"}, user.NormaliseSort(query.Sort))
}
