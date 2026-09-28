package core

import (
	"fmt"
	"io"
	"sync"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

// ProgressBar 提供自适应终端进度条，自动区分交互式终端 (动态渲染) 与非交互式环境 (离散里程碑输出)。
type ProgressBar struct {
	w             io.Writer
	model         progress.Model
	total         int64
	current       int64
	label         string
	interactive   bool
	startTime     time.Time
	lastMilestone int
	mu            sync.Mutex
	finished      bool
}

// NewProgressBar 构造全新终端进度条。
func NewProgressBar(cmd *cobra.Command, total int64, label string) *ProgressBar {
	if total < 0 {
		total = 0
	}

	return &ProgressBar{
		w:           ResolveWriter(cmd),
		model:       progress.New(progress.WithColors(lipgloss.Color("39"), lipgloss.Color("42")), progress.WithWidth(26), progress.WithoutPercentage()),
		total:       total,
		label:       label,
		interactive: IsInteractive(),
		startTime:   time.Now(),
	}
}

// Set 更新当前已完成数量并刷新进度条展示。
func (b *ProgressBar) Set(current int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return
	}

	if current > b.total && b.total > 0 {
		current = b.total
	}
	b.current = current

	ratio := 0.0
	if b.total > 0 {
		ratio = float64(b.current) / float64(b.total)
	}
	pct := int(ratio * 100)

	if b.interactive {
		rate := float64(b.current) / max(time.Since(b.startTime).Seconds(), 0.001)
		barStr := b.model.ViewAs(ratio)
		_, _ = fmt.Fprintf(b.w, "\r  %-14s %s %3d%% (%s/%s) %s/s",
			b.label,
			barStr,
			pct,
			humanize.Comma(b.current),
			humanize.Comma(b.total),
			humanize.Comma(int64(rate)),
		)
		return
	}

	// 非交互式终端 (如 CI 管道) 采用按 25% 离散里程碑打印，防止刷屏
	if pct >= b.lastMilestone+25 || (pct == 100 && b.lastMilestone < 100) {
		b.lastMilestone = (pct / 25) * 25
		_, _ = fmt.Fprintf(b.w, "  • %s: %d%% (%s/%s)\n",
			b.label,
			pct,
			humanize.Comma(b.current),
			humanize.Comma(b.total),
		)
	}
}

// Incr 增量更新已完成数量。
func (b *ProgressBar) Incr(delta int64) {
	b.Set(b.current + delta)
}

// Finish 标记进度条结束并清理输出行。
func (b *ProgressBar) Finish() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finished {
		return
	}
	b.finished = true

	if b.interactive {
		// 清理动态行，使后续输出紧随其后干净打印
		_, _ = fmt.Fprint(b.w, "\r\033[2K\r")
	}
}
