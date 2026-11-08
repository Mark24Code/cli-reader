package tui

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cli-reader/internal/book"
	"cli-reader/internal/formats"
)

func pickerFixture() pickerModel {
	dir := filepath.Join("books")
	entries := []formats.Entry{
		{Path: filepath.Join(dir, "alpha.txt"), Name: "alpha.txt", Format: book.FormatTXT},
		{Path: filepath.Join(dir, "beta.pdf"), Name: "beta.pdf", Format: book.FormatPDF},
	}
	return pickerModel{dir: dir, all: entries, filtered: entries}
}

func TestPickerFilterAndSelect(t *testing.T) {
	p := pickerFixture()
	next, _ := p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	p = next.(pickerModel)

	next, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bet")})
	p = next.(pickerModel)
	if len(p.filtered) != 1 || p.filtered[0].Name != "beta.pdf" {
		t.Fatalf("filtered = %#v", p.filtered)
	}

	next, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	p = next.(pickerModel)
	if cmd == nil || p.selected != filepath.Join("books", "beta.pdf") {
		t.Fatalf("selected = %q cmd=%v", p.selected, cmd)
	}
}

func TestPickerBackspaceAndClear(t *testing.T) {
	p := pickerFixture()
	p.width, p.height = 80, 24
	next, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zz")})
	p = next.(pickerModel)
	if len(p.filtered) != 0 {
		t.Fatalf("filtered = %#v", p.filtered)
	}
	next, _ = p.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	p = next.(pickerModel)
	next, _ = p.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	p = next.(pickerModel)
	if len(p.filtered) != 2 {
		t.Fatalf("filtered after clear = %#v", p.filtered)
	}
}

func TestPickerViewFitsWidth(t *testing.T) {
	p := pickerFixture()
	next, _ := p.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	p = next.(pickerModel)
	view := p.View()
	if lines := len(splitLines(view)); lines != 20 {
		t.Fatalf("view lines = %d", lines)
	}
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
