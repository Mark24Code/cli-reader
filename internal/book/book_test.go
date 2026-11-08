package book

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLazyChapterLRU(t *testing.T) {
	b := &Book{Chapters: make([]Chapter, 5)}
	for i := range b.Chapters {
		b.Chapters[i].Href = fmt.Sprintf("ch%d", i)
	}
	loads := 0
	b.SetChapterLoader(func(i int) error {
		loads++
		b.Chapters[i].Blocks = []Block{{Kind: BlockParagraph, Spans: []Span{{Text: "x"}}}}
		return nil
	})
	for i := 0; i < 5; i++ {
		if ch := b.EnsureChapter(i); ch == nil {
			t.Fatalf("EnsureChapter(%d) = nil", i)
		}
	}
	if loads != 5 {
		t.Fatalf("loads = %d", loads)
	}
	for i := 0; i < 2; i++ {
		if b.Chapters[i].Blocks != nil {
			t.Fatalf("chapter %d should have been evicted", i)
		}
	}
	for i := 2; i < 5; i++ {
		if b.Chapters[i].Blocks == nil {
			t.Fatalf("chapter %d should be loaded", i)
		}
	}

	b.EnsureChapter(0)
	if loads != 6 {
		t.Fatalf("loads after reload = %d", loads)
	}
	if b.Chapters[0].Blocks == nil {
		t.Fatal("chapter 0 did not reload")
	}
	if b.Chapters[2].Blocks != nil {
		t.Fatal("oldest chapter should be evicted again")
	}
}

func TestJoinLines(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"hello", "world", "hello world"},
		{"中文", "继续", "中文继续"},
		{"hello", "中文", "hello中文"},
		{"exam-", "ple", "example"},
		{"word-", "Word", "word- Word"},
		{"", "only", "only"},
	}
	for _, tc := range cases {
		if got := JoinLines(tc.a, tc.b); got != tc.want {
			t.Errorf("JoinLines(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestGenericResolveLink(t *testing.T) {
	b := &Book{Chapters: []Chapter{
		{Href: "ch1.xhtml"},
		{Href: "sub/ch2.xhtml"},
	}}
	cases := []struct {
		base, raw string
		chapter   int
		fragment  string
		external  bool
		missing   bool
	}{
		{"ch1.xhtml", "sub/ch2.xhtml#x", 1, "x", false, false},
		{"sub/ch2.xhtml", "#top", 1, "top", false, false},
		{"ch1.xhtml", "https://example.com", -1, "", true, false},
		{"ch1.xhtml", "../escape.gone", -1, "", false, true},
		{"ch1.xhtml", "missing.xhtml", -1, "", false, true},
	}
	for _, tc := range cases {
		got := b.ResolveLink(tc.base, tc.raw)
		if got.Chapter != tc.chapter || got.Fragment != tc.fragment ||
			got.External != tc.external || got.Missing != tc.missing {
			t.Errorf("ResolveLink(%q, %q) = %#v", tc.base, tc.raw, got)
		}
	}
}

func TestDirImagesContainment(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "img"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "img", "a.png"), []byte("\x89PNG\r\n\x1a\nxxxx"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside.png")
	if err := os.WriteFile(outside, []byte("\x89PNG\r\n\x1a\nxxxx"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })

	d := NewDirImages(root)
	data, mt, err := d.ImageBytes("img/a.png")
	if err != nil || len(data) == 0 || mt != "image/png" {
		t.Fatalf("valid image: data=%d mt=%q err=%v", len(data), mt, err)
	}
	if _, _, err := d.ImageBytes("../outside.png"); err == nil {
		t.Fatal("path traversal should be rejected")
	}
	if _, _, err := d.ImageBytes("/etc/passwd"); err == nil {
		t.Fatal("absolute path should be rejected")
	}
}

func TestUnitLabel(t *testing.T) {
	b := &Book{}
	if got := b.UnitLabel("章"); got != "章" {
		t.Fatalf("default = %q", got)
	}
	b.Unit = "页"
	if got := b.UnitLabel("章"); got != "页" {
		t.Fatalf("configured = %q", got)
	}
}
