package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"claude-epub-reader/internal/epub"
	"claude-epub-reader/internal/imgterm"
	"claude-epub-reader/internal/state"
)

type modalKind int

const (
	modalNone modalKind = iota
	modalTOC
	modalFavorites
	modalHelp
)

type modalState struct {
	kind   modalKind
	cursor int
	offset int
}

func (m Model) modalListRows() int {
	rows := m.height - 12
	if rows < 3 {
		rows = 3
	}
	if rows > 24 {
		rows = 24
	}
	return rows
}

func (m *Model) helpLines() []string {
	protocol := m.protocol.Label()
	if m.protocol == imgterm.ProtocolNone {
		protocol += "（阅读页仍有字符缩略图）"
	}
	lines := []string{
		"快捷键 · 浏览",
		"  ↑/↓ 或 j/k        逐行滚动",
		"  PgUp/PgDn/Space   整页滚动",
		"  ←/→ 或 h/l        上一章 / 下一章",
		"  g / G             章首 / 章尾",
		"  Enter             打开视口内图片",
		"  t                 目录",
		"  f / s             收藏位置 / 收藏本章",
		"  F                 收藏列表",
		"  b                 返回上一个位置",
		"  / 或 i            输入对话命令",
		"  ? / F1            帮助",
		"  q                 退出",
		"",
		"对话命令",
		"  help / 帮助        打开本帮助",
		"  toc / 目录         打开目录",
		"  下一章 / 上一章    章节导航",
		"  goto 3 / 跳到第三章 按章节号跳转",
		"  收藏 / 收藏本章    添加位置/章节收藏",
		"  收藏列表           查看收藏",
		"  返回               回到上一个跳转位置",
		"  图片 2             打开本章第 2 张图片",
		"  目录 / 进度 / 退出 目录 / 状态 / 退出",
		"",
		"图片",
		"  协议：" + protocol,
		fmt.Sprintf("  终端最小尺寸：%d 列 × %d 行", MinWidth, MinHeight),
		"  缩略图是彩色字符；Enter 使用原生协议看高清图。",
		"",
		"状态文件",
		"  " + m.store.Path,
	}
	return lines
}

func (m Model) viewModal(base string) string {
	boxW := m.width - 8
	if boxW > 80 {
		boxW = 80
	}
	if boxW < 30 {
		boxW = 30
	}
	contentW := boxW - 6
	if contentW < 20 {
		contentW = 20
	}
	rows := m.modalListRows()

	var title, footer string
	var body []string
	switch m.modal.kind {
	case modalTOC:
		title = fmt.Sprintf("目录 · %s", m.book.Title)
		if contentW >= 56 {
			footer = "↑↓ 选择 · PgUp/PgDn 翻页 · Enter 跳转 · s 收藏章节 · Esc 关闭"
		} else {
			footer = "↑↓ 选择 · Enter 跳转 · s 收藏 · Esc 关闭"
		}
		body = m.tocRows(contentW, rows)
	case modalFavorites:
		title = "收藏"
		footer = "↑↓ 选择 · Enter 跳转 · d 删除 · Esc 关闭"
		body = m.favoriteRows(contentW, rows)
	case modalHelp:
		title = "帮助"
		footer = "↑↓ 或 PgUp/PgDn 滚动 · Esc 关闭"
		all := m.helpLines()
		off := m.modal.offset
		if off < 0 {
			off = 0
		}
		if off >= len(all) {
			off = max(0, len(all)-rows)
		}
		end := off + rows
		if end > len(all) {
			end = len(all)
		}
		for _, line := range all[off:end] {
			body = append(body, padPlain(fitPlain(line, contentW), contentW))
		}
	}
	if len(body) == 0 {
		body = append(body, padPlain(fitPlain("（空）", contentW), contentW))
	}

	lines := make([]string, 0, len(body)+5)
	lines = append(lines, padPlainANSI(truncLine(styleAccentBold.Render(title), contentW), contentW))
	lines = append(lines, "")
	lines = append(lines, body...)
	lines = append(lines, "")
	lines = append(lines, padPlainANSI(truncLine(styleMuted.Render(footer), contentW), contentW))
	box := styleModal.Render(strings.Join(lines, "\n"))
	return overlayModal(base, box, m.width, m.height)
}

func (m Model) tocRows(contentW, rows int) []string {
	if len(m.book.TOC) == 0 {
		return []string{padPlain(fitPlain("这本书没有目录信息", contentW), contentW)}
	}
	offset := m.modal.offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(m.book.TOC) {
		offset = max(0, len(m.book.TOC)-rows)
	}
	end := offset + rows
	if end > len(m.book.TOC) {
		end = len(m.book.TOC)
	}
	var out []string
	for i := offset; i < end; i++ {
		e := m.book.TOC[i]
		marker := " "
		if i == m.modal.cursor {
			marker = "❯"
		} else if e.Chapter == m.chapter {
			marker = "●"
		}
		star := " "
		if e.Chapter >= 0 && m.bookState.IsChapterFavorite(m.book.Chapters[e.Chapter].Href) {
			star = "★"
		}
		line := strings.Repeat("  ", e.Depth) + marker + " " + star + " " + e.Title
		line = padPlain(fitPlain(line, contentW), contentW)
		switch {
		case i == m.modal.cursor:
			line = styleSelected.Render(line)
		case e.Chapter < 0:
			line = styleMuted.Render(line)
		case e.Chapter == m.chapter:
			line = styleAccent.Render(line)
		}
		out = append(out, line)
	}
	return out
}

func (m Model) favoriteRows(contentW, rows int) []string {
	favs := m.bookState.Favorites
	if len(favs) == 0 {
		return []string{padPlain(fitPlain("还没有收藏。阅读时按 f 收藏位置，按 s 收藏整章。", contentW), contentW)}
	}
	offset := m.modal.offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(favs) {
		offset = max(0, len(favs)-rows)
	}
	end := offset + rows
	if end > len(favs) {
		end = len(favs)
	}
	var out []string
	for i := offset; i < end; i++ {
		f := favs[i]
		marker := " "
		if i == m.modal.cursor {
			marker = "❯"
		}
		kind := "章节"
		if f.Kind == state.FavoritePosition {
			kind = fmt.Sprintf("位置 %.0f%%", f.Percent*100)
		}
		title := epub.SanitizeText(f.ChapterTitle)
		if title == "" {
			title = f.Href
		}
		line := marker + " [" + kind + "] " + title
		line = padPlain(fitPlain(line, contentW), contentW)
		if i == m.modal.cursor {
			line = styleSelected.Render(line)
		}
		out = append(out, line)
	}
	return out
}

func overlayModal(base, box string, screenW, screenH int) string {
	if screenW <= 0 || screenH <= 0 {
		return base
	}
	bgLines := strings.Split(ansi.Strip(base), "\n")
	for len(bgLines) < screenH {
		bgLines = append(bgLines, "")
	}
	if len(bgLines) > screenH {
		bgLines = bgLines[:screenH]
	}
	boxLines := strings.Split(box, "\n")
	boxW := 0
	for _, line := range boxLines {
		if w := ansi.StringWidth(line); w > boxW {
			boxW = w
		}
	}
	boxH := len(boxLines)
	x := (screenW - boxW) / 2
	y := (screenH - boxH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	dim := lipgloss.NewStyle().Faint(true)
	out := make([]string, screenH)
	for row := 0; row < screenH; row++ {
		bg := padPlain(cutPlain(bgLines[row], screenW), screenW)
		boxIndex := row - y
		if boxIndex < 0 || boxIndex >= boxH {
			out[row] = dim.Render(bg)
			continue
		}
		left := padPlain(cutPlain(ansi.Cut(bg, 0, x), x), x)
		rightW := screenW - x - boxW
		if rightW < 0 {
			rightW = 0
		}
		right := padPlain(cutPlain(ansi.Cut(bg, x+boxW, screenW), rightW), rightW)
		boxLine := padPlainANSI(boxLines[boxIndex], boxW)
		out[row] = dim.Render(left) + boxLine + dim.Render(right)
	}
	return strings.Join(out, "\n")
}

func fitPlain(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

func cutPlain(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "")
}

func padPlain(s string, width int) string {
	w := ansi.StringWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func padPlainANSI(s string, width int) string {
	w := ansi.StringWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}
