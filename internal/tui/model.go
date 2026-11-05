package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"cli-epub-reader/internal/epub"
	"cli-epub-reader/internal/imgterm"
	"cli-epub-reader/internal/render"
	"cli-epub-reader/internal/state"
)

const (
	MinWidth  = 56
	MinHeight = 16
)

type mode int

const (
	modeBrowse mode = iota
	modeInput
)

type location struct {
	Href    string
	Percent float64
}

type viewerDoneMsg struct {
	Err error
}

type Model struct {
	book      *epub.Book
	renderer  *render.Renderer
	store     *state.Store
	bookState *state.BookState
	bookKey   string
	protocol  imgterm.Protocol

	width, height int
	tooSmall      bool
	smallPercent  float64

	viewport viewport.Model
	input    textinput.Model
	mode     mode
	modal    modalState

	chapter int
	content *render.RenderedChapter

	// tocCursor remembers which TOC entry ←/→last landed on. It lets section
	// navigation keep stepping even when a short chapter clamps the viewport so
	// anchor lines cannot be compared directly. -1 means "derive from scroll".
	tocCursor int

	initialHref    string
	initialPercent float64
	hasInitial     bool
	hasRendered    bool

	notice      string
	noticeError bool

	history    []location
	cmdHistory []string
	cmdHistPos int

	viewerBusy bool
	selection  selectionState
}

func New(book *epub.Book, store *state.Store, key string, protocol imgterm.Protocol) Model {
	m := Model{
		book:       book,
		renderer:   render.NewRenderer(book),
		store:      store,
		bookKey:    key,
		protocol:   protocol,
		viewport:   viewport.New(0, 0),
		input:      textinput.New(),
		cmdHistPos: -1,
		tocCursor:  -1,
	}
	m.bookState = store.Book(key, book.Path, book.Title, book.Author)
	if idx := book.ChapterByHref(m.bookState.Last.Href); idx >= 0 {
		m.chapter = idx
		m.initialHref = m.bookState.Last.Href
		m.initialPercent = m.bookState.Last.Percent
		m.hasInitial = true
	}
	m.input.Prompt = "❯ "
	m.input.Placeholder = "输入消息或命令，例如：下一章、目录、help"
	m.input.CharLimit = 300
	m.input.PromptStyle = styleAccent
	m.input.TextStyle = styleText
	m.input.PlaceholderStyle = styleMuted
	m.input.Blur()

	if err := store.Warning(); err != nil {
		m.noticeError = true
		m.notice = err.Error()
	} else if m.hasInitial && m.initialPercent > 0.005 {
		m.notice = "已恢复上次阅读位置"
	} else {
		m.notice = "按 / 输入命令 · ? 查看帮助 · Enter 打开可见图片"
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) bodyHeight() int {
	h := m.height - 6
	if h < 1 {
		h = 1
	}
	return h
}

func (m *Model) ensureSize(width, height int) {
	oldPercent := m.currentPercent()
	wasSmall := m.tooSmall
	wasRendered := m.hasRendered
	widthChanged := width != m.width

	if width < MinWidth || height < MinHeight {
		if !m.tooSmall && wasRendered {
			m.smallPercent = oldPercent
		}
		m.width, m.height = width, height
		m.tooSmall = true
		return
	}

	m.width, m.height = width, height
	m.tooSmall = false
	m.viewport.Width = width
	m.viewport.Height = m.bodyHeight()
	m.input.Width = max(10, width-14)

	if !wasRendered {
		percent := 0.0
		if m.hasInitial {
			percent = m.initialPercent
		}
		m.renderAt(percent, "", false, false)
		m.hasRendered = true
		return
	}
	if m.modal.kind != modalNone {
		m.clampModalOffset()
	}
	if wasSmall {
		m.renderAt(m.smallPercent, "", false, false)
		return
	}
	if widthChanged {
		m.renderAt(oldPercent, "", false, false)
		return
	}
	// Height-only changes do not require retypesetting. Preserve approximate
	// progress while keeping line breaks identical.
	m.applyPercent(oldPercent)
}

func (m *Model) renderAt(percent float64, fragment string, atBottom, atTop bool) {
	if m.chapter < 0 || m.chapter >= len(m.book.Chapters) {
		return
	}
	ch := &m.book.Chapters[m.chapter]
	m.content = m.renderer.RenderChapter(ch, m.width, render.Options{})
	m.viewport.Width = m.width
	m.viewport.Height = m.bodyHeight()
	m.viewport.SetContent(strings.Join(m.content.Lines, "\n"))
	offset := 0
	switch {
	case fragment != "":
		if line, ok := m.content.LineForAnchor(fragment); ok {
			offset = line
		}
	case atBottom:
		offset = render.OffsetForPercent(len(m.content.Lines), m.bodyHeight(), 1)
	case atTop:
		offset = 0
	default:
		offset = render.OffsetForPercent(len(m.content.Lines), m.bodyHeight(), percent)
	}
	m.setOffset(offset)
	m.syncLast()
}

func (m *Model) applyPercent(percent float64) {
	if m.content == nil {
		return
	}
	m.setOffset(render.OffsetForPercent(len(m.content.Lines), m.bodyHeight(), percent))
	m.syncLast()
}

func (m *Model) setOffset(offset int) {
	max := len(m.content.Lines) - m.bodyHeight()
	if max < 0 {
		max = 0
	}
	if offset < 0 {
		offset = 0
	}
	if offset > max {
		offset = max
	}
	m.viewport.SetYOffset(offset)
}

func (m Model) currentPercent() float64 {
	if !m.hasRendered || m.content == nil {
		if m.hasInitial {
			return m.initialPercent
		}
		return 0
	}
	return render.ScrollPercent(len(m.content.Lines), m.bodyHeight(), m.viewport.YOffset)
}

func (m Model) currentLocation() location {
	href := ""
	if m.chapter >= 0 && m.chapter < len(m.book.Chapters) {
		href = m.book.Chapters[m.chapter].Href
	}
	return location{Href: href, Percent: m.currentPercent()}
}

func (m *Model) syncLast() {
	if m.bookState == nil || m.content == nil {
		return
	}
	m.bookState.Last = epub.Position{Href: m.book.Chapters[m.chapter].Href, Percent: m.currentPercent()}
}

func (m *Model) saveLast() error {
	m.syncLast()
	if m.store == nil {
		return nil
	}
	return m.store.Save()
}

func (m *Model) pushHistory() {
	hasContent := m.content != nil && m.hasRendered
	if !hasContent {
		return
	}
	loc := m.currentLocation()
	if n := len(m.history); n > 0 && m.history[n-1] == loc {
		return
	}
	m.history = append(m.history, loc)
	if len(m.history) > 50 {
		m.history = m.history[len(m.history)-50:]
	}
}

func (m *Model) goToChapter(index int, fragment string, percent float64, atBottom bool, push bool) {
	if index < 0 || index >= len(m.book.Chapters) {
		m.setNotice("章节不存在", true)
		return
	}
	if push {
		m.pushHistory()
	}
	m.chapter = index
	m.tocCursor = -1
	m.renderAt(percent, fragment, atBottom, false)
	ch := m.book.Chapters[index]
	m.setNotice(fmt.Sprintf("第 %d/%d 章 · %s", index+1, len(m.book.Chapters), ch.Title), false)
}

func (m *Model) goBack() {
	if len(m.history) == 0 {
		m.setNotice("没有可返回的位置", true)
		return
	}
	loc := m.history[len(m.history)-1]
	m.history = m.history[:len(m.history)-1]
	idx := m.book.ChapterByHref(loc.Href)
	if idx < 0 {
		m.setNotice("历史位置对应的章节已不存在", true)
		return
	}
	m.chapter = idx
	m.tocCursor = -1
	m.renderAt(loc.Percent, "", false, false)
	m.setNotice(fmt.Sprintf("已返回 · 第 %d 章", idx+1), false)
}

func (m *Model) nextChapter(atBottom bool) {
	if m.chapter >= len(m.book.Chapters)-1 {
		m.setNotice("已经是最后一章", false)
		return
	}
	m.goToChapter(m.chapter+1, "", 0, atBottom, true)
}

func (m *Model) prevChapter(atBottom bool) {
	if m.chapter <= 0 {
		m.setNotice("已经是第一章", false)
		return
	}
	m.goToChapter(m.chapter-1, "", 0, atBottom, true)
}

// tocIndex returns the TOC entry that best describes the current section. When
// ←/→ has already landed on an entry it keeps that cursor; otherwise the
// position is derived from the scroll offset.
func (m *Model) tocIndex() int {
	if m.tocCursor >= 0 && m.tocCursor < len(m.book.TOC) &&
		m.book.TOC[m.tocCursor].Chapter == m.chapter {
		return m.tocCursor
	}
	return m.tocIndexForPosition()
}

// tocIndexForPosition returns the TOC entry that best describes the visible
// reading position: the last entry in the current chapter whose anchor sits at
// or above the top of the viewport. It returns -1 when the book has no usable
// TOC entry for the current chapter.
func (m *Model) tocIndexForPosition() int {
	if m.content == nil || len(m.book.TOC) == 0 {
		return -1
	}
	top := m.viewport.YOffset
	if m.viewport.AtBottom() && len(m.content.Lines) > m.bodyHeight() {
		// The bottom of a long chapter is the end of its last section, even
		// though that section's anchor may still be below the scrolling limit.
		top = len(m.content.Lines)
	}
	best, bestLine := -1, -1
	firstInChapter := -1
	for i, entry := range m.book.TOC {
		if entry.Chapter != m.chapter {
			continue
		}
		if firstInChapter < 0 {
			firstInChapter = i
		}
		line := 0
		if entry.Fragment != "" {
			anchor, ok := m.content.LineForAnchor(entry.Fragment)
			if !ok {
				continue
			}
			line = anchor
		}
		if line <= top && line >= bestLine {
			best, bestLine = i, line
		}
	}
	if best >= 0 {
		return best
	}
	return firstInChapter
}

// nextSection moves to the next TOC entry, which usually is a subsection
// rather than a whole spine document.
func (m *Model) nextSection() {
	idx := m.tocIndex()
	if idx < 0 {
		m.nextChapter(false)
		return
	}
	for i := idx + 1; i < len(m.book.TOC); i++ {
		if m.book.TOC[i].Chapter >= 0 {
			m.goToTOCEntry(i)
			return
		}
	}
	m.setNotice("已经是最后一节", false)
}

// prevSection moves to the previous TOC entry.
func (m *Model) prevSection() {
	idx := m.tocIndex()
	if idx < 0 {
		m.prevChapter(false)
		return
	}
	for i := idx - 1; i >= 0; i-- {
		if m.book.TOC[i].Chapter >= 0 {
			m.goToTOCEntry(i)
			return
		}
	}
	m.setNotice("已经是第一节", false)
}

func (m *Model) goToTOCEntry(index int) {
	if index < 0 || index >= len(m.book.TOC) {
		return
	}
	entry := m.book.TOC[index]
	if entry.Chapter < 0 || entry.Chapter >= len(m.book.Chapters) {
		m.setNotice("该目录项指向正文之外的内容", true)
		return
	}
	if entry.Chapter == m.chapter {
		m.pushHistory()
		m.renderAt(0, entry.Fragment, false, false)
	} else {
		m.goToChapter(entry.Chapter, entry.Fragment, 0, false, true)
	}
	m.tocCursor = index
	title := entry.Title
	if title == "" {
		title = m.book.Chapters[entry.Chapter].Title
	}
	m.setNotice(fmt.Sprintf("第 %d/%d 章 · %s", entry.Chapter+1, len(m.book.Chapters), title), false)
}

func (m *Model) scrollDown(page bool) {
	if m.content == nil {
		return
	}
	m.tocCursor = -1
	if m.viewport.AtBottom() {
		m.nextChapter(false)
		return
	}
	if page {
		m.viewport.PageDown()
	} else {
		m.viewport.ScrollDown(1)
	}
	m.syncLast()
}

func (m *Model) scrollUp(page bool) {
	if m.content == nil {
		return
	}
	m.tocCursor = -1
	if m.viewport.AtTop() {
		m.prevChapter(true)
		return
	}
	if page {
		m.viewport.PageUp()
	} else {
		m.viewport.ScrollUp(1)
	}
	m.syncLast()
}

func (m *Model) gotoTop() {
	if m.content == nil {
		return
	}
	m.tocCursor = -1
	m.setOffset(0)
	m.syncLast()
}

func (m *Model) gotoBottom() {
	if m.content == nil {
		return
	}
	m.tocCursor = -1
	m.setOffset(len(m.content.Lines))
	m.syncLast()
}

func (m *Model) setNotice(text string, isErr bool) {
	m.notice = text
	m.noticeError = isErr
}

func (m *Model) enterInput() tea.Cmd {
	m.mode = modeInput
	m.modal = modalState{}
	m.cmdHistPos = len(m.cmdHistory)
	m.input.CursorEnd()
	return m.input.Focus()
}

func (m *Model) leaveInput() {
	m.mode = modeBrowse
	m.input.SetValue("")
	m.input.Blur()
}

func (m *Model) submitInput() tea.Cmd {
	raw := strings.TrimSpace(m.input.Value())
	m.leaveInput()
	if raw == "" {
		return nil
	}
	m.cmdHistory = append(m.cmdHistory, raw)
	m.cmdHistPos = len(m.cmdHistory)
	return m.runAction(ParseCommand(raw))
}

func (m *Model) runAction(a Action) tea.Cmd {
	switch a.Kind {
	case ActionNone:
		return nil
	case ActionHelp:
		m.openModal(modalHelp)
	case ActionAbout:
		m.openModal(modalAbout)
	case ActionTOC:
		m.openModal(modalTOC)
	case ActionFavorites:
		m.openModal(modalFavorites)
	case ActionNextChapter:
		m.nextChapter(false)
	case ActionPrevChapter:
		m.prevChapter(false)
	case ActionNextSection:
		m.nextSection()
	case ActionPrevSection:
		m.prevSection()
	case ActionGotoChapter:
		if a.Chapter < 1 || a.Chapter > len(m.book.Chapters) {
			m.setNotice(fmt.Sprintf("章节号应在 1–%d 之间", len(m.book.Chapters)), true)
			return nil
		}
		m.goToChapter(a.Chapter-1, "", 0, false, true)
	case ActionPageDown:
		m.scrollDown(true)
	case ActionPageUp:
		m.scrollUp(true)
	case ActionTop:
		m.gotoTop()
		m.setNotice("已到章首", false)
	case ActionBottom:
		m.gotoBottom()
		m.setNotice("已到章尾", false)
	case ActionTogglePositionFavorite:
		m.togglePositionFavorite()
	case ActionToggleChapterFavorite:
		m.toggleChapterFavorite()
	case ActionBack:
		m.goBack()
	case ActionOpenImage:
		return m.openImage(a.Image)
	case ActionStatus:
		m.setNotice(fmt.Sprintf("第 %d/%d 章 · 章内 %.0f%% · 收藏 %d 条",
			m.chapter+1, len(m.book.Chapters), m.currentPercent()*100, len(m.bookState.Favorites)), false)
	case ActionQuit:
		return tea.Quit
	case ActionUnknown:
		m.setNotice("没理解这条指令 · 输入 help 查看可用命令", true)
	}
	return nil
}

func (m *Model) handleCommandHistory(up bool) {
	if len(m.cmdHistory) == 0 {
		return
	}
	if up {
		if m.cmdHistPos > 0 {
			m.cmdHistPos--
		}
	} else {
		if m.cmdHistPos < len(m.cmdHistory)-1 {
			m.cmdHistPos++
		} else {
			m.cmdHistPos = len(m.cmdHistory)
			m.input.SetValue("")
			return
		}
	}
	if m.cmdHistPos >= 0 && m.cmdHistPos < len(m.cmdHistory) {
		m.input.SetValue(m.cmdHistory[m.cmdHistPos])
		m.input.CursorEnd()
	}
}

// Finalize persists the final reading position. It is called after the
// Bubble Tea program exits.
func (m *Model) Finalize() error {
	if m.store == nil {
		return nil
	}
	return m.saveLast()
}
