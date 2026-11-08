package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"cli-reader/internal/book"
	"cli-reader/internal/imgterm"
	"cli-reader/internal/pdfbook"
	"cli-reader/internal/render"
	"cli-reader/internal/state"
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
	book      *book.Book
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

	pdfMode          string
	pdfTextBook      *book.Book
	pdfImageBook     *book.Book
	requestImageMode bool

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

func New(bk *book.Book, store *state.Store, key string, protocol imgterm.Protocol) Model {
	m := Model{
		book:       bk,
		renderer:   render.NewRenderer(bk),
		store:      store,
		bookKey:    key,
		protocol:   protocol,
		viewport:   viewport.New(0, 0),
		input:      textinput.New(),
		cmdHistPos: -1,
		tocCursor:  -1,
	}
	if bk.Format == book.FormatPDF {
		m.pdfMode = bk.Mode
		if m.pdfMode == "" {
			if bk.PageMode {
				m.pdfMode = book.ModeImage
			} else {
				m.pdfMode = book.ModeText
			}
		}
		if m.pdfMode == book.ModeImage {
			m.pdfImageBook = bk
		} else {
			m.pdfTextBook = bk
		}
	}
	m.bookState = store.Book(key, bk.Path, bk.Title, bk.Author)
	if idx := bk.ChapterByHref(m.bookState.Last.Href); idx >= 0 {
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
	} else if bk.Format == book.FormatPDF {
		mode := "图片"
		if m.pdfMode == book.ModeText {
			mode = "文本"
		}
		notice := fmt.Sprintf("PDF %s模式 · 按 m 切换文本/图片", mode)
		for _, w := range bk.Warnings {
			if strings.Contains(w, "图片模式不可用") {
				notice = "PDF 文本模式（图片模式不可用，按 m 重试）"
				break
			}
		}
		if m.hasInitial && m.initialPercent > 0.005 {
			notice = "已恢复阅读位置 · " + notice
		}
		m.notice = notice
	} else if m.hasInitial && m.initialPercent > 0.005 {
		m.notice = "已恢复上次阅读位置"
	} else if len(bk.Warnings) > 0 {
		m.notice = fmt.Sprintf("已打开，但有 %d 条解析提示", len(bk.Warnings))
	} else {
		m.notice = "按 / 输入命令 · ? 查看帮助 · Enter 打开可见图片"
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

// WantsImageMode reports that the TUI should exit so main can start the
// full-screen native PDF image viewer.
func (m Model) WantsImageMode() bool { return m.requestImageMode }

func (m *Model) requestPDFImage() tea.Cmd {
	if m.book == nil || m.book.Format != book.FormatPDF {
		m.setNotice("只有 PDF 支持文本/图片模式切换", true)
		return nil
	}
	if m.pdfMode == book.ModeImage {
		return nil
	}
	if m.protocol == imgterm.ProtocolNone {
		m.setNotice("当前终端不支持原生图片协议，无法使用 PDF 图片模式；请使用 iTerm2 / Kitty / WezTerm", true)
		return nil
	}
	m.requestImageMode = true
	m.setNotice("正在切换到 PDF 图片模式...", false)
	return tea.Quit
}

func (m *Model) togglePDFMode() tea.Cmd {
	if m.book == nil || m.book.Format != book.FormatPDF {
		m.setNotice("只有 PDF 支持文本/图片模式切换", true)
		return nil
	}
	if m.pdfMode == book.ModeImage {
		m.setPDFMode(book.ModeText)
		return nil
	}
	return m.requestPDFImage()
}

func (m *Model) setPDFMode(target string) {
	if m.book == nil || m.book.Format != book.FormatPDF {
		m.setNotice("只有 PDF 支持文本/图片模式切换", true)
		return
	}
	if target != book.ModeText && target != book.ModeImage {
		return
	}
	if target == m.pdfMode {
		return
	}
	if target == book.ModeImage && m.protocol == imgterm.ProtocolNone {
		m.setNotice("当前终端不支持原生图片协议，无法使用 PDF 图片模式；请使用 iTerm2 / Kitty / WezTerm", true)
		return
	}

	var next *book.Book
	switch target {
	case book.ModeImage:
		if m.pdfImageBook != nil {
			next = m.pdfImageBook
		} else {
			b, err := pdfbook.OpenImage(m.book.Path)
			if err != nil {
				if errors.Is(err, pdfbook.ErrImageModeUnsupported) {
					m.setNotice("图片模式不可用：未找到 sips / pdftoppm / mutool / gs / ImageMagick，请安装后重试", true)
				} else {
					m.setNotice("切换到图片模式失败: "+err.Error(), true)
				}
				return
			}
			m.pdfImageBook = b
			next = b
		}
	case book.ModeText:
		if m.pdfTextBook != nil {
			next = m.pdfTextBook
		} else {
			b, err := pdfbook.OpenText(m.book.Path)
			if err != nil {
				m.setNotice("切换到文本模式失败: "+err.Error(), true)
				return
			}
			m.pdfTextBook = b
			next = b
		}
	default:
		return
	}

	if m.pdfMode == book.ModeImage {
		m.pdfImageBook = m.book
	} else {
		m.pdfTextBook = m.book
	}
	m.book = next
	m.pdfMode = target
	m.book.Mode = target
	m.renderer = render.NewRenderer(next)
	if m.store != nil {
		m.bookState = m.store.Book(m.bookKey, next.Path, next.Title, next.Author)
	}
	if m.chapter >= len(next.Chapters) {
		m.chapter = len(next.Chapters) - 1
	}
	if m.chapter < 0 {
		m.chapter = 0
	}
	m.content = nil
	m.hasRendered = false
	m.hasInitial = false
	m.initialHref = ""
	m.initialPercent = 0
	m.tocCursor = -1
	m.renderAt(0, "", false, false)
	m.hasRendered = true
	label := "文本"
	if target == book.ModeImage {
		label = "图片"
	}
	m.setNotice("已切换到 PDF "+label+"模式 · 按 m 切换", false)
}

func (m Model) bodyHeight() int {
	h := m.height - 6
	if h < 1 {
		h = 1
	}
	return h
}

// unit returns the reading-unit label: 章 for EPUB, 节 for text, 页 for
// PDF/CBZ.
func (m Model) unit() string {
	if m.book == nil {
		return "章"
	}
	return m.book.UnitLabel("章")
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
	opts := render.Options{}
	if m.book.PageMode || ch.PageMode {
		opts.FullPage = true
		opts.MaxImageCols = max(8, m.width-2)
		if m.book.Format == book.FormatPDF && m.book.Mode == book.ModeImage {
			// Use the full terminal width so the rasterized page is as precise
			// as a character thumbnail can be; the page scrolls vertically.
			opts.MaxImageRows = 4096
		} else {
			opts.MaxImageRows = max(2, m.bodyHeight()-2)
		}
	} else {
		opts.MaxImageRows = min(14, max(2, m.bodyHeight()-2))
	}
	m.content = m.renderer.RenderChapter(ch, m.width, opts)
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
	m.bookState.Last = book.Position{Href: m.book.Chapters[m.chapter].Href, Percent: m.currentPercent()}
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
		m.setNotice("内容不存在", true)
		return
	}
	if push {
		m.pushHistory()
	}
	m.chapter = index
	m.tocCursor = -1
	m.renderAt(percent, fragment, atBottom, false)
	ch := m.book.Chapters[index]
	m.setNotice(fmt.Sprintf("第 %d/%d %s · %s", index+1, len(m.book.Chapters), m.unit(), ch.Title), false)
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
		m.setNotice("历史位置对应的阅读单元已不存在", true)
		return
	}
	m.chapter = idx
	m.tocCursor = -1
	m.renderAt(loc.Percent, "", false, false)
	m.setNotice(fmt.Sprintf("已返回 · 第 %d %s", idx+1, m.unit()), false)
}

func (m *Model) nextChapter(atBottom bool) {
	if m.chapter >= len(m.book.Chapters)-1 {
		m.setNotice(fmt.Sprintf("已经是最后一%s", m.unit()), false)
		return
	}
	m.goToChapter(m.chapter+1, "", 0, atBottom, true)
}

func (m *Model) prevChapter(atBottom bool) {
	if m.chapter <= 0 {
		m.setNotice(fmt.Sprintf("已经是第一%s", m.unit()), false)
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
	m.setNotice(fmt.Sprintf("第 %d/%d %s · %s", entry.Chapter+1, len(m.book.Chapters), m.unit(), title), false)
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
			m.setNotice(fmt.Sprintf("编号应在 1–%d 之间", len(m.book.Chapters)), true)
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
	case ActionPDFTextMode:
		m.setPDFMode(book.ModeText)
	case ActionPDFImageMode:
		return m.requestPDFImage()
	case ActionStatus:
		m.setNotice(fmt.Sprintf("第 %d/%d %s · 单元内 %.0f%% · 收藏 %d 条",
			m.chapter+1, len(m.book.Chapters), m.unit(), m.currentPercent()*100, len(m.bookState.Favorites)), false)
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
