// Package codec 提供底层数据交换与序列化适配器。
package codec

import (
	"io"

	"github.com/gin-gonic/gin/codec/json"
	gojson "github.com/goccy/go-json"
)

// GoJSONAdapter 把 goccy/go-json 适配为 Gin 官方 json.Core 接口。
type GoJSONAdapter struct{}

var _ json.Core = GoJSONAdapter{}

// Marshal 将对象序列化为 JSON 字节切片。
func (GoJSONAdapter) Marshal(v any) ([]byte, error) {
	return gojson.Marshal(v)
}

// Unmarshal 将 JSON 字节切片反序列化到目标对象。
func (GoJSONAdapter) Unmarshal(data []byte, v any) error {
	return gojson.Unmarshal(data, v)
}

// MarshalIndent 将对象带缩进序列化。
func (GoJSONAdapter) MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return gojson.MarshalIndent(v, prefix, indent)
}

// NewEncoder 创建 JSON 流编码器。
func (GoJSONAdapter) NewEncoder(w io.Writer) json.Encoder {
	return gojson.NewEncoder(w)
}

// NewDecoder 创建 JSON 流解码器。
func (GoJSONAdapter) NewDecoder(r io.Reader) json.Decoder {
	return gojson.NewDecoder(r)
}

// EnableGoJSON 把 Gin 全局运行时 json.API 替换为高性能 go-json 适配器。
func EnableGoJSON() {
	json.API = GoJSONAdapter{}
}
