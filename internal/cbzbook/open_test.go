package cbzbook

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func buildCBZ(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "comic.cbz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOpenOrderAndMetadata(t *testing.T) {
	p := buildCBZ(t, map[string]string{
		"page10.png":          "\x89PNG\r\n\x1a\n10",
		"page2.png":           "\x89PNG\r\n\x1a\n2",
		"page1.jpg":           "\xff\xd8\xff1",
		"notes.txt":           "ignore",
		"__MACOSX/page99.png": "\x89PNG\r\n\x1a\n",
		"ComicInfo.xml": `<ComicInfo>
			<Title>测试漫画</Title>
			<Series>系列</Series>
			<Number>3</Number>
			<Writer>作者丙</Writer>
			<LanguageISO>zh</LanguageISO>
			<PageCount>3</PageCount>
		</ComicInfo>`,
	})
	b, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.Title != "测试漫画" || b.Author != "作者丙" || b.Language != "zh" {
		t.Fatalf("metadata = %q / %q / %q", b.Title, b.Author, b.Language)
	}
	if !b.PageMode || b.Unit != "页" {
		t.Fatalf("page mode = %v unit = %q", b.PageMode, b.Unit)
	}
	if b.Chapters[0].Title != "第 1 页" {
		t.Fatalf("first title = %q", b.Chapters[0].Title)
	}
	if len(b.Chapters) != 3 {
		t.Fatalf("pages = %d", len(b.Chapters))
	}
	got := []string{b.Chapters[0].Href, b.Chapters[1].Href, b.Chapters[2].Href}
	want := []string{"page1.jpg", "page2.png", "page10.png"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if !b.Chapters[0].PageMode || len(b.Chapters[0].Blocks) != 1 || b.Chapters[0].Blocks[0].Image == nil {
		t.Fatalf("page chapter shape: %#v", b.Chapters[0])
	}
	data, mt, err := b.ImageBytes("page2.png")
	if err != nil || len(data) == 0 || mt != "image/png" {
		t.Fatalf("image bytes: %d %q %v", len(data), mt, err)
	}
	if len(b.TOC) != 3 {
		t.Fatalf("toc = %d", len(b.TOC))
	}
}

func TestOpenNoImages(t *testing.T) {
	p := buildCBZ(t, map[string]string{"readme.txt": "hello"})
	if _, err := Open(p); err == nil {
		t.Fatal("expected error for image-free zip")
	}
}

func TestNaturalLess(t *testing.T) {
	if !naturalLess("page2.png", "page10.png") {
		t.Fatal("page2 should sort before page10")
	}
	if !naturalLess("ch1/page1.png", "ch1/page2.png") {
		t.Fatal("directory ordering")
	}
}
