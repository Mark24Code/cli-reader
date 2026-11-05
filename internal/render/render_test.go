package render_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"claude-epub-reader/internal/epub"
	"claude-epub-reader/internal/render"
	"claude-epub-reader/internal/testepub"
)

func sampleBook(t *testing.T) *epub.Book {
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
	return book
}

func TestChapterFitsWidth(t *testing.T) {
	book := sampleBook(t)
	r := render.NewRenderer(book)
	for _, width := range []int{56, 72, 80, 120} {
		for i := range book.Chapters {
			rc := r.RenderChapter(&book.Chapters[i], width, render.Options{})
			for n, line := range rc.Lines {
				if got := ansi.StringWidth(line); got > width {
					t.Fatalf("width=%d chapter=%d line=%d: got %d cells: %q", width, i, n, got, ansi.Strip(line))
				}
			}
			if len(rc.Lines) == 0 {
				t.Fatalf("empty chapter %d", i)
			}
		}
	}
}

func TestRenderImageAndAnchorTracking(t *testing.T) {
	book := sampleBook(t)
	r := render.NewRenderer(book)
	rc := r.RenderChapter(&book.Chapters[1], 80, render.Options{})
	if len(rc.Images) != 1 {
		t.Fatalf("images = %d", len(rc.Images))
	}
	img := rc.Images[0]
	if img.StartLine < 0 || img.EndLine <= img.StartLine {
		t.Fatalf("bad image span %#v", img)
	}
	if rc.VisibleImage(img.StartLine, 10) == nil {
		t.Fatal("image should be visible")
	}
	if rc.VisibleImage(img.EndLine+2, 5) != nil {
		t.Fatal("image should not be visible")
	}
	if line, ok := rc.LineForAnchor("syntax"); !ok || line <= 0 {
		t.Fatalf("anchor missing: %d %v", line, ok)
	}
}

func TestLinkSpansTrackCoordinates(t *testing.T) {
	book := sampleBook(t)
	r := render.NewRenderer(book)
	rc := r.RenderChapter(&book.Chapters[1], 80, render.Options{})
	want := map[string]bool{"ch1.xhtml#why": false, "#syntax": false}
	for _, link := range rc.Links {
		if _, ok := want[link.Href]; ok {
			want[link.Href] = true
		}
		if link.Line < 0 || link.Line >= len(rc.Lines) {
			t.Fatalf("link line out of range: %#v", link)
		}
		lineW := ansi.StringWidth(ansi.Strip(rc.Lines[link.Line]))
		if link.StartCol < 0 || link.EndCol > lineW || link.EndCol <= link.StartCol {
			t.Fatalf("bad link columns %#v (line width %d)", link, lineW)
		}
		href, ok := rc.LinkAt(link.Line, link.StartCol)
		if !ok || href != link.Href {
			t.Fatalf("LinkAt(%d,%d) = %q,%v want %q", link.Line, link.StartCol, href, ok, link.Href)
		}
		if _, ok := rc.LinkAt(link.Line, link.EndCol); ok {
			t.Fatalf("LinkAt past EndCol should miss: %#v", link)
		}
	}
	for href, found := range want {
		if !found {
			t.Fatalf("missing link span for %q", href)
		}
	}
}

func TestScrollPercent(t *testing.T) {
	if p := render.ScrollPercent(10, 20, 0); p != 0 {
		t.Fatalf("short chapter percent = %v", p)
	}
	if p := render.ScrollPercent(100, 20, 40); p != 0.5 {
		t.Fatalf("percent = %v", p)
	}
	if off := render.OffsetForPercent(100, 20, 0.5); off != 40 {
		t.Fatalf("offset = %d", off)
	}
}

func TestCodeBlockHighlighted(t *testing.T) {
	book := sampleBook(t)
	r := render.NewRenderer(book)
	rc := r.RenderChapter(&book.Chapters[1], 80, render.Options{})
	found := false
	for _, line := range rc.Lines {
		plain := ansi.Strip(line)
		if len(plain) > 0 && (contains(plain, "╭─ go") || contains(plain, "func main")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("code block missing from render")
	}
}

func contains(s, sub string) bool {
	return bytes.Contains([]byte(s), []byte(sub))
}

func TestCodeBlockUsesTruecolorStyles(t *testing.T) {
	book := sampleBook(t)
	r := render.NewRenderer(book)
	rc := r.RenderChapter(&book.Chapters[1], 80, render.Options{})
	colorSeqs := 0
	for _, line := range rc.Lines {
		colorSeqs += strings.Count(line, "\x1b[38;2;")
	}
	if colorSeqs < 10 {
		t.Fatalf("expected multiple truecolor syntax runs, got %d", colorSeqs)
	}
}
