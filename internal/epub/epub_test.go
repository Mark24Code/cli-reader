package epub_test

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	bookmodel "cli-reader/internal/book"
	"cli-reader/internal/epub"
	"cli-reader/internal/testepub"
)

func openSample(t *testing.T, opts testepub.Options) *bookmodel.Book {
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
		case bookmodel.BlockCode:
			codeBlocks++
			if block.Lang != "go" && block.Lang != "python" {
				t.Errorf("unexpected code language %q", block.Lang)
			}
			if strings.Contains(block.Code, "func main") && !strings.Contains(block.Code, "fmt.Println") {
				t.Errorf("code text missing")
			}
		case bookmodel.BlockImage:
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
	got := bookmodel.SanitizeText("a\x1b[31mb\x00c\u009bd\n\tkeep")
	if strings.ContainsAny(got, "\x1b\x00\u009b") {
		t.Fatalf("control characters survived: %q", got)
	}
	if !strings.Contains(got, "keep") {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
}

func addZipEntry(t *testing.T, data []byte, name string, content []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		w, err := zw.Create(f.Name)
		if err != nil {
			rc.Close()
			t.Fatal(err)
		}
		if _, err := io.Copy(w, rc); err != nil {
			rc.Close()
			t.Fatal(err)
		}
		rc.Close()
	}
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFontObfuscationIsAllowed(t *testing.T) {
	data, err := testepub.BuildSample(testepub.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	enc := []byte(`<?xml version="1.0"?>
<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <EncryptedData>
    <EncryptionMethod Algorithm="http://www.idpf.org/2008/embedding"/>
    <CipherData><CipherReference URI="OEBPS/font.otf"/></CipherData>
  </EncryptedData>
</encryption>`)
	data = addZipEntry(t, data, "META-INF/encryption.xml", enc)
	b, err := epub.OpenReaderAt(bytes.NewReader(data), int64(len(data)), "demo.epub")
	if err != nil {
		t.Fatalf("font-only obfuscation should open: %v", err)
	}
	if len(b.Warnings) == 0 || !strings.Contains(strings.Join(b.Warnings, " "), "字体混淆") {
		t.Fatalf("missing obfuscation warning: %#v", b.Warnings)
	}
}

func TestEncryptedContentIsDRM(t *testing.T) {
	data, err := testepub.BuildSample(testepub.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	enc := []byte(`<?xml version="1.0"?>
<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <EncryptedData>
    <EncryptionMethod Algorithm="http://example.com/adept"/>
    <CipherData><CipherReference URI="OEBPS/ch1.xhtml"/></CipherData>
  </EncryptedData>
</encryption>`)
	data = addZipEntry(t, data, "META-INF/encryption.xml", enc)
	if _, err := epub.OpenReaderAt(bytes.NewReader(data), int64(len(data)), "demo.epub"); !bookmodel.IsDRM(err) {
		t.Fatalf("expected DRM error, got %v", err)
	}
}

func TestRightsXMLIsDRM(t *testing.T) {
	data, err := testepub.BuildSample(testepub.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	data = addZipEntry(t, data, "META-INF/rights.xml", []byte(`<rights/>`))
	if _, err := epub.OpenReaderAt(bytes.NewReader(data), int64(len(data)), "demo.epub"); !bookmodel.IsDRM(err) {
		t.Fatalf("expected DRM error, got %v", err)
	}
}
