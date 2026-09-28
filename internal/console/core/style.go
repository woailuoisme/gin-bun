package core

import (
	"errors"
	"fmt"
	"io"
	"os"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

// 通用 CLI 选项常量。
const (
	FlagForce      = "force"
	UsageForceFlag = "Skip interactive confirmation prompt"
)

// 控制台输出样式与语义化 Badge。
var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

// IsInteractive 返回当前标准输入和输出是否均为可交互终端。
func IsInteractive() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
}

// ResolveWriter 抽取目标输出流，优先使用 Cobra 命令自带的 OutOrStdout()。
func ResolveWriter(cmd *cobra.Command) io.Writer {
	if cmd != nil {
		return cmd.OutOrStdout()
	}
	return os.Stdout
}

// Title 渲染并打印一行标题至命令输出流。
func Title(cmd *cobra.Command, text string) {
	_, _ = fmt.Fprintln(ResolveWriter(cmd), titleStyle.Render("• "+text))
}

// Success 渲染一行成功提示至命令输出流。
func Success(cmd *cobra.Command, format string, args ...any) {
	_, _ = fmt.Fprintln(ResolveWriter(cmd), successStyle.Render("✓ "+fmt.Sprintf(format, args...)))
}

// Warn 渲染一行告警提示至命令输出流。
func Warn(cmd *cobra.Command, format string, args ...any) {
	_, _ = fmt.Fprintln(ResolveWriter(cmd), warnStyle.Render("! "+fmt.Sprintf(format, args...)))
}

// Info 渲染一行常规信息提示至命令输出流。
func Info(cmd *cobra.Command, format string, args ...any) {
	_, _ = fmt.Fprintln(ResolveWriter(cmd), fmt.Sprintf(format, args...))
}

// ErrCanceled 表示用户在交互式确认中主动放弃了操作。
//
// 它必须是一个可判定的错误而不是 nil：否则脚本无法区分"已取消"与"已执行"，
// 一次误按取消会被当成成功退出，掩盖后续流水线应当中断的事实。
var ErrCanceled = errors.New("operation canceled by user")

// Confirm 弹出交互式确认弹窗。在非交互式环境（CI/CD）下要求显式传入 --force。
func Confirm(title, description string, affirmative ...string) (bool, error) {
	if !IsInteractive() {
		return false, errors.New("confirmation required for destructive operation: please run interactively or specify --force")
	}

	aff := "Yes, continue"
	if len(affirmative) > 0 && affirmative[0] != "" {
		aff = affirmative[0]
	}
	var confirmed bool
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(title).
			Description(description).
			Affirmative(aff).
			Negative("Cancel").
			Value(&confirmed),
	))

	if err := form.Run(); err != nil {
		return false, fmt.Errorf("failed to read confirmation: %w", err)
	}
	return confirmed, nil
}
