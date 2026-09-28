package vector

import (
	"database/sql/driver"
	"fmt"
	"strings"

	"github.com/pgvector/pgvector-go"
	"github.com/samber/lo"
)

// Vector 表示高维浮点数特征向量，用于 AI Embedding 与空间相似度搜索。
type Vector struct {
	inner pgvector.Vector
}

// NewVector 从 float32 切片构造特征向量。
func NewVector(values []float32) Vector {
	return Vector{inner: pgvector.NewVector(values)}
}

// NewVectorFromFloat64 从 float64 切片构造特征向量（自动降精转换为 float32）。
func NewVectorFromFloat64(values []float64) Vector {
	f32s := lo.Map(values, func(item float64, _ int) float32 {
		return float32(item)
	})
	return NewVector(f32s)
}

// Parse 从向量格式字符串（如 "[1.0, 2.0, 3.0]"）解析出 Vector 实例。
func Parse(s string) (Vector, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	var inner pgvector.Vector
	if err := inner.Parse(s); err != nil {
		return Vector{}, fmt.Errorf("parse vector failed: %w", err)
	}
	return Vector{inner: inner}, nil
}

// Dim 返回向量的维度（即元素个数）。
func (v Vector) Dim() int {
	return len(v.inner.Slice())
}

// Slice 返回向量的底层 float32 切片副本。
func (v Vector) Slice() []float32 {
	return v.inner.Slice()
}

// IsEmpty 判定向量是否为空（维度为 0）。
func (v Vector) IsEmpty() bool {
	return v.Dim() == 0
}

// String 输出向量的标准文本表示（如 "[1,2,3]"）。
func (v Vector) String() string {
	return v.inner.String()
}

// Value 实现 driver.Valuer 接口，将向量输出为数据库驱动支持的值。
//
//nolint:nilnil // driver.Valuer returns (nil, nil) to write SQL NULL
func (v Vector) Value() (driver.Value, error) {
	if v.IsEmpty() {
		return nil, nil
	}
	return v.inner.Value()
}

// Scan 实现 sql.Scanner 接口，从数据库返回值中反序列化向量。
func (v *Vector) Scan(src any) error {
	if src == nil {
		v.inner = pgvector.Vector{}
		return nil
	}
	switch s := src.(type) {
	case string:
		cleaned := strings.ReplaceAll(strings.TrimSpace(s), " ", "")
		return v.inner.Scan(cleaned)
	case []byte:
		cleaned := strings.ReplaceAll(strings.TrimSpace(string(s)), " ", "")
		return v.inner.Scan(cleaned)
	default:
		return v.inner.Scan(src)
	}
}

// MarshalJSON 实现 json.Marshaler 接口，序列化为标准 JSON 浮点数组。
func (v Vector) MarshalJSON() ([]byte, error) {
	if v.IsEmpty() {
		return []byte("[]"), nil
	}
	return v.inner.MarshalJSON()
}

// UnmarshalJSON 实现 json.Unmarshaler 接口，宽容解析 JSON 数组。
func (v *Vector) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		v.inner = pgvector.Vector{}
		return nil
	}
	return v.inner.UnmarshalJSON(data)
}
