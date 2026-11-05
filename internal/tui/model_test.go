package tui

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"cli-epub-reader/internal/epub"
	"cli-epub-reader/internal/imgterm"
	"cli-epub-reader/internal/state"
	"cli-epub-reader/internal/testepub"
)

func testModel(t *testing.T) Model {
	t.Helper()
	data, err := testepub.BuildSample(testepub.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	book, err := epub.OpenReaderAt(bytes.NewReader(data), int64(len(data)), "demo.epub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = book.Close() })
	store := state.Load(filepath.Join(t.TempDir(), "state.json"))
	m := New(book, store, state.BookKey("demo.epub"), imgterm.ProtocolNone)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return next.(Model)
}

func sendKey(t *testing.T, m Model, k tea.KeyMsg) Model {
	t.Helper()
	next, _ := m.Update(k)
	return next.(Model)
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func assertViewWidth(t *testing.T, m Model) {
	t.Helper()
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Fatalf("view lines = %d, height = %d", len(lines), m.height)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > m.width {
			t.Fatalf("line %d width %d > %d: %q", i, w, m.width, ansi.Strip(line))
		}
	}
}

func TestModelLayoutAndResize(t *testing.T) {
	m := testModel(t)
	assertViewWidth(t, m)
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "cli-epub-reader") {
		t.Fatalf("header missing project name:\n%s", view)
	}
	if !strings.Contains(view, "终端里的 Claude") {
		t.Fatalf("header missing book title:\n%s", view)
	}

	m = sendKey(t, m, keyRunes("t"))
	if m.modal.kind != modalTOC {
		t.Fatal("t did not open TOC")
	}
	assertViewWidth(t, m)
	before := m.chapter
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.modal.kind != modalNone {
		t.Fatal("Enter did not close TOC")
	}
	if len(m.history) == 0 {
		t.Fatal("jump did not push history")
	}
	m = sendKey(t, m, keyRunes("b"))
	if m.chapter != before {
		t.Fatalf("back did not restore chapter: got %d want %d", m.chapter, before)
	}

	m = sendKey(t, m, keyRunes("/"))
	if m.mode != modeInput {
		t.Fatal("/ did not focus prompt")
	}
	for _, r := range "help" {
		m = sendKey(t, m, keyRunes(string(r)))
	}
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.modal.kind != modalHelp {
		t.Fatalf("help command did not open modal, got %v", m.modal.kind)
	}
	assertViewWidth(t, m)
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	m = sendKey(t, m, keyRunes("f"))
	if len(m.bookState.Favorites) != 1 {
		t.Fatalf("favorites = %d", len(m.bookState.Favorites))
	}
	if err := m.Finalize(); err != nil {
		t.Fatal(err)
	}
	reloaded := state.Load(m.store.Path)
	bs := reloaded.Book(m.bookKey, m.book.Path, m.book.Title, m.book.Author)
	if len(bs.Favorites) != 1 {
		t.Fatalf("persisted favorites = %d", len(bs.Favorites))
	}

	next, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	m = next.(Model)
	if !m.tooSmall {
		t.Fatal("expected tooSmall")
	}
	if !strings.Contains(ansi.Strip(m.View()), "拉宽") {
		t.Fatal("missing resize hint")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 90, Height: 28})
	m = next.(Model)
	if m.tooSmall {
		t.Fatal("expected recovery from tooSmall")
	}
	assertViewWidth(t, m)
}

func TestAboutModal(t *testing.T) {
	m := testModel(t)
	m = sendKey(t, m, keyRunes("a"))
	if m.modal.kind != modalAbout {
		t.Fatalf("a did not open about, got %v", m.modal.kind)
	}
	assertViewWidth(t, m)
	view := ansi.Strip(m.View())
	for _, want := range []string{
		"关于", "cli-epub-reader", "Mark24Code", "mark.zhangyoung@gmail.com",
		"github.com/Mark24Code/cli-epub-reader",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("about view missing %q:\n%s", want, view)
		}
	}
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.modal.kind != modalNone {
		t.Fatal("Esc did not close about")
	}

	m = sendKey(t, m, keyRunes("/"))
	for _, r := range "关于" {
		m = sendKey(t, m, keyRunes(string(r)))
	}
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.modal.kind != modalAbout {
		t.Fatalf("关于 command did not open about, got %v", m.modal.kind)
	}
}

func TestCommandHelpAndTOC(t *testing.T) {
	m := testModel(t)
	m = sendKey(t, m, keyRunes("/"))
	for _, r := range "跳到第三章" {
		m = sendKey(t, m, keyRunes(string(r)))
	}
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.chapter != 2 {
		t.Fatalf("chapter = %d, want 2", m.chapter)
	}
}

func TestLayoutWidthsAndModals(t *testing.T) {
	for _, size := range []struct{ w, h int }{{56, 16}, {60, 20}, {100, 32}, {140, 50}} {
		m := testModel(t)
		next, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		m = next.(Model)
		assertViewWidth(t, m)
		m = sendKey(t, m, keyRunes("t"))
		assertViewWidth(t, m)
		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
		m = sendKey(t, m, keyRunes("?"))
		assertViewWidth(t, m)
	}
}

func TestArrowKeysNavigateAdjacentSections(t *testing.T) {
	m := testModel(t)
	if m.chapter != 0 {
		t.Fatalf("initial chapter = %d", m.chapter)
	}

	// → must first move to the subsection inside the current chapter, not
	// skip straight to the next spine document.
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.chapter != 0 {
		t.Fatalf("→ should stay in chapter 1 for its subsection, got chapter %d", m.chapter)
	}
	if !strings.Contains(m.notice, "为什么在终端读书") {
		t.Fatalf("→ did not land on the first subsection: %q", m.notice)
	}
	anchor, ok := m.content.LineForAnchor("why")
	if !ok {
		t.Fatal("missing anchor for the first subsection")
	}
	want := anchor
	if maxOff := len(m.content.Lines) - m.bodyHeight(); want > maxOff {
		want = maxOff
	}
	if want < 0 {
		want = 0
	}
	if m.viewport.YOffset != want {
		t.Fatalf("→ offset = %d, want %d (anchor line %d)", m.viewport.YOffset, want, anchor)
	}

	// The next section belongs to chapter 2.
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if m.chapter != 1 {
		t.Fatalf("→ from the last section should reach chapter 2, got %d", m.chapter)
	}

	// ← walks back to the same subsection.
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.chapter != 0 || !strings.Contains(m.notice, "为什么在终端读书") {
		t.Fatalf("← did not return to the subsection: chapter=%d notice=%q", m.chapter, m.notice)
	}

	// ← again returns to the chapter's opening section...
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.chapter != 0 || m.viewport.YOffset != 0 {
		t.Fatalf("← should reach the chapter start: chapter=%d offset=%d", m.chapter, m.viewport.YOffset)
	}
	// ...and then stops at the very first section.
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if !strings.Contains(m.notice, "已经是第一节") {
		t.Fatalf("← at the first section should stop: %q", m.notice)
	}

	// n/p still switch whole chapters.
	m = sendKey(t, m, keyRunes("n"))
	if m.chapter != 1 {
		t.Fatalf("n should switch to the next chapter, got %d", m.chapter)
	}
	m = sendKey(t, m, keyRunes("p"))
	if m.chapter != 0 {
		t.Fatalf("p should switch to the previous chapter, got %d", m.chapter)
	}
}

func TestScrollAcrossChapterBoundaries(t *testing.T) {
	m := testModel(t)
	m.gotoBottom()
	before := m.chapter
	m.scrollDown(false)
	if m.chapter != before+1 {
		t.Fatalf("down at bottom did not advance chapter: %d -> %d", before, m.chapter)
	}
	if m.viewport.YOffset != 0 {
		t.Fatalf("next chapter should start at top, offset=%d", m.viewport.YOffset)
	}
	m.gotoTop()
	m.scrollUp(false)
	if m.chapter != before {
		t.Fatalf("up at top did not go back chapter: %d", m.chapter)
	}
	if m.viewport.AtTop() {
		t.Fatal("previous chapter should be positioned at bottom")
	}
}

func TestResizePreservesApproximatePosition(t *testing.T) {
	m := testModel(t)
	m = sendKey(t, m, keyRunes("/"))
	for _, r := range "goto 2" {
		m = sendKey(t, m, keyRunes(string(r)))
	}
	m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	for i := 0; i < 6; i++ {
		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	before := m.currentPercent()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	m = next.(Model)
	after := m.currentPercent()
	if diff := after - before; diff > 0.03 || diff < -0.03 {
		t.Fatalf("resize changed position: before %.3f after %.3f", before, after)
	}
	if before == 0 {
		t.Fatal("expected non-zero scroll position")
	}
}
