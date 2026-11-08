package mdbook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cli-reader/internal/book"
)

func TestOpenMarkdown(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.png"), []byte("\x89PNG\r\n\x1a\nxx"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := `---
title: Markdown 测试
author: 作者乙
language: zh-CN
---

# 第一章

正文 **加粗** 与 ` + "`行内代码`" + `。

## 小节

` + "```go" + `
package main
` + "```" + `

| 列一 | 列二 |
| --- | ---: |
| A | B |

![本地图](a.png)

![远程图](https://example.com/x.png)
`
	p := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Markdown 测试" || b.Author != "作者乙" || b.Language != "zh-CN" {
		t.Fatalf("metadata = %#v", b)
	}
	if len(b.TOC) < 2 {
		t.Fatalf("toc = %#v", b.TOC)
	}
	var code, table int
	var refs []book.ImageRef
	for _, blk := range b.Chapters[0].Blocks {
		switch blk.Kind {
		case book.BlockCode:
			code++
			if blk.Lang != "go" {
				t.Fatalf("code lang = %q", blk.Lang)
			}
		case book.BlockTable:
			table++
		case book.BlockImage:
			if blk.Image != nil {
				refs = append(refs, *blk.Image)
			}
		}
	}
	if code != 1 || table != 1 {
		t.Fatalf("code=%d table=%d", code, table)
	}
	if len(refs) != 2 || refs[0].Path != "a.png" || !refs[1].External {
		t.Fatalf("images = %#v", refs)
	}
}

func TestLargeMarkdownSplitsAndLoadsLazily(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 3; i++ {
		fmt.Fprintf(&sb, "# Part %d\n\n", i)
		for j := 0; j < 200; j++ {
			sb.WriteString("paragraph text\n\n")
		}
	}
	p := filepath.Join(t.TempDir(), "large.md")
	if err := os.WriteFile(p, []byte(sb.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Chapters) < 2 {
		t.Fatalf("large Markdown was not split: %d", len(b.Chapters))
	}
	if len(b.TOC) < 2 {
		t.Fatalf("TOC = %#v", b.TOC)
	}
	if b.Chapters[0].Blocks != nil {
		t.Fatal("large Markdown should load chapters lazily")
	}
	for i := range b.Chapters {
		if len(b.EnsureChapter(i).Blocks) == 0 {
			t.Fatalf("chapter %d has no blocks", i)
		}
	}
}

func TestChineseAfterLongASCIIPrefix(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("# Title\n\n")
	for sb.Len() < 1500 {
		sb.WriteString("This is a long English paragraph used to push Chinese text past the charset prescan window. ")
	}
	sb.WriteString("\n\n中文正文测试。\n")
	p := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(p, []byte(sb.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, blk := range b.Chapters[0].Blocks {
		for _, sp := range blk.Spans {
			text.WriteString(sp.Text)
		}
	}
	if !strings.Contains(text.String(), "中文正文测试") {
		t.Fatalf("Chinese text was mangled: %q", text.String()[max(0, text.Len()-80):])
	}
}
