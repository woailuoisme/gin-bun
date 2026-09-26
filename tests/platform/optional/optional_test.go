// Package optional_test 验证 PATCH 局部更新所需的三态绑定语义（api.md 2.3）。
//
// 三态是契约的基础：把"字段缺省"与"显式 null"混淆会让 PUT 与 PATCH 语义无法区分。
package optional_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/platform/optional"
)

// patchSample 是 PATCH 三态语义的测试载体。
type patchSample struct {
	Name  optional.Optional[string] `json:"name"`
	Image optional.Optional[string] `json:"image"`
	Count optional.Optional[int]    `json:"count"`
}

// TestOptionalDistinguishesAbsentFromNull 校验"缺省"与"显式 null"可被区分。
func TestOptionalDistinguishesAbsentFromNull(t *testing.T) {
	var sample patchSample
	require.NoError(t, json.Unmarshal([]byte(`{"image":null,"count":0}`), &sample))

	assert.False(t, sample.Name.Present, "未出现的字段不能被当成待更新")
	assert.False(t, sample.Name.IsClear())
	assert.False(t, sample.Name.IsSet())

	assert.True(t, sample.Image.Present)
	assert.True(t, sample.Image.IsClear(), "显式 null 表示置空字段")

	assert.True(t, sample.Count.Present)
	assert.True(t, sample.Count.IsSet())
	assert.Equal(t, 0, *sample.Count.Value, "零值必须与缺省区分开")
}

// TestOptionalParsesConcreteValue 校验具体值被正确取出。
func TestOptionalParsesConcreteValue(t *testing.T) {
	var sample patchSample
	require.NoError(t, json.Unmarshal([]byte(`{"name":"Alice","count":7}`), &sample))

	require.True(t, sample.Name.IsSet())
	assert.Equal(t, "Alice", *sample.Name.Value)
	require.True(t, sample.Count.IsSet())
	assert.Equal(t, 7, *sample.Count.Value)
}

// TestOptionalRejectsWrongType 校验类型不匹配仍然报错而不是被静默吞掉。
func TestOptionalRejectsWrongType(t *testing.T) {
	var sample patchSample
	assert.Error(t, json.Unmarshal([]byte(`{"count":"not-a-number"}`), &sample))
}

// TestOptionalInSlice 校验嵌套在切片中依旧能区分三态。
func TestOptionalInSlice(t *testing.T) {
	var payload struct {
		Items []patchSample `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"items":[{"name":"a"},{"image":null}]}`), &payload))

	require.Len(t, payload.Items, 2)
	assert.True(t, payload.Items[0].Name.IsSet())
	assert.False(t, payload.Items[0].Image.Present)
	assert.True(t, payload.Items[1].Image.IsClear())
}
