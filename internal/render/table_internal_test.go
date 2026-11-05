package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"cli-epub-reader/internal/epub"
)

func TestTableRenderInvariants(t *testing.T) {
	long := strings.Repeat("unbreakable-", 30)
	rows := [][]epub.TableCell{
		{
			{Spans: []epub.Span{{Text: "列一"}}, Header: true},
			{Spans: []epub.Span{{Text: "列二"}}, Header: true},
			{Spans: []epub.Span{{Text: "列三"}}, Header: true},
		},
		{
			{Spans: []epub.Span{{Text: long}}},
			{Spans: []epub.Span{{Text: "并排两列", Link: "ch1.xhtml#x"}}, ColSpan: 2, Align: "center"},
		},
		{
			{Spans: []epub.Span{{Text: "跨行"}}, RowSpan: 2},
			{Spans: []epub.Span{{Text: "乙"}}},
			{Spans: []epub.Span{{Text: "丙"}}},
		},
		{
			{Spans: []epub.Span{{Text: "丁"}}},
			{Spans: []epub.Span{{Text: "戊"}}},
		},
		{
			// Malformed/ragged row: missing cells must still be padded out.
			{Spans: []epub.Span{{Text: "只有一列"}}},
		},
	}
	r := NewRenderer(nil)
	for width := 20; width <= 140; width++ {
		rb := r.renderTable(&epub.Block{Kind: epub.BlockTable, Table: rows}, width)
		grid := len(rb.Lines) > 0 && strings.HasPrefix(ansi.Strip(rb.Lines[0]), "  ┌")
		want := -1
		foundLink := false
		for i, line := range rb.Lines {
			w := ansi.StringWidth(line)
			if w > width {
				t.Fatalf("width=%d line %d is %d cells: %q", width, i, w, ansi.Strip(line))
			}
			if grid {
				if want < 0 {
					want = w
				} else if w != want {
					t.Fatalf("width=%d line %d is %d cells, want %d: %q", width, i, w, want, ansi.Strip(line))
				}
			}
			for _, link := range rb.Links[i] {
				foundLink = true
				if link.start < 0 || link.end > w || link.end <= link.start {
					t.Fatalf("width=%d line %d bad link %#v", width, i, link)
				}
				if !strings.Contains(ansi.Strip(line), "并排两列") {
					continue
				}
				if link.href != "ch1.xhtml#x" {
					t.Fatalf("width=%d line %d link href = %q", width, i, link.href)
				}
			}
		}
		if width == 60 && !foundLink {
			t.Fatalf("link inside a table cell was not tracked at width %d", width)
		}
	}
}
