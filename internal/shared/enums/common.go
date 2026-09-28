// Package enums 定义系统跨领域通用枚举与常量。
package enums

import (
	"fmt"
	"strings"
)

// EnumInfo 定义枚举项的通用元数据，包括取值、显示标签、徽标颜色与排序优先级。
type EnumInfo[T any] struct {
	Value    T
	Label    string
	Color    string
	Priority int
}

// FindByLabel 在枚举元数据字典中按显示标签查找对应的枚举值。
func FindByLabel[T comparable](label string, m map[T]EnumInfo[T]) (T, bool) {
	lower := strings.ToLower(strings.TrimSpace(label))
	for k, info := range m {
		if strings.ToLower(info.Label) == lower {
			return k, true
		}
	}
	var zero T
	return zero, false
}

// FindByLegacyValue 在兼容旧值字典中查找对应的枚举值。
func FindByLegacyValue[T any](val string, m map[string]T) (T, bool) {
	if res, ok := m[val]; ok {
		return res, true
	}
	lower := strings.ToLower(strings.TrimSpace(val))
	for k, v := range m {
		if strings.ToLower(k) == lower {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// CreateOptionsMap 根据枚举元数据字典构造前端选择器所需的选项映射。
func CreateOptionsMap[T comparable](m map[T]EnumInfo[T]) map[string]string {
	opts := make(map[string]string, len(m))
	for k, info := range m {
		opts[fmt.Sprint(k)] = info.Label
	}
	return opts
}

// CreateOptionsWithColorMap 根据枚举元数据字典构造带颜色样式的前端选择器选项映射。
func CreateOptionsWithColorMap[T comparable](m map[T]EnumInfo[T]) map[string]map[string]string {
	opts := make(map[string]map[string]string, len(m))
	for k, info := range m {
		opts[fmt.Sprint(k)] = map[string]string{
			"label": info.Label,
			"color": info.Color,
		}
	}
	return opts
}
