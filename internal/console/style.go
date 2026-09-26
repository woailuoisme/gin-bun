package console

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

// 控制台输出样式。CLI 只负责提示，业务输出一律走 zerolog。
var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

// title 渲染并打印一行标题。
//
// 它与 success / warn 一样直接输出：调用方是按语句使用的（db:fresh 的三个阶段提示），
// 早先这个函数只返回字符串，导致那几行提示被静默丢弃、一个都不打印。
func title(text string) {
	fmt.Println(titleStyle.Render(text))
}

// success 渲染一行成功提示。
func success(format string, args ...any) {
	fmt.Println(successStyle.Render(fmt.Sprintf(format, args...)))
}

// warn 渲染一行告警提示。
func warn(format string, args ...any) {
	fmt.Println(warnStyle.Render(fmt.Sprintf(format, args...)))
}
