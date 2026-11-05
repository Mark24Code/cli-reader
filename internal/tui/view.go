package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"cli-epub-reader/internal/theme"
)

var (
	styleAccent     = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Accent))
	styleAccentBold = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Accent)).Bold(true)
	styleMuted      = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Muted))
	styleDim        = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Dim))
	styleText       = lipgloss.NewStyle()
	styleError      = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Error))
	styleSelected   = lipgloss.NewStyle().
			Background(lipgloss.Color(theme.Accent)).
			Foreground(lipgloss.Color("#1F1E1D")).
			Bold(true)
	styleModal = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(theme.Accent)).
			Padding(1, 2)
)

func (m Model) View() string {
	view := m.renderView()
	if m.selection.active && m.selection.moved {
		view = highlightSelection(view, m.selection)
	}
	return view
}

func (m Model) renderView() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.tooSmall {
		return m.viewTooSmall()
	}
	base := m.viewMain()
	if m.modal.kind != modalNone {
		return m.viewModal(base)
	}
	return base
}

func (m Model) viewMain() string {
	lines := make([]string, 0, m.height)
	lines = append(lines, m.headerLine())
	lines = append(lines, m.ruleLine())
	for _, line := range strings.Split(m.viewport.View(), "\n") {
		lines = append(lines, truncLine(line, m.width))
	}
	for len(lines) < 2+m.bodyHeight() {
		lines = append(lines, "")
	}
	lines = lines[:2+m.bodyHeight()]
	lines = append(lines, m.noticeLine())
	lines = append(lines, m.ruleLine())
	lines = append(lines, m.promptLine())
	lines = append(lines, m.hintLine())
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	for i := range lines {
		lines[i] = truncLine(lines[i], m.width)
	}
	return strings.Join(lines, "\n")
}

func (m Model) headerLine() string {
	total := len(m.book.Chapters)
	chapter := m.chapter + 1
	pct := m.overallPercent()
	filled, empty := progressParts(pct, 8)
	bar := styleAccent.Render(filled) + styleDim.Render(empty)
	right := bar + " " + styleMuted.Render(fmt.Sprintf("%.0f%% · %d/%d", pct*100, chapter, total))
	left := styleAccentBold.Render("✻ cli-epub-reader") + " " + styleMuted.Render("·") + " " + styleText.Render("《"+m.book.Title+"》")
	return joinSides(left, right, m.width)
}

func (m Model) ruleLine() string {
	if m.width <= 0 {
		return ""
	}
	return styleDim.Render(strings.Repeat("─", m.width))
}

func (m Model) noticeLine() string {
	if m.notice == "" {
		return ""
	}
	text := strings.ReplaceAll(m.notice, "\n", " ")
	style := styleAccent
	if m.noticeError {
		style = styleError
	}
	return truncLine("  "+style.Render("✻")+" "+text, m.width)
}

func (m Model) promptLine() string {
	if m.mode == modeInput {
		view := m.input.View()
		right := styleAccent.Render("输入中")
		return joinSides("  "+view, right, m.width)
	}
	left := "  " + styleAccent.Render("❯") + " " + styleMuted.Render("/ 输入消息或命令")
	right := styleDim.Render("浏览")
	return joinSides(left, right, m.width)
}

func (m Model) hintLine() string {
	var hint string
	if m.mode == modeInput {
		hint = "Enter 发送  ·  Esc 返回阅读  ·  ↑↓ 命令历史  ·  Ctrl+C 退出"
	} else if m.width < 80 {
		if m.findImage(0) != nil {
			hint = "↑↓ 滚动  ·  Enter 图片  ·  t 目录  ·  f 收藏  ·  / 对话  ·  ? 帮助"
		} else {
			hint = "↑↓ 滚动  ·  t 目录  ·  f 收藏  ·  / 对话  ·  ? 帮助  ·  q 退出"
		}
	} else {
		hint = "↑↓ 滚动  ·  ←→ 章节  ·  Enter 图片  ·  t 目录  ·  f 收藏  ·  b 返回  ·  / 对话  ·  ? 帮助"
		if m.findImage(0) == nil {
			hint = "↑↓ 滚动  ·  ←→ 章节  ·  t 目录  ·  f 收藏  ·  F 收藏列表  ·  b 返回  ·  / 对话  ·  ? 帮助"
		}
	}
	return truncLine("  "+styleDim.Render(hint), m.width)
}

func (m Model) overallPercent() float64 {
	if len(m.book.Chapters) == 0 {
		return 0
	}
	p := (float64(m.chapter) + m.currentPercent()) / float64(len(m.book.Chapters))
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	return p
}

func progressParts(p float64, width int) (string, string) {
	if width < 1 {
		width = 1
	}
	filled := int(p*float64(width) + 0.5)
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("▰", filled), strings.Repeat("▱", width-filled)
}

func (m Model) viewTooSmall() string {
	lines := []string{
		styleAccentBold.Render("✻ cli-epub-reader"),
		"",
		"终端窗口过小，请拉宽后再继续阅读",
		fmt.Sprintf("当前 %d 列 × %d 行 · 至少 %d 列 × %d 行", m.width, m.height, MinWidth, MinHeight),
		"拉宽后会自动恢复到原阅读位置",
		"",
		styleMuted.Render("q / Ctrl+C 退出"),
	}
	out := make([]string, m.height)
	for i := range out {
		out[i] = ""
	}
	top := (m.height - len(lines)) / 2
	if top < 0 {
		top = 0
	}
	for i, line := range lines {
		row := top + i
		if row >= m.height {
			break
		}
		plainW := ansi.StringWidth(line)
		left := (m.width - plainW) / 2
		if left < 0 {
			left = 0
		}
		out[row] = strings.Repeat(" ", left) + truncLine(line, m.width-left)
	}
	return strings.Join(out, "\n")
}

func joinSides(left, right string, width int) string {
	if width <= 0 {
		return ""
	}
	lw := ansi.StringWidth(left)
	rw := ansi.StringWidth(right)
	if rw >= width {
		return truncLine(right, width)
	}
	space := width - lw - rw
	if space < 1 {
		left = truncLine(left, width-rw-1)
		if left == "" {
			return right
		}
		lw = ansi.StringWidth(left)
		space = width - lw - rw
	}
	if space < 0 {
		space = 0
	}
	return left + strings.Repeat(" ", space) + right
}

func truncLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "")
}

func padVisible(s string, width int) string {
	w := ansi.StringWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}
