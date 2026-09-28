package console

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	bubbletable "charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	lipglosstable "charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"bgin/internal/bootstrap"
	"bgin/internal/config"
	"bgin/internal/console/core"
)

// RouteEntry 描述解析提取后的单条 HTTP 路由信息。
type RouteEntry struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	Handler    string `json:"handler"`
	RawHandler string `json:"raw_handler"`
}

// newRouteListCmd 构造列出所有已注册路由的命令。
func newRouteListCmd() *cobra.Command {
	var (
		methodFilter string
		pathFilter   string
		sortField    string
		reverse      bool
		jsonOutput   bool
		interactive  bool
	)

	cmd := &cobra.Command{
		Use:   "route:list",
		Short: "List all registered HTTP routes with methods and handlers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, cfgErr := config.Load()
			if cfgErr != nil || cfg == nil {
				// route list 属自省命令：配置不可用时用最小配置渲染路由表，
				// 但必须提示结果未必反映真实运行时的中间件行为。
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: load config failed, rendering routes with defaults: %v\n", cfgErr)
				cfg = config.Default()
			}

			engine, err := bootstrap.InspectEngine(cfg)
			if err != nil {
				return fmt.Errorf("failed to inspect engine routes: %w", err)
			}

			rawRoutes := engine.Routes()
			routes := make([]RouteEntry, 0, len(rawRoutes))
			for _, r := range rawRoutes {
				if methodFilter != "" && !strings.EqualFold(r.Method, methodFilter) {
					continue
				}
				if pathFilter != "" && !strings.Contains(strings.ToLower(r.Path), strings.ToLower(pathFilter)) {
					continue
				}

				routes = append(routes, RouteEntry{
					Method:     r.Method,
					Path:       r.Path,
					Handler:    formatHandlerName(r.Handler),
					RawHandler: r.Handler,
				})
			}

			// 排序规则
			slices.SortFunc(routes, func(a, b RouteEntry) int {
				var res int
				switch strings.ToLower(sortField) {
				case "method":
					res = strings.Compare(a.Method, b.Method)
					if res == 0 {
						res = strings.Compare(a.Path, b.Path)
					}
				case "handler":
					res = strings.Compare(a.Handler, b.Handler)
					if res == 0 {
						res = strings.Compare(a.Path, b.Path)
					}
				default: // "path"
					res = strings.Compare(a.Path, b.Path)
					if res == 0 {
						res = strings.Compare(a.Method, b.Method)
					}
				}
				if reverse {
					return -res
				}
				return res
			})

			if jsonOutput {
				data, err := json.MarshalIndent(routes, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal routes to JSON: %w", err)
				}
				_, _ = fmt.Fprintln(core.ResolveWriter(cmd), string(data))
				return nil
			}

			if interactive {
				return runInteractiveRouteTable(cmd, routes)
			}

			renderLipglossTable(cmd, routes)
			return nil
		},
	}

	cmd.Flags().StringVarP(&methodFilter, "method", "m", "", "Filter routes by HTTP method (GET, POST, etc.)")
	cmd.Flags().StringVarP(&pathFilter, "path", "p", "", "Filter routes by URI path pattern")
	cmd.Flags().StringVarP(&sortField, "sort", "s", "path", "Sort by column: path, method, handler")
	cmd.Flags().BoolVarP(&reverse, "reverse", "r", false, "Reverse route sorting order")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output route list as formatted JSON")
	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Explore routes interactively using bubbles/table TUI")

	return cmd
}

// formatHandlerName 将冗长的运行时函数路径精简为易读的结构体方法名。
func formatHandlerName(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "-"
	}
	s = strings.TrimSuffix(s, "-fm")
	s = strings.TrimPrefix(s, "bgin/internal/features/")
	s = strings.TrimPrefix(s, "bgin/internal/")
	s = strings.TrimPrefix(s, "bgin/")
	s = strings.ReplaceAll(s, ".(*Handler).", ".Handler.")
	return s
}

// methodStyle 根据 HTTP Method 返回带有语义化强调色的 Lipgloss Style。
func methodStyle(method string) lipgloss.Style {
	switch strings.ToUpper(method) {
	case "GET":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")) // Green
	case "POST":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")) // Yellow
	case "PUT":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")) // Blue/Cyan
	case "DELETE":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")) // Red
	case "PATCH":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("207")) // Magenta
	case "HEAD", "OPTIONS":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("244")) // Grey
	default:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))
	}
}

// renderLipglossTable 采用 lipgloss/table 渲染标准 ANSI 静态彩色表格。
func renderLipglossTable(cmd *cobra.Command, routes []RouteEntry) {
	writer := core.ResolveWriter(cmd)
	if len(routes) == 0 {
		core.Warn(cmd, "No routes found matching current filter criteria.")
		return
	}

	core.Title(cmd, fmt.Sprintf("HTTP Route List (%d routes registered)", len(routes)))

	rows := make([][]string, len(routes))
	for i, r := range routes {
		rows[i] = []string{r.Method, r.Path, r.Handler}
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("39"))

	borderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	t := lipglosstable.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(borderStyle).
		Headers("METHOD", "URI", "HANDLER").
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == lipglosstable.HeaderRow {
				return headerStyle
			}
			if col == 0 {
				return methodStyle(rows[row][0])
			}
			return lipgloss.NewStyle()
		})

	termWidth := 120
	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 40 {
		termWidth = w
	}
	t.Width(termWidth)

	_, _ = fmt.Fprintln(writer, t.Render())
	core.Info(cmd, "Total: %d routes", len(routes))
}

// routeTUIModel 是基于 bubbles/table 的全屏交互式 Bubble Tea 模型。
type routeTUIModel struct {
	table  bubbletable.Model
	routes []RouteEntry
}

func (m routeTUIModel) Init() tea.Cmd {
	return nil
}

func (m routeTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.table.SetWidth(msg.Width)
		m.table.SetHeight(max(10, msg.Height-5))
	}
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m routeTUIModel) View() tea.View {
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	help := helpStyle.Render(fmt.Sprintf("  [Total: %d]  ↑/k: Up • ↓/j: Down • q/esc: Quit", len(m.routes)))
	v := tea.NewView(m.table.View() + "\n" + help)
	v.AltScreen = true
	return v
}

// runInteractiveRouteTable 唤起全屏 bubbles/table 交互式浏览窗口。
func runInteractiveRouteTable(cmd *cobra.Command, routes []RouteEntry) error {
	if !core.IsInteractive() {
		core.Warn(cmd, "Interactive mode requires a TTY terminal, falling back to standard table.")
		renderLipglossTable(cmd, routes)
		return nil
	}

	columns := []bubbletable.Column{
		{Title: "METHOD", Width: 10},
		{Title: "URI", Width: 35},
		{Title: "HANDLER", Width: 45},
	}

	rows := make([]bubbletable.Row, len(routes))
	for i, r := range routes {
		rows[i] = bubbletable.Row{r.Method, r.Path, r.Handler}
	}

	styles := bubbletable.DefaultStyles()
	styles.Header = styles.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color("39"))
	styles.Selected = styles.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)

	t := bubbletable.New(
		bubbletable.WithColumns(columns),
		bubbletable.WithRows(rows),
		bubbletable.WithFocused(true),
		bubbletable.WithHeight(20),
		bubbletable.WithStyles(styles),
	)

	m := routeTUIModel{table: t, routes: routes}
	p := tea.NewProgram(m, tea.WithOutput(core.ResolveWriter(cmd)))
	_, err := p.Run()
	return err
}
