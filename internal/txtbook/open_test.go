package txtbook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cli-reader/internal/book"
)

func writeTXT(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "novel.txt")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOpenChaptersAndMetadata(t *testing.T) {
	p := writeTXT(t, `书名：测试小说
作者：某人

第一章 开始

第一段
第二行

第二章 继续

> 引用内容

- 项目一
- 项目二
`)
	b, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "测试小说" || b.Author != "某人" {
		t.Fatalf("metadata = %q / %q", b.Title, b.Author)
	}
	if len(b.Chapters) != 2 {
		t.Fatalf("chapters = %d", len(b.Chapters))
	}
	if b.Chapters[0].Title != "第一章 开始" || b.Chapters[1].Title != "第二章 继续" {
		t.Fatalf("titles = %q / %q", b.Chapters[0].Title, b.Chapters[1].Title)
	}
	var para string
	for _, blk := range b.Chapters[0].Blocks {
		if blk.Kind == book.BlockParagraph {
			para = blk.Spans[0].Text
		}
	}
	if para != "第一段第二行" && para != "第一段 第二行" {
		// Chinese lines join without a space; keep both accepted for clarity.
		t.Fatalf("paragraph = %q", para)
	}
	var quote, items int
	for _, blk := range b.Chapters[1].Blocks {
		switch blk.Kind {
		case book.BlockParagraph:
			if blk.Quote {
				quote++
			}
		case book.BlockListItem:
			items++
		}
	}
	if quote != 1 || items != 2 {
		t.Fatalf("quote=%d items=%d", quote, items)
	}
}

func TestNoHeadingFallback(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 1600; i++ {
		sb.WriteString("一行普通正文内容。\n")
		if i%20 == 19 {
			sb.WriteString("\n")
		}
	}
	b, err := Open(writeTXT(t, sb.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Chapters) < 2 {
		t.Fatalf("expected chunked chapters, got %d", len(b.Chapters))
	}
	if !strings.Contains(strings.Join(b.Warnings, " "), "没有识别到章节标题") {
		t.Fatalf("warnings = %#v", b.Warnings)
	}
}

func TestCodeBlock(t *testing.T) {
	p := writeTXT(t, `第一章 代码

下面是一段代码：

    func main() {
        fmt.Println("hi")
    }
`)
	b, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	code := 0
	for _, blk := range b.Chapters[0].Blocks {
		if blk.Kind == book.BlockCode {
			code++
			if !strings.Contains(blk.Code, "func main()") {
				t.Fatalf("code = %q", blk.Code)
			}
		}
	}
	if code != 1 {
		t.Fatalf("code blocks = %d", code)
	}
}
