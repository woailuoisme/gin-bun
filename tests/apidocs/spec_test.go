// Package apidocs_test 把"哪些端点进文档、哪些不进"固化成回归测试。
//
// 规范本身由 `just docs` 生成，这里不校验它的细节，只校验最重要的一条边界：
// 面向调用方的端点必须收录，后台管理端点必须不收录（ADR-0005）。
// 有人给 admin 包加回 swag 注解时，这个测试会立刻失败。
package apidocs_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/apidocs"
)

// TestSpecCoversClientSurfaceOnly 校验规范的收录范围。
func TestSpecCoversClientSurfaceOnly(t *testing.T) {
	var spec struct {
		BasePath string                     `json:"basePath"`
		Paths    map[string]json.RawMessage `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(apidocs.Spec, &spec), "规范无法解析，可能需要重新执行 just docs")
	require.NotEmpty(t, spec.Paths)

	assert.Equal(t, "/v1", spec.BasePath)

	// 前端自助与认证端点必须在文档里；路径不含 /v1 前缀，因为 basePath 已经是它。
	for _, path := range []string{"/users/me", "/auth/sign-in/email", "/auth/token/refresh"} {
		assert.Contains(t, spec.Paths, path)
	}

	// 管理端刻意不参与文档生成。
	for path := range spec.Paths {
		assert.NotContains(t, path, "/admin/", "后台管理端点不应出现在对外文档里")
	}
}
