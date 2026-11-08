// Package mdbook loads Markdown documents through goldmark and the shared
// HTML importer.
package mdbook

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"

	"cli-reader/internal/book"
	"cli-reader/internal/htmlimport"
	"cli-reader/internal/textdecode"
)

const maxFileBytes = 32 << 20

// Open reads a Markdown file into the shared book model.
func Open(name string) (*book.Book, error) {
	raw, truncated, err := readLimited(name, maxFileBytes)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	var warnings []string
	if truncated {
		warnings = append(warnings, fmt.Sprintf("文件超过 %d MiB，已截断读取", maxFileBytes>>20))
	}
	if !utf8.Valid(raw) {
		decoded := textdecode.Decode(raw)
		text = decoded.Text
		if decoded.Warning != "" {
			warnings = append(warnings, decoded.Warning)
		}
		if decoded.Encoding != "" && !strings.HasPrefix(decoded.Encoding, "UTF-8") {
			warnings = append(warnings, "检测到文本编码: "+decoded.Encoding)
		}
	}

	meta, body := splitFrontMatter(text)
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Footnote),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(html.WithUnsafe(), html.WithXHTML()),
	)
	var rendered bytes.Buffer
	if err := md.Convert([]byte(body), &rendered); err != nil {
		return nil, fmt.Errorf("解析 Markdown 失败: %w", err)
	}

	res, err := htmlimport.Parse(rendered.Bytes(), htmlimport.Options{
		Href:                   filepath.Base(name),
		GenerateHeadingAnchors: true,
		ResolveImage:           book.ResolveLocalImage,
	})
	if err != nil {
		return nil, fmt.Errorf("解析 Markdown 失败: %w", err)
	}
	if len(res.Chapter.Blocks) == 0 {
		return nil, fmt.Errorf("Markdown 中没有可阅读的正文")
	}

	title := strings.TrimSpace(meta["title"])
	if title == "" {
		for _, h := range res.Headings {
			if h.Level == 1 && h.Title != "" {
				title = h.Title
				break
			}
		}
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	}
	author := strings.TrimSpace(meta["author"])
	if author == "" {
		author = res.Author
	}

	b := &book.Book{
		Path:     name,
		Title:    title,
		Author:   author,
		Language: strings.TrimSpace(meta["language"]),
		Format:   book.FormatMarkdown,
		Unit:     "节",
		Chapters: []book.Chapter{*res.Chapter},
	}
	b.Warnings = append(b.Warnings, warnings...)
	b.SetImageProvider(book.NewDirImages(filepath.Dir(name)))
	b.TOC = book.TOCEntriesFromHeadings(0, res.Headings)
	b.EnsureTOC()
	return b, nil
}

func readLimited(name string, limit int64) ([]byte, bool, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

func splitFrontMatter(text string) (map[string]string, string) {
	meta := make(map[string]string)
	text = strings.TrimPrefix(text, "\ufeff")
	if !strings.HasPrefix(text, "---\n") {
		return meta, text
	}
	rest := text[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return meta, text
	}
	header := rest[:end]
	for _, line := range strings.Split(header, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch key {
		case "title", "标题":
			meta["title"] = value
		case "author", "作者":
			meta["author"] = value
		case "language", "lang", "语言":
			meta["language"] = value
		}
	}
	body := rest[end+4:]
	body = strings.TrimPrefix(body, "\n")
	return meta, body
}
