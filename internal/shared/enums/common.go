// Package enums 定义系统跨领域通用枚举与常量。
package enums

import (
	"fmt"
	"strings"

	platformenums "bgin/internal/platform/enums"
)

// 通用颜色常量定义，用于枚举元数据展示与徽标渲染。
//
// 定义已下沉到平台层的 platform/enums：支付枚举同样需要这套词汇，
// 放在平台层可让 platform/payment 直接复用而不必反向依赖 shared。
// 这里保留同名别名，维持本包既有调用点与对外常量的稳定性。
const (
	ColorPrimary   = platformenums.ColorPrimary
	ColorSecondary = platformenums.ColorSecondary
	ColorSuccess   = platformenums.ColorSuccess
	ColorDanger    = platformenums.ColorDanger
	ColorWarning   = platformenums.ColorWarning
	ColorInfo      = platformenums.ColorInfo
	ColorGray      = platformenums.ColorGray
	ColorPurple    = platformenums.ColorPurple
	ColorOrange    = platformenums.ColorOrange
)

// 通用状态与操作标签常量，定义见 platform/enums。
const (
	LabelPending = platformenums.LabelPending
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
