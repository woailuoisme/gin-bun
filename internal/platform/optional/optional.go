// Package optional 提供 JSON 局部更新所需的字段三态容器（缺省、显式 null、具体值）。
package optional

import (
	"bytes"
	"encoding/json"
)

// Optional 表示字段三态：缺省（Present=false）、显式 null（Present=true, Value=nil）、具体值。
type Optional[T any] struct {
	// Present 表示该字段在 JSON 中是否出现。
	Present bool
	// Value 字段具体值；Present 为 true 且 Value 为 nil 时表示显式 null。
	Value *T
}

// UnmarshalJSON 解析 JSON 字段三态。
func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	o.Present = true
	if bytes.Equal(data, []byte("null")) {
		o.Value = nil
		return nil
	}

	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	o.Value = &value
	return nil
}

// IsClear 判断该字段是否被显式置为 null。
func (o *Optional[T]) IsClear() bool { return o.Present && o.Value == nil }

// IsSet 判断该字段是否被赋予了非空具体值。
func (o *Optional[T]) IsSet() bool { return o.Present && o.Value != nil }
