package htmlbook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cli-reader/internal/book"
)

func TestOpenHTML(t *testing.T) {
	dir := t.TempDir()
	imgDir := filepath.Join(dir, "img")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imgDir, "a.png"), []byte("\x89PNG\r\n\x1a\nxx"), 0o600); err != nil {
		t.Fatal(err)
	}
	html := `<!doctype html><html lang="zh-CN"><head>
<title>测试文档</title>
<meta name="author" content="作者甲">
</head><body>
<h1>第一章</h1>
<p>正文内容。</p>
<h2 id="sub">小节</h2>
<p>更多内容。</p>
<img src="img/a.png" alt="本地图">
<img src="https://example.com/x.png" alt="远程图">
<img src="../outside.png" alt="越界图">
</body></html>`
	p := filepath.Join(dir, "doc.html")
	if err := os.WriteFile(p, []byte(html), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "测试文档" || b.Author != "作者甲" || b.Language != "zh-CN" {
		t.Fatalf("metadata = %#v", b)
	}
	if len(b.Chapters) != 1 {
		t.Fatalf("chapters = %d", len(b.Chapters))
	}
	if len(b.TOC) < 2 {
		t.Fatalf("toc = %#v", b.TOC)
	}
	var refs []book.ImageRef
	for _, blk := range b.Chapters[0].Blocks {
		if blk.Image != nil {
			refs = append(refs, *blk.Image)
		}
	}
	if len(refs) != 3 {
		t.Fatalf("images = %d", len(refs))
	}
	if refs[0].Path != "img/a.png" {
		t.Fatalf("local image path = %q", refs[0].Path)
	}
	if !refs[1].External {
		t.Fatalf("remote image should be external: %#v", refs[1])
	}
	if refs[2].Path != "" {
		t.Fatalf("escaping image should have no path: %#v", refs[2])
	}
}

func TestOpenHTMLNoBody(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty.html")
	if err := os.WriteFile(p, []byte("<html><body><script>x</script></body></html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p); err == nil || !strings.Contains(err.Error(), "没有可阅读") {
		t.Fatalf("expected empty error, got %v", err)
	}
}
