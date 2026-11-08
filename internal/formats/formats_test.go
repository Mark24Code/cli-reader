package formats

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"cli-reader/internal/book"
	"cli-reader/internal/testepub"
)

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseFormat(t *testing.T) {
	cases := map[string]book.Format{
		"auto":     "",
		"":         "",
		"epub":     book.FormatEPUB,
		"md":       book.FormatMarkdown,
		"markdown": book.FormatMarkdown,
		"azw3":     book.FormatMOBI,
		"cbz":      book.FormatCBZ,
	}
	for in, want := range cases {
		got, ok := ParseFormat(in)
		if !ok || got != want {
			t.Errorf("ParseFormat(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := ParseFormat("docx"); ok {
		t.Fatal("docx should be rejected")
	}
}

func TestDetectByExtension(t *testing.T) {
	cases := map[string]book.Format{
		"a.txt":      book.FormatTXT,
		"a.markdown": book.FormatMarkdown,
		"a.html":     book.FormatHTML,
		"a.pdf":      book.FormatPDF,
	}
	for name, want := range cases {
		p := writeTemp(t, name, []byte("hello"))
		got, err := Detect(p)
		if err != nil || got != want {
			t.Errorf("Detect(%s) = %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestDetectMagic(t *testing.T) {
	pdf := writeTemp(t, "book.bin", []byte("%PDF-1.7\n"))
	if got, err := Detect(pdf); err != nil || got != book.FormatPDF {
		t.Fatalf("pdf magic: %q %v", got, err)
	}

	pdb := make([]byte, 80)
	copy(pdb[60:64], "BOOK")
	copy(pdb[64:68], "MOBI")
	p := writeTemp(t, "book.bin", pdb)
	if got, err := Detect(p); err != nil || got != book.FormatMOBI {
		t.Fatalf("mobi magic: %q %v", got, err)
	}
}

func TestDetectZip(t *testing.T) {
	data, err := testepub.BuildSample(testepub.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	p := writeTemp(t, "book.epub", data)
	if got, err := Detect(p); err != nil || got != book.FormatEPUB {
		t.Fatalf("epub zip: %q %v", got, err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("001.png")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("\x89PNG\r\n\x1a\n"))
	zw.Close()
	p = writeTemp(t, "book.cbz", buf.Bytes())
	if got, err := Detect(p); err != nil || got != book.FormatCBZ {
		t.Fatalf("cbz zip: %q %v", got, err)
	}
}

func TestDetectUnsupported(t *testing.T) {
	p := writeTemp(t, "book.bin", []byte{0, 1, 2, 3, 4, 5, 0, 0})
	if _, err := Detect(p); err == nil {
		t.Fatal("binary input should be unsupported")
	}
}

func TestOpenTextFormats(t *testing.T) {
	dir := t.TempDir()
	txt := filepath.Join(dir, "novel.txt")
	if err := os.WriteFile(txt, []byte("第一章 开始\n\n正文。\n\n第二章 继续\n\n更多正文。\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# 标题\n\n正文。\n\n## 小节\n\n更多。\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	html := filepath.Join(dir, "doc.html")
	if err := os.WriteFile(html, []byte("<html><head><title>网页</title></head><body><h1>标题</h1><p>正文。</p></body></html>"), 0o600); err != nil {
		t.Fatal(err)
	}

	epubData, err := testepub.BuildSample(testepub.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	epubPath := writeTemp(t, "demo.epub", epubData)

	var cbz bytes.Buffer
	zw := zip.NewWriter(&cbz)
	w, err := zw.Create("001.png")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("\x89PNG\r\n\x1a\n"))
	zw.Close()
	cbzPath := writeTemp(t, "demo.cbz", cbz.Bytes())

	overridePath := writeTemp(t, "book.dat", []byte("第一章 测试\n\n正文。\n"))

	cases := []struct {
		path   string
		format book.Format
	}{
		{epubPath, book.FormatEPUB},
		{txt, book.FormatTXT},
		{md, book.FormatMarkdown},
		{html, book.FormatHTML},
		{cbzPath, book.FormatCBZ},
	}
	for _, tc := range cases {
		b, err := Open(tc.path, "")
		if err != nil {
			t.Fatalf("Open(%s): %v", filepath.Base(tc.path), err)
		}
		if b.Format != tc.format {
			t.Errorf("%s format = %q, want %q", filepath.Base(tc.path), b.Format, tc.format)
		}
		if len(b.Chapters) == 0 || len(b.TOC) == 0 {
			t.Errorf("%s has no chapters/TOC", filepath.Base(tc.path))
		}
		_ = b.Close()
	}

	b, err := Open(overridePath, book.FormatTXT)
	if err != nil {
		t.Fatalf("explicit format override: %v", err)
	}
	if b.Format != book.FormatTXT {
		t.Fatalf("override format = %q", b.Format)
	}
	_ = b.Close()
}
