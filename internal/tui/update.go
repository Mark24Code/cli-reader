package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.ensureSize(msg.Width, msg.Height)
		return m, nil
	case viewerDoneMsg:
		m.viewerBusy = false
		if msg.Err != nil {
			m.setNotice("图片查看器退出: "+msg.Err.Error(), true)
		} else {
			m.setNotice("已返回阅读", false)
		}
		return m, nil
	case copiedMsg:
		m.setNotice(fmt.Sprintf("已复制 %d 个字符", msg.Chars), false)
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	if m.mode == modeInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.selection = selectionState{}
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.tooSmall {
		if key == "q" {
			return m, tea.Quit
		}
		return m, nil
	}
	if m.viewerBusy {
		return m, nil
	}
	if m.modal.kind != modalNone {
		return m.handleModalKey(key)
	}
	if m.mode == modeInput {
		switch key {
		case "enter":
			return m, m.submitInput()
		case "esc":
			m.leaveInput()
			return m, nil
		case "up":
			m.handleCommandHistory(true)
			return m, nil
		case "down":
			m.handleCommandHistory(false)
			return m, nil
		case "ctrl+u":
			m.input.SetValue("")
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	switch key {
	case "up", "k":
		m.scrollUp(false)
	case "down", "j":
		m.scrollDown(false)
	case "pgup":
		m.scrollUp(true)
	case "pgdown", " ":
		m.scrollDown(true)
	case "home", "g":
		m.gotoTop()
		m.setNotice("已到章首", false)
	case "end", "G":
		m.gotoBottom()
		m.setNotice("已到章尾", false)
	case "left", "h":
		m.prevSection()
	case "right", "l":
		m.nextSection()
	case "n":
		m.nextChapter(false)
	case "p":
		m.prevChapter(false)
	case "t", "ctrl+t":
		m.openModal(modalTOC)
	case "f":
		m.togglePositionFavorite()
	case "F":
		m.openModal(modalFavorites)
	case "s":
		m.toggleChapterFavorite()
	case "b", "ctrl+b":
		m.goBack()
	case "?", "f1", "ctrl+h":
		m.openModal(modalHelp)
	case "a":
		m.openModal(modalAbout)
	case "enter":
		if m.findImage(0) != nil {
			return m, m.openImage(0)
		}
		return m, m.enterInput()
	case "o":
		return m, m.openImage(0)
	case "/", ":", "i":
		return m, m.enterInput()
	case "q":
		return m, tea.Quit
	case "esc":
		m.setNotice("", false)
	}
	return m, nil
}

func (m Model) handleModalKey(key string) (tea.Model, tea.Cmd) {
	if key == "esc" {
		m.modal = modalState{}
		return m, nil
	}
	switch m.modal.kind {
	case modalTOC:
		switch key {
		case "up", "k":
			m.modalMove(-1)
		case "down", "j":
			m.modalMove(1)
		case "pgup":
			m.modalMove(-m.modalListRows())
		case "pgdown":
			m.modalMove(m.modalListRows())
		case "home", "g":
			m.modal.cursor = 0
			m.modal.offset = 0
		case "end", "G":
			if n := len(m.book.TOC); n > 0 {
				m.modal.cursor = n - 1
				m.ensureModalCursorVisible()
			}
		case "enter":
			if len(m.book.TOC) == 0 {
				m.modal = modalState{}
				m.setNotice("这本书没有可用目录", true)
				return m, nil
			}
			entry := m.book.TOC[m.modal.cursor]
			m.modal = modalState{}
			if entry.Chapter < 0 {
				m.setNotice("该目录项指向正文之外的内容", true)
				return m, nil
			}
			m.goToChapter(entry.Chapter, entry.Fragment, 0, false, true)
		case "s":
			m.toggleTOCChapterFavorite()
		}
	case modalFavorites:
		switch key {
		case "up", "k":
			m.modalMove(-1)
		case "down", "j":
			m.modalMove(1)
		case "pgup":
			m.modalMove(-m.modalListRows())
		case "pgdown":
			m.modalMove(m.modalListRows())
		case "home", "g":
			m.modal.cursor = 0
			m.modal.offset = 0
		case "end", "G":
			if n := len(m.bookState.Favorites); n > 0 {
				m.modal.cursor = n - 1
				m.ensureModalCursorVisible()
			}
		case "enter":
			m.jumpToFavorite(m.modal.cursor)
		case "d", "delete", "backspace":
			m.deleteFavorite(m.modal.cursor)
		}
	case modalHelp, modalAbout:
		switch key {
		case "up", "k":
			if m.modal.offset > 0 {
				m.modal.offset--
			}
		case "down", "j":
			m.modal.offset++
		case "pgup":
			m.modal.offset -= m.modalListRows()
		case "pgdown":
			m.modal.offset += m.modalListRows()
		case "home", "g":
			m.modal.offset = 0
		case "end", "G":
			m.modal.offset = max(0, len(m.scrollModalLines())-m.modalListRows())
		}
		m.clampModalOffset()
	}
	return m, nil
}

func (m *Model) clampModalOffset() {
	switch m.modal.kind {
	case modalTOC:
		m.ensureModalCursorVisible()
	case modalFavorites:
		m.ensureModalCursorVisible()
	case modalHelp, modalAbout:
		maxOff := max(0, len(m.scrollModalLines())-m.modalListRows())
		if m.modal.offset < 0 {
			m.modal.offset = 0
		}
		if m.modal.offset > maxOff {
			m.modal.offset = maxOff
		}
	}
}

// scrollModalLines returns the full text of a scrollable modal.
func (m Model) scrollModalLines() []string {
	if m.modal.kind == modalAbout {
		return m.aboutLines(m.width)
	}
	return m.helpLines()
}

func (m *Model) modalMove(delta int) {
	count := 0
	switch m.modal.kind {
	case modalTOC:
		count = len(m.book.TOC)
	case modalFavorites:
		count = len(m.bookState.Favorites)
	}
	if count == 0 {
		return
	}
	m.modal.cursor += delta
	if m.modal.cursor < 0 {
		m.modal.cursor = 0
	}
	if m.modal.cursor >= count {
		m.modal.cursor = count - 1
	}
	m.ensureModalCursorVisible()
}

func (m *Model) ensureModalCursorVisible() {
	rows := m.modalListRows()
	if m.modal.cursor < m.modal.offset {
		m.modal.offset = m.modal.cursor
	}
	if rows > 0 && m.modal.cursor >= m.modal.offset+rows {
		m.modal.offset = m.modal.cursor - rows + 1
	}
	if m.modal.offset < 0 {
		m.modal.offset = 0
	}
}

func (m *Model) toggleTOCChapterFavorite() {
	if len(m.book.TOC) == 0 || m.modal.cursor < 0 || m.modal.cursor >= len(m.book.TOC) {
		return
	}
	entry := m.book.TOC[m.modal.cursor]
	if entry.Chapter < 0 {
		return
	}
	ch := m.book.Chapters[entry.Chapter]
	m.bookState.ToggleChapterFavorite(ch.Href, ch.Title)
	m.syncLast()
	_ = m.store.Save()
}

func (m *Model) jumpToFavorite(index int) {
	if index < 0 || index >= len(m.bookState.Favorites) {
		return
	}
	f := m.bookState.Favorites[index]
	idx := m.book.ChapterByHref(f.Href)
	m.modal = modalState{}
	if idx < 0 {
		m.setNotice("收藏对应的章节已不存在", true)
		return
	}
	percent := 0.0
	if f.Kind == "position" {
		percent = f.Percent
	}
	m.goToChapter(idx, "", percent, false, true)
	m.setNotice("已跳转到收藏 · "+m.favoriteTitle(f), false)
}

func (m *Model) deleteFavorite(index int) {
	if index < 0 || index >= len(m.bookState.Favorites) {
		return
	}
	m.bookState.RemoveFavorite(index)
	m.syncLast()
	if err := m.store.Save(); err != nil {
		m.setNotice("收藏保存失败: "+err.Error(), true)
	}
	if m.modal.cursor >= len(m.bookState.Favorites) {
		m.modal.cursor = max(0, len(m.bookState.Favorites)-1)
	}
	m.ensureModalCursorVisible()
}

func (m *Model) openModal(kind modalKind) {
	m.modal = modalState{kind: kind}
	m.input.Blur()
	m.mode = modeBrowse
	if kind == modalTOC {
		for i, entry := range m.book.TOC {
			if entry.Chapter == m.chapter {
				m.modal.cursor = i
				break
			}
		}
		m.ensureModalCursorVisible()
	}
	if kind == modalFavorites && len(m.bookState.Favorites) > 0 {
		m.modal.cursor = len(m.bookState.Favorites) - 1
		m.ensureModalCursorVisible()
	}
}
