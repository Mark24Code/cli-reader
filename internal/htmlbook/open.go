// Package htmlbook loads a standalone HTML/XHTML document.
package htmlbook

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"cli-reader/internal/book"
	"cli-reader/internal/htmlimport"
	"cli-reader/internal/textdecode"
)

const maxFileBytes = 64 << 20

// Open reads an HTML/XHTML file into the shared book model.
func Open(name string) (*book.Book, error) {
	data, truncated, err := readLimited(name, maxFileBytes)
	if err != nil {
		return nil, err
	}
	var warnings []string
	if truncated {
		warnings = append(warnings, fmt.Sprintf("文件超过 %d MiB，已截断读取", maxFileBytes>>20))
	}
	if needsTextDecode(data) {
		decoded := textdecode.Decode(data)
		data = []byte(decoded.Text)
		if decoded.Warning != "" {
			warnings = append(warnings, decoded.Warning)
		}
		if decoded.Encoding != "" && !strings.HasPrefix(decoded.Encoding, "UTF-8") {
			warnings = append(warnings, "检测到文本编码: "+decoded.Encoding)
		}
	}

	href := filepath.Base(name)
	parse := func() (htmlimport.Result, error) {
		return htmlimport.Parse(data, htmlimport.Options{
			Href:                   href,
			GenerateHeadingAnchors: true,
			ResolveImage:           book.ResolveLocalImage,
		})
	}
	res, err := parse()
	if err != nil {
		return nil, fmt.Errorf("解析 HTML 失败: %w", err)
	}
	if len(res.Chapter.Blocks) == 0 {
		return nil, fmt.Errorf("HTML 中没有可阅读的正文")
	}

	title := res.DocTitle
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

	chapters := []book.Chapter{*res.Chapter}
	toc := book.TOCEntriesFromHeadings(0, res.Headings)
	splitLevel := 0
	chunked := false
	if len(data) > 2<<20 || len(res.Chapter.Blocks) > 400 {
		if level := book.ChooseHeadingLevel(res.Headings); level > 0 {
			if split, splitTOC := book.SplitByHeadings(res.Chapter, res.Headings, level, href); len(split) > 1 {
				chapters = split
				toc = splitTOC
				splitLevel = level
			}
		}
		if splitLevel == 0 {
			if chunks := book.ChunkChapter(res.Chapter, 250, href); len(chunks) > 1 {
				chapters = chunks
				toc = nil
				chunked = true
			}
		}
	}
	lazy := splitLevel > 0 || chunked
	if lazy {
		for i := range chapters {
			chapters[i].Blocks = nil
			chapters[i].Anchors = nil
		}
	}

	b := &book.Book{
		Path:     name,
		Title:    title,
		Author:   res.Author,
		Language: res.Lang,
		Format:   book.FormatHTML,
		Unit:     "节",
		Chapters: chapters,
	}
	b.Warnings = append(b.Warnings, warnings...)
	b.SetImageProvider(book.NewDirImages(filepath.Dir(name)))
	b.TOC = toc
	b.EnsureTOC()
	if lazy {
		b.SetChapterLoader(func(i int) error {
			r, err := parse()
			if err != nil {
				return err
			}
			var part *book.Chapter
			if splitLevel > 0 {
				part = book.SplitPartByHeadings(r.Chapter, r.Headings, splitLevel, i, href)
			} else {
				part = book.ChunkChapterPart(r.Chapter, 250, i, href)
			}
			if part == nil {
				return fmt.Errorf("章节索引无效")
			}
			target := &b.Chapters[i]
			target.Blocks = part.Blocks
			target.Anchors = part.Anchors
			target.Heading = part.Heading
			target.DocTitle = part.DocTitle
			return nil
		})
	}
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

// needsTextDecode reports whether the document has no declared charset and is
// not valid UTF-8, in which case statistical detection is better than the
// HTML default (Windows-1252).
func needsTextDecode(data []byte) bool {
	if utf8.Valid(data) {
		return false
	}
	head := data
	if len(head) > 8192 {
		head = head[:8192]
	}
	lower := bytes.ToLower(head)
	// Trust an explicit <meta charset>; otherwise statistically detect the
	// encoding rather than letting the HTML default (Windows-1252) win.
	if bytes.Contains(lower, []byte("<meta")) &&
		(bytes.Contains(lower, []byte("charset=")) || bytes.Contains(lower, []byte("encoding="))) {
		return false
	}
	return true
}
