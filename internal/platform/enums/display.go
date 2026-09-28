// Package enums 定义枚举元数据在展示层复用的通用词汇（颜色、标签）。
//
// 它位于平台层且不依赖任何内部包，因此 shared/enums 与 platform/payment/enums
// 都能安全复用它，而不必让平台层反向依赖 shared 业务内核。
package enums

// 通用颜色常量定义，用于枚举元数据展示与徽标渲染。
const (
	ColorPrimary   = "primary"
	ColorSecondary = "secondary"
	ColorSuccess   = "success"
	ColorDanger    = "danger"
	ColorWarning   = "warning"
	ColorInfo      = "info"
	ColorGray      = "gray"
	ColorPurple    = "purple"
	ColorOrange    = "orange"
)

// 通用状态与操作标签常量。
const (
	LabelPending = "Pending"
)
