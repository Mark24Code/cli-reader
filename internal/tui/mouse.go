package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// wheelScrollLines is how many text lines one wheel notch moves.
const wheelScrollLines = 3

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.tooSmall || m.viewerBusy {
		return m, nil
	}
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.wheelScroll(-wheelScrollLines)
		return m, nil
	case tea.MouseButtonWheelDown:
		m.wheelScroll(wheelScrollLines)
		return m, nil
	}
	if msg.Button == tea.MouseButtonLeft {
		m.handleClick(msg.X, msg.Y)
	}
	return m, nil
}

func (m *Model) wheelScroll(lines int) {
	if lines == 0 {
		return
	}
	if m.modal.kind != modalNone {
		m.scrollModal(lines)
		return
	}
	step := 1
	if lines < 0 {
		step = -1
		lines = -lines
	}
	for i := 0; i < lines; i++ {
		before := m.chapter
		if step < 0 {
			m.scrollUp(false)
		} else {
			m.scrollDown(false)
		}
		// At a chapter boundary one notch pages into the neighbouring chapter;
		// do not keep cascading through several chapters.
		if m.chapter != before {
			break
		}
	}
}

func (m *Model) scrollModal(lines int) {
	switch m.modal.kind {
	case modalTOC, modalFavorites:
		m.modalMove(lines)
	case modalHelp:
		m.modal.offset += lines
		m.clampModalOffset()
	}
}

func (m *Model) handleClick(x, y int) {
	if m.modal.kind != modalNone {
		m.clickModal(x, y)
		return
	}
	if m.mode == modeInput || m.content == nil {
		return
	}
	const bodyTop = 2 // header + rule
	if y < bodyTop || y >= bodyTop+m.bodyHeight() {
		return
	}
	line := y - bodyTop + m.viewport.YOffset
	href, ok := m.content.LinkAt(line, x)
	if !ok {
		return
	}
	m.followLink(href)
}

func (m *Model) followLink(raw string) {
	if m.chapter < 0 || m.chapter >= len(m.book.Chapters) {
		return
	}
	base := m.book.Chapters[m.chapter].Href
	target := m.book.ResolveLink(base, raw)
	switch {
	case target.External:
		m.setNotice("外部链接未打开："+raw, true)
		return
	case target.Missing:
		m.setNotice("链接目标不在本书中："+raw, true)
		return
	case target.Chapter < 0:
		m.setNotice("链接指向正文之外的内容", true)
		return
	}
	m.goToChapter(target.Chapter, target.Fragment, 0, false, true)
}

func (m *Model) clickModal(x, y int) {
	box := m.modalBoxRect()
	if x < box.x || x >= box.x+box.width || y < box.y || y >= box.y+box.height {
		// Clicking outside the dialog closes it, like Esc.
		m.modal = modalState{}
		return
	}
	if x < box.bodyLeft || x >= box.bodyLeft+box.contentW {
		return
	}
	if y < box.bodyTop || y >= box.bodyTop+box.rows {
		return
	}
	row := m.modal.offset + (y - box.bodyTop)
	switch m.modal.kind {
	case modalTOC:
		if row < 0 || row >= len(m.book.TOC) {
			return
		}
		entry := m.book.TOC[row]
		m.modal.cursor = row
		if entry.Chapter < 0 {
			m.setNotice("该目录项指向正文之外的内容", true)
			return
		}
		m.modal = modalState{}
		m.goToChapter(entry.Chapter, entry.Fragment, 0, false, true)
	case modalFavorites:
		if row < 0 || row >= len(m.bookState.Favorites) {
			return
		}
		m.modal.cursor = row
		m.jumpToFavorite(row)
	}
}
