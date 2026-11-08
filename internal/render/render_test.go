package render_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	bookmodel "cli-reader/internal/book"
	"cli-reader/internal/epub"
	"cli-reader/internal/render"
	"cli-reader/internal/testepub"
)

type mapImages map[string][]byte

func (m mapImages) ImageBytes(path string) ([]byte, string, error) {
	data, ok := m[path]
	if !ok {
		return nil, "", image.ErrFormat
	}
	return data, "image/png", nil
}

func TestFullPageImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 6), G: uint8(y * 3), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	ch := &bookmodel.Chapter{
		Index:    0,
		Href:     "page-1.png",
		Title:    "第 1 页",
		PageMode: true,
		Blocks: []bookmodel.Block{{
			Kind:  bookmodel.BlockImage,
			Image: &bookmodel.ImageRef{Path: "page-1.png"},
		}},
	}
	r := render.NewRenderer(mapImages{"page-1.png": buf.Bytes()})
	rc := r.RenderChapter(ch, 80, render.Options{
		FullPage:     true,
		MaxImageCols: 78,
		MaxImageRows: 16,
	})
	plain := ansi.Strip(strings.Join(rc.Lines, "\n"))
	if strings.Contains(plain, "✻ Claude") || strings.Contains(plain, "正在阅读") {
		t.Fatalf("full page kept decorative header:\n%s", plain)
	}
	if len(rc.Images) != 1 {
		t.Fatalf("images = %d", len(rc.Images))
	}
	if len(rc.Lines) > 16 {
		t.Fatalf("full page used %d lines, want <= 16", len(rc.Lines))
	}
	for i, line := range rc.Lines {
		if w := ansi.StringWidth(line); w > 80 {
			t.Fatalf("line %d width %d > 80", i, w)
		}
	}
}

func sampleBook(t *testing.T) *bookmodel.Book {
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

func TestEmphasisStylesPreserved(t *testing.T) {
	book := sampleBook(t)
	r := render.NewRenderer(book)

	rc := r.RenderChapter(&book.Chapters[0], 80, render.Options{})
	joined := strings.Join(rc.Lines, "\n")
	for _, want := range []string{
		"\x1b[1m粗体\x1b[0m",
		"\x1b[3m斜体\x1b[0m",
		"\x1b[1m样式粗体\x1b[0m",
		"\x1b[3m样式斜体\x1b[0m",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing emphasis %q in rendered chapter", want)
		}
	}

	// Table cells must keep inline styles instead of being flattened to text.
	rc = r.RenderChapter(&book.Chapters[2], 80, render.Options{})
	found := false
	for _, line := range rc.Lines {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, "目录") || !strings.Contains(plain, "│") {
			continue
		}
		found = true
		if !strings.Contains(line, "\x1b[1m") {
			t.Fatalf("table cell lost bold style: %q", line)
		}
		if !strings.Contains(line, "\x1b[3m") {
			t.Fatalf("table cell lost italic style: %q", line)
		}
	}
	if !found {
		t.Fatal("table row not found in rendered chapter")
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

func tableChapter(caption []bookmodel.Span, rows [][]bookmodel.TableCell) *bookmodel.Chapter {
	return &bookmodel.Chapter{
		Index: 0,
		Blocks: []bookmodel.Block{{
			Kind:         bookmodel.BlockTable,
			Table:        rows,
			TableCaption: caption,
		}},
	}
}

// borderColumns returns the display columns of vertical border glyphs.
func borderColumns(line string) []int {
	var cols []int
	col := 0
	for _, r := range ansi.Strip(line) {
		if r == '│' {
			cols = append(cols, col)
		}
		col += runewidth.RuneWidth(r)
	}
	return cols
}

func TestTableRendersAlignedGrid(t *testing.T) {
	ch := tableChapter(nil, [][]bookmodel.TableCell{
		{
			{Spans: []bookmodel.Span{{Text: "功能"}}, Header: true},
			{Spans: []bookmodel.Span{{Text: "入口"}}, Header: true},
		},
		{
			{Spans: []bookmodel.Span{{Text: "目录", Bold: true}}},
			{Spans: []bookmodel.Span{{Text: "t", Italic: true}}},
		},
		{
			{Spans: []bookmodel.Span{{Text: "收藏"}}},
			{Spans: []bookmodel.Span{{Text: "f / s"}}},
		},
	})
	rc := render.NewRenderer(nil).RenderChapter(ch, 60, render.Options{})

	var grid []string
	for _, line := range rc.Lines {
		plain := ansi.Strip(line)
		if strings.ContainsAny(plain, "┌┬┐├┼┤└┴┘") || strings.HasPrefix(plain, "  │") {
			grid = append(grid, line)
		}
	}
	if len(grid) < 7 {
		t.Fatalf("expected a full grid, got %d lines:\n%s", len(grid), strings.Join(grid, "\n"))
	}
	wantWidth := ansi.StringWidth(ansi.Strip(grid[0]))
	var wantCols []int
	for _, line := range grid {
		if got := ansi.StringWidth(ansi.Strip(line)); got != wantWidth {
			t.Fatalf("grid line width %d != %d: %q", got, wantWidth, ansi.Strip(line))
		}
		cols := borderColumns(line)
		if len(cols) == 0 {
			continue
		}
		if wantCols == nil {
			wantCols = cols
			continue
		}
		if !slices.Equal(cols, wantCols) {
			t.Fatalf("columns are not aligned: %v != %v in %q", cols, wantCols, ansi.Strip(line))
		}
	}
	if len(wantCols) != 3 {
		t.Fatalf("2-column table should draw 3 vertical borders, got %v", wantCols)
	}

	joined := strings.Join(rc.Lines, "\n")
	if !strings.Contains(joined, "\x1b[1;38;2;217;119;87m功能\x1b[0m") {
		t.Fatalf("header cell is not styled as a bold accent: %q", joined)
	}
	if !strings.Contains(joined, "\x1b[1m目录\x1b[0m") || !strings.Contains(joined, "\x1b[3mt\x1b[0m") {
		t.Fatalf("cell inline styles were lost: %q", joined)
	}
}

func TestTableMergesColspanAndRowspan(t *testing.T) {
	ch := tableChapter([]bookmodel.Span{{Text: "合并单元格"}}, [][]bookmodel.TableCell{
		{
			{Spans: []bookmodel.Span{{Text: "A"}}, Header: true},
			{Spans: []bookmodel.Span{{Text: "B"}}, Header: true},
			{Spans: []bookmodel.Span{{Text: "C"}}, Header: true},
		},
		{{Spans: []bookmodel.Span{{Text: "横跨三列的说明"}}, ColSpan: 3, Align: "center"}},
		{
			{Spans: []bookmodel.Span{{Text: "x"}}},
			{Spans: []bookmodel.Span{{Text: "y"}}, RowSpan: 2},
			{Spans: []bookmodel.Span{{Text: "z"}}},
		},
		{
			{Spans: []bookmodel.Span{{Text: "1"}}},
			{Spans: []bookmodel.Span{{Text: "2"}}},
		},
	})
	rc := render.NewRenderer(nil).RenderChapter(ch, 60, render.Options{})

	var spanning string
	var continuation string
	for _, line := range rc.Lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "横跨三列的说明") {
			spanning = plain
		}
		if strings.HasPrefix(plain, "  │ 1") {
			continuation = plain
		}
	}
	if spanning == "" {
		t.Fatal("colspan cell was not rendered")
	}
	if got := len(borderColumns(spanning)); got != 2 {
		t.Fatalf("colspan row should be split by only its outer borders, got %d: %q", got, spanning)
	}
	if continuation == "" {
		t.Fatal("content after a rowspan was not rendered")
	}
	if got := len(borderColumns(continuation)); got != 4 {
		t.Fatalf("rowspan continuation row should still draw every column border, got %d: %q", got, continuation)
	}
	if !strings.Contains(continuation, "2") {
		t.Fatalf("cell after a rowspan continuation was misplaced: %q", continuation)
	}
}

func TestTableFallsBackWhenColumnsCannotFit(t *testing.T) {
	header := make([]bookmodel.TableCell, 6)
	data := make([]bookmodel.TableCell, 6)
	for i := 0; i < 6; i++ {
		header[i] = bookmodel.TableCell{Spans: []bookmodel.Span{{Text: []string{"一", "二", "三", "四", "五", "六"}[i]}}, Header: true}
		data[i] = bookmodel.TableCell{Spans: []bookmodel.Span{{Text: []string{"a", "b", "c", "d", "e", "f"}[i]}}}
	}
	ch := tableChapter(nil, [][]bookmodel.TableCell{header, data})
	rc := render.NewRenderer(nil).RenderChapter(ch, 20, render.Options{})
	joined := strings.Join(rc.Lines, "\n")
	if strings.Contains(joined, "┌") {
		t.Fatalf("6 columns cannot fit in 20 cells and should fall back:\n%s", joined)
	}
	if !strings.Contains(ansi.Strip(joined), "一: ") {
		t.Fatalf("stacked fallback should keep header labels:\n%s", ansi.Strip(joined))
	}
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
