package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"cli-reader/internal/book"
	"cli-reader/internal/formats"
)

// ErrNoBooks reports that a directory holds no openable documents.
var ErrNoBooks = errors.New("目录中没有可打开的文档")

// PickBook lists openable documents in dir and lets the user choose one.
// Returning ok=false means the user cancelled.
func PickBook(dir string, only book.Format) (string, bool, error) {
	entries, err := formats.ListSupported(dir, only)
	if err != nil {
		return "", false, err
	}
	if len(entries) == 0 {
		return "", false, ErrNoBooks
	}
	p := pickerModel{
		dir:      dir,
		all:      entries,
		filtered: entries,
	}
	program := tea.NewProgram(p, tea.WithAltScreen())
	final, err := program.Run()
	if err != nil {
		return "", false, err
	}
	pm := final.(pickerModel)
	if pm.selected == "" {
		return "", false, nil
	}
	return pm.selected, true, nil
}

type pickerModel struct {
	dir      string
	all      []formats.Entry
	filtered []formats.Entry
	filter   string

	cursor int
	offset int

	width  int
	height int

	selected string
}

func (p pickerModel) Init() tea.Cmd { return nil }

func (p pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		p.ensureCursor()
		return p, nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return p, tea.Quit
		case tea.KeyEnter:
			if len(p.filtered) > 0 && p.cursor >= 0 && p.cursor < len(p.filtered) {
				p.selected = p.filtered[p.cursor].Path
				return p, tea.Quit
			}
			return p, nil
		case tea.KeyUp:
			p.move(-1)
		case tea.KeyDown:
			p.move(1)
		case tea.KeyPgUp:
			p.move(-p.listRows())
		case tea.KeyPgDown:
			p.move(p.listRows())
		case tea.KeyHome:
			p.cursor = 0
			p.ensureCursor()
		case tea.KeyEnd:
			if len(p.filtered) > 0 {
				p.cursor = len(p.filtered) - 1
				p.ensureCursor()
			}
		case tea.KeyBackspace:
			r := []rune(p.filter)
			if len(r) > 0 {
				p.filter = string(r[:len(r)-1])
				p.applyFilter()
			}
		case tea.KeyCtrlU:
			p.filter = ""
			p.applyFilter()
		case tea.KeyRunes:
			p.filter += string(msg.Runes)
			p.applyFilter()
		case tea.KeySpace:
			p.filter += " "
			p.applyFilter()
		}
	}
	return p, nil
}

func (p *pickerModel) applyFilter() {
	p.cursor = 0
	p.offset = 0
	needle := strings.ToLower(strings.TrimSpace(p.filter))
	if needle == "" {
		p.filtered = p.all
		return
	}
	p.filtered = nil
	for _, e := range p.all {
		if strings.Contains(strings.ToLower(e.Name), needle) {
			p.filtered = append(p.filtered, e)
		}
	}
}

func (p *pickerModel) move(delta int) {
	if len(p.filtered) == 0 {
		return
	}
	p.cursor += delta
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = len(p.filtered) - 1
	}
	p.ensureCursor()
}

func (p *pickerModel) listRows() int {
	rows := p.height - 6
	if rows < 3 {
		rows = 3
	}
	return rows
}

func (p *pickerModel) ensureCursor() {
	rows := p.listRows()
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+rows {
		p.offset = p.cursor - rows + 1
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

func (p pickerModel) View() string {
	if p.width <= 0 || p.height <= 0 {
		return ""
	}
	if p.width < 32 || p.height < 8 {
		return centerText(p.width, p.height, []string{
			styleAccentBold.Render("✻ cli-reader"),
			"",
			"终端窗口过小，请拉宽后继续",
			styleMuted.Render("q / Esc 退出"),
		})
	}
	lines := make([]string, 0, p.height)
	header := styleAccentBold.Render("✻ cli-reader") + " " + styleMuted.Render("·") + " " +
		styleText.Render("选择书籍") + " " + styleDim.Render("("+p.dir+")")
	right := styleMuted.Render(fmt.Sprintf("%d 个可打开文档", len(p.filtered)))
	lines = append(lines, joinSides("  "+header, right, p.width))
	lines = append(lines, styleDim.Render(strings.Repeat("─", p.width)))

	prompt := "  " + styleAccent.Render("❯") + " "
	if p.filter == "" {
		prompt += styleMuted.Render("输入文字过滤…") + styleAccent.Render("▏")
	} else {
		prompt += styleText.Render(p.filter) + styleAccent.Render("▏")
	}
	lines = append(lines, truncLine(prompt, p.width))

	rows := p.listRows()
	p.ensureCursor()
	if len(p.filtered) == 0 {
		lines = append(lines, "  "+styleMuted.Render("没有匹配的可打开文档"))
	} else {
		for i := 0; i < rows; i++ {
			idx := p.offset + i
			if idx >= len(p.filtered) {
				break
			}
			e := p.filtered[idx]
			label := fmt.Sprintf("  [%-6s] ", e.Format.Label())
			name := ansi.Truncate(e.Name, max(1, p.width-ansi.StringWidth(label)-2), "…")
			line := label + name
			if idx == p.cursor {
				lines = append(lines, styleSelected.Render(padVisible(line, p.width)))
			} else {
				lines = append(lines, "  "+styleAccent.Render(strings.TrimSpace(e.Format.Label())+"  ")+styleText.Render(e.Name))
			}
		}
	}
	for len(lines) < p.height-1 {
		lines = append(lines, "")
	}
	if len(lines) > p.height-1 {
		lines = lines[:p.height-1]
	}
	footer := "  ↑↓ 选择 · Enter 打开 · Esc 取消 · 输入文字过滤"
	lines = append(lines, truncLine("  "+styleDim.Render(footer), p.width))
	for len(lines) < p.height {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = truncLine(lines[i], p.width)
	}
	return strings.Join(lines, "\n")
}

func centerText(width, height int, lines []string) string {
	out := make([]string, height)
	top := (height - len(lines)) / 2
	if top < 0 {
		top = 0
	}
	for i, line := range lines {
		row := top + i
		if row >= height {
			break
		}
		plain := ansi.StringWidth(line)
		left := (width - plain) / 2
		if left < 0 {
			left = 0
		}
		out[row] = strings.Repeat(" ", left) + truncLine(line, width-left)
	}
	return strings.Join(out, "\n")
}
