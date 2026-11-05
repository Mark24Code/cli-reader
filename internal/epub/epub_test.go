package epub_test

import (
	"bytes"
	"strings"
	"testing"

	"cli-epub-reader/internal/epub"
	"cli-epub-reader/internal/testepub"
)

func openSample(t *testing.T, opts testepub.Options) *epub.Book {
	t.Helper()
	data, err := testepub.BuildSample(opts)
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

func TestParseMetadataSpineAndTOC(t *testing.T) {
	book := openSample(t, testepub.DefaultOptions())
	if book.Title != "终端里的 Claude" {
		t.Fatalf("title = %q", book.Title)
	}
	if book.Author != "Mark24" {
		t.Fatalf("author = %q", book.Author)
	}
	if len(book.Chapters) != 3 {
		t.Fatalf("chapters = %d", len(book.Chapters))
	}
	if book.Chapters[0].Title != "第一章 开篇" {
		t.Fatalf("chapter title = %q", book.Chapters[0].Title)
	}
	if len(book.TOC) < 5 {
		t.Fatalf("toc entries = %d: %#v", len(book.TOC), book.TOC)
	}
	var found bool
	for _, entry := range book.TOC {
		if strings.Contains(entry.Title, "语法高亮") {
			found = true
			if entry.Chapter != 1 {
				t.Errorf("fragment toc chapter = %d, want 1", entry.Chapter)
			}
			if entry.Fragment != "syntax" {
				t.Errorf("fragment = %q", entry.Fragment)
			}
			if entry.Depth < 1 {
				t.Errorf("expected nested depth, got %d", entry.Depth)
			}
		}
	}
	if !found {
		t.Fatal("nested TOC entry missing")
	}
}

func TestXHTMLBlocksAnchorsAndImages(t *testing.T) {
	book := openSample(t, testepub.DefaultOptions())
	ch1 := book.Chapters[0]
	if _, ok := ch1.Anchors["why"]; !ok {
		t.Fatalf("anchor why missing: %#v", ch1.Anchors)
	}
	if ch1.Heading != "第一章 开篇" {
		t.Fatalf("heading = %q", ch1.Heading)
	}
	ch2 := book.Chapters[1]
	var codeBlocks int
	var imageRefs int
	for _, block := range ch2.Blocks {
		switch block.Kind {
		case epub.BlockCode:
			codeBlocks++
			if block.Lang != "go" && block.Lang != "python" {
				t.Errorf("unexpected code language %q", block.Lang)
			}
			if strings.Contains(block.Code, "func main") && !strings.Contains(block.Code, "fmt.Println") {
				t.Errorf("code text missing")
			}
		case epub.BlockImage:
			imageRefs++
			if block.Image == nil || block.Image.Path == "" {
				t.Errorf("image path not resolved: %#v", block.Image)
			}
		}
	}
	if codeBlocks != 3 {
		t.Fatalf("code blocks = %d, want 3", codeBlocks)
	}
	if imageRefs != 1 {
		t.Fatalf("image refs = %d, want 1", imageRefs)
	}
	data, mediaType, err := book.ImageBytes("OEBPS/images/wide.png")
	if err != nil {
		t.Fatal(err)
	}
	if mediaType != "image/png" || len(data) == 0 {
		t.Fatalf("unexpected image: %s len=%d", mediaType, len(data))
	}
}

func TestNCXOnlyAndFallbackTOC(t *testing.T) {
	book := openSample(t, testepub.Options{IncludeNCX: true})
	if len(book.TOC) < 4 {
		t.Fatalf("ncx toc entries = %d", len(book.TOC))
	}
	if book.TOC[0].Title != "第一章 开篇" {
		t.Fatalf("ncx first title = %q", book.TOC[0].Title)
	}

	book2 := openSample(t, testepub.Options{})
	if len(book2.TOC) != len(book2.Chapters) {
		t.Fatalf("fallback toc = %d, chapters = %d", len(book2.TOC), len(book2.Chapters))
	}
	for i, entry := range book2.TOC {
		if entry.Chapter != i {
			t.Fatalf("fallback entry %d targets chapter %d", i, entry.Chapter)
		}
	}
}

func TestResolveLink(t *testing.T) {
	book := openSample(t, testepub.DefaultOptions())
	base := book.Chapters[1].Href
	cases := []struct {
		raw      string
		chapter  int
		fragment string
		external bool
		missing  bool
	}{
		{raw: "ch1.xhtml#why", chapter: 0, fragment: "why"},
		{raw: "#syntax", chapter: 1, fragment: "syntax"},
		{raw: "https://example.com/book", chapter: -1, external: true},
		{raw: "missing.xhtml", chapter: -1, missing: true},
		{raw: "nav.xhtml", chapter: -1},
	}
	for _, tc := range cases {
		got := book.ResolveLink(base, tc.raw)
		if got.Chapter != tc.chapter || got.Fragment != tc.fragment ||
			got.External != tc.external || got.Missing != tc.missing {
			t.Errorf("ResolveLink(%q) = %#v, want chapter=%d fragment=%q external=%v missing=%v",
				tc.raw, got, tc.chapter, tc.fragment, tc.external, tc.missing)
		}
	}
}

func TestSanitizeText(t *testing.T) {
	got := epub.SanitizeText("a\x1b[31mb\x00c\u009bd\n\tkeep")
	if strings.ContainsAny(got, "\x1b\x00\u009b") {
		t.Fatalf("control characters survived: %q", got)
	}
	if !strings.Contains(got, "keep") {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
}
