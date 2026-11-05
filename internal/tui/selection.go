package tui

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	osc52 "github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// selPoint is a cell position on screen. Both fields are zero-based.
type selPoint struct {
	X, Y int
}

// selectionState tracks a mouse drag over the rendered screen.
type selectionState struct {
	active bool
	moved  bool
	anchor selPoint
	cursor selPoint
}

// normalize orders the anchor and cursor top-left first.
func (s selectionState) normalize() (selPoint, selPoint) {
	a, b := s.anchor, s.cursor
	if a.Y > b.Y || (a.Y == b.Y && a.X > b.X) {
		a, b = b, a
	}
	return a, b
}

// selectedText extracts the plain text covered by a selection from the
// rendered view. Selection is inclusive of both endpoints.
func selectedText(view string, sel selectionState) string {
	lines := strings.Split(view, "\n")
	a, b := sel.normalize()
	var out []string
	for y := a.Y; y <= b.Y; y++ {
		if y < 0 || y >= len(lines) {
			continue
		}
		width := ansi.StringWidth(lines[y])
		start, end := 0, width
		if y == a.Y {
			start = a.X
		}
		if y == b.Y {
			end = b.X + 1
		}
		start = clampInt(start, 0, width)
		end = clampInt(end, 0, width)
		if start >= end {
			continue
		}
		plain := ansi.Strip(lines[y])
		out = append(out, strings.TrimRight(ansi.Cut(plain, start, end), " "))
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// highlightSelection reverses the video of the selected cells while keeping
// the rest of the screen untouched.
func highlightSelection(view string, sel selectionState) string {
	lines := strings.Split(view, "\n")
	a, b := sel.normalize()
	for y := a.Y; y <= b.Y; y++ {
		if y < 0 || y >= len(lines) {
			continue
		}
		width := ansi.StringWidth(lines[y])
		start, end := 0, width
		if y == a.Y {
			start = a.X
		}
		if y == b.Y {
			end = b.X + 1
		}
		start = clampInt(start, 0, width)
		end = clampInt(end, 0, width)
		if start >= end {
			continue
		}
		prefix := ansi.Cut(lines[y], 0, start)
		body := ansi.Strip(ansi.Cut(lines[y], start, end))
		suffix := ansi.Cut(lines[y], end, width)
		lines[y] = prefix + "\x1b[7m" + body + "\x1b[27m" + suffix
	}
	return strings.Join(lines, "\n")
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// copyTextCmd copies text using the native clipboard, falling back to OSC 52
// for terminals where no clipboard helper is available (for example over SSH).
func copyTextCmd(text string) tea.Cmd {
	chars := utf8.RuneCountInString(text)
	return func() tea.Msg {
		if err := clipboard.WriteAll(text); err == nil {
			return copiedMsg{Chars: chars}
		}
		_, _ = fmt.Fprint(os.Stderr, osc52.New(text))
		return copiedMsg{Chars: chars}
	}
}

type copiedMsg struct {
	Chars int
}
