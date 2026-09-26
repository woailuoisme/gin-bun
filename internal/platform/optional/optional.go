// Package optional 提供 JSON 局部更新所需的字段三态载体。
//
// 它只是一个绑定原语，不含任何业务语义：encoding/json 会把"字段缺省"与"显式 null"
// 都解成零值，而 api.md 2.3 要求两者语义不同（前者不改动，后者置空），
// 因此需要一个额外记录"字段是否出现"的容器。
package optional

import "encoding/json"

// Optional 表示一个字段的三态：缺省、显式 null、具体值。
//
// Present 表示该字段在 JSON 中是否出现；Present 为真而 Value 为 nil 即显式传了 null。
type Optional[T any] struct {
	// Present 表示该字段在 JSON 中是否出现。
	Present bool
	// Value 为 nil 且有 Present 时，表示客户端显式传了 null。
	Value *T
}

// UnmarshalJSON 记录字段是否出现。标准库不会为缺省字段调用本方法，因此 Present 可靠。
func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	o.Present = true
	if string(data) == "null" {
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

// IsClear 判断该字段是否被显式置空。
//
// 接收者统一为指针（与 UnmarshalJSON 一致）：同一类型的方法集混用值与指针接收者，
// 会让"哪些方法可用"取决于拿到的是值还是指针。
func (o *Optional[T]) IsClear() bool { return o.Present && o.Value == nil }

// IsSet 判断该字段是否被赋予了具体值。
func (o *Optional[T]) IsSet() bool { return o.Present && o.Value != nil }
