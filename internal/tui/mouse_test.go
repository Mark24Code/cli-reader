package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"cli-reader/internal/render"
)

func sendMouse(t *testing.T, m Model, button tea.MouseButton, x, y int) Model {
	t.Helper()
	return sendMouseAction(t, m, tea.MouseActionPress, button, x, y)
}

func sendMouseAction(t *testing.T, m Model, action tea.MouseAction, button tea.MouseButton, x, y int) Model {
	t.Helper()
	next, _ := m.Update(tea.MouseMsg{
		Action: action,
		Button: button,
		X:      x,
		Y:      y,
	})
	return next.(Model)
}

func sendClick(t *testing.T, m Model, x, y int) Model {
	t.Helper()
	m = sendMouseAction(t, m, tea.MouseActionPress, tea.MouseButtonLeft, x, y)
	return sendMouseAction(t, m, tea.MouseActionRelease, tea.MouseButtonLeft, x, y)
}

func TestMouseWheelScrollsContent(t *testing.T) {
	m := testModel(t)
	m.goToChapter(1, "", 0, false, false)
	before := m.viewport.YOffset
	m = sendMouse(t, m, tea.MouseButtonWheelDown, 10, 10)
	if m.viewport.YOffset != before+wheelScrollLines {
		t.Fatalf("wheel down offset = %d, want %d", m.viewport.YOffset, before+wheelScrollLines)
	}
	m = sendMouse(t, m, tea.MouseButtonWheelUp, 10, 10)
	if m.viewport.YOffset != before {
		t.Fatalf("wheel up offset = %d, want %d", m.viewport.YOffset, before)
	}
}

func TestMouseClickFollowsContentLink(t *testing.T) {
	m := testModel(t)
	m.goToChapter(1, "", 0, false, false)
	var link render.LinkSpan
	found := false
	for _, l := range m.content.Links {
		if l.Href == "ch1.xhtml#why" {
			link = l
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("content link missing: %#v", m.content.Links)
	}
	offset := max(0, link.Line-1)
	m.setOffset(offset)
	offset = m.viewport.YOffset
	y := link.Line - offset + 2
	m = sendClick(t, m, link.StartCol, y)
	if m.chapter != 0 {
		t.Fatalf("click did not follow link: chapter = %d", m.chapter)
	}
	if m.viewport.YOffset == 0 {
		t.Fatal("link to fragment should land below the chapter start")
	}
}

func TestMouseClickOutsideContentLinkDoesNothing(t *testing.T) {
	m := testModel(t)
	m.goToChapter(1, "", 0, false, false)
	chapter := m.chapter
	var link render.LinkSpan
	for _, l := range m.content.Links {
		link = l
		break
	}
	offset := max(0, link.Line-1)
	m.setOffset(offset)
	offset = m.viewport.YOffset
	y := link.Line - offset + 2
	m = sendClick(t, m, 0, y)
	if m.chapter != chapter {
		t.Fatalf("click at column 0 changed chapter to %d", m.chapter)
	}
}

func TestModalGeometryMatchesView(t *testing.T) {
	m := testModel(t)
	m = sendKey(t, m, keyRunes("t"))
	box := m.modalBoxRect()
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if box.bodyTop+2 >= len(lines) {
		t.Fatalf("body row outside view: bodyTop=%d lines=%d", box.bodyTop, len(lines))
	}
	if row := lines[box.bodyTop+2]; !strings.Contains(row, "第二章") {
		t.Fatalf("body row 2 = %q, want 第二章", row)
	}
	if col := ansi.StringWidth(lines[box.bodyTop]); col <= box.bodyLeft {
		t.Fatalf("body row too narrow: %d, bodyLeft=%d", col, box.bodyLeft)
	}
}

func TestMouseClickTOCRowJumps(t *testing.T) {
	m := testModel(t)
	m = sendKey(t, m, keyRunes("t"))
	if m.modal.kind != modalTOC {
		t.Fatal("t did not open TOC")
	}
	box := m.modalBoxRect()
	// Row 2 is the third TOC entry: 第二章.
	m = sendClick(t, m, box.bodyLeft+2, box.bodyTop+2)
	if m.modal.kind != modalNone {
		t.Fatalf("TOC modal still open: %v", m.modal.kind)
	}
	if m.chapter != 1 {
		t.Fatalf("clicked TOC row did not jump: chapter = %d", m.chapter)
	}
}

func TestSelectedTextAndHighlight(t *testing.T) {
	view := "hello world\nsecond line"
	single := selectionState{
		active: true, moved: true,
		anchor: selPoint{X: 0, Y: 0},
		cursor: selPoint{X: 4, Y: 0},
	}
	if got, want := selectedText(view, single), "hello"; got != want {
		t.Fatalf("single-line selection = %q, want %q", got, want)
	}

	multi := selectionState{
		active: true, moved: true,
		anchor: selPoint{X: 6, Y: 0},
		cursor: selPoint{X: 3, Y: 1},
	}
	if got, want := selectedText(view, multi), "world\nseco"; got != want {
		t.Fatalf("multi-line selection = %q, want %q", got, want)
	}

	highlighted := highlightSelection(view, multi)
	if !strings.Contains(highlighted, "\x1b[7m") {
		t.Fatal("selection is not highlighted")
	}
	src := strings.Split(view, "\n")
	for i, line := range strings.Split(highlighted, "\n") {
		if got, want := ansi.StringWidth(line), ansi.StringWidth(src[i]); got != want {
			t.Fatalf("highlight changed line %d width: %d != %d", i, got, want)
		}
	}
}

func TestMouseDragSelectsInsteadOfClickingLink(t *testing.T) {
	m := testModel(t)
	m.goToChapter(1, "", 0, false, false)
	var link render.LinkSpan
	for _, l := range m.content.Links {
		if l.Href == "ch1.xhtml#why" {
			link = l
			break
		}
	}
	offset := max(0, link.Line-1)
	m.setOffset(offset)
	offset = m.viewport.YOffset
	y := link.Line - offset + 2

	m = sendMouseAction(t, m, tea.MouseActionPress, tea.MouseButtonLeft, link.StartCol, y)
	m = sendMouseAction(t, m, tea.MouseActionMotion, tea.MouseButtonLeft, link.StartCol+6, y)
	if !m.selection.active || !m.selection.moved {
		t.Fatalf("drag did not create a selection: %#v", m.selection)
	}
	if !strings.Contains(m.View(), "\x1b[7m") {
		t.Fatal("dragging did not highlight the selection")
	}
	if got := selectedText(m.renderView(), m.selection); strings.TrimSpace(got) == "" {
		t.Fatal("selected text is empty")
	}

	next, cmd := m.Update(tea.MouseMsg{
		Action: tea.MouseActionRelease,
		Button: tea.MouseButtonLeft,
		X:      link.StartCol + 6,
		Y:      y,
	})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("release did not schedule a clipboard copy")
	}
	if m.selection.active {
		t.Fatal("selection still active after release")
	}
	if m.chapter != 1 {
		t.Fatalf("dragging followed the link: chapter = %d", m.chapter)
	}
}

func TestMouseClickOutsideModalClosesIt(t *testing.T) {
	m := testModel(t)
	m = sendKey(t, m, keyRunes("t"))
	m = sendClick(t, m, 0, 0)
	if m.modal.kind != modalNone {
		t.Fatalf("outside click did not close modal: %v", m.modal.kind)
	}
}
