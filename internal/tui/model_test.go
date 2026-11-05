package tui

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"claude-epub-reader/internal/epub"
	"claude-epub-reader/internal/imgterm"
	"claude-epub-reader/internal/state"
	"claude-epub-reader/internal/testepub"
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
