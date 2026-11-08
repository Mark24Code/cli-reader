// Package txtbook loads plain-text books, detects their encoding and splits
// them into chapters.
package txtbook

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"cli-reader/internal/book"
	"cli-reader/internal/textdecode"
)

const (
	maxFileBytes = 64 << 20
	maxChunkLine = 1200
	maxChunkByte = 64 << 10
)

var (
	reChapterCN = regexp.MustCompile(`^第\s*[0-9零一二三四五六七八九十百千万两]+\s*[章节回卷篇部](?:\s*[^\n]{0,40})?$`)
	reChapterEN = regexp.MustCompile(`(?i)^(chapter|part|book|volume|prologue|epilogue|preface|afterword|appendix)\b[^\n]{0,60}$`)
	reSpecialCN = regexp.MustCompile(`^(前言|序章|序言|楔子|引子|后记|尾声|终章|目录|简介|作者的话)\s*$`)
	reMarkdown  = regexp.MustCompile(`^(#{1,3})\s+(.+)$`)
	reListItem  = regexp.MustCompile(`^(\s*)([-*•+]|\d{1,3}[.)、])\s+(.*)$`)
	reTitleMeta = regexp.MustCompile(`(?i)^\s*(书名|标题|title)\s*[：:]\s*(.+)$`)
	reAuthMeta  = regexp.MustCompile(`(?i)^\s*(作者|author)\s*[：:]\s*(.+)$`)
)

type section struct {
	title string
	level int
	lines []string
}

// Open reads a TXT file into the shared book model.
func Open(name string) (*book.Book, error) {
	data, truncated, err := readLimited(name, maxFileBytes)
	if err != nil {
		return nil, err
	}
	decoded := textdecode.Decode(data)
	if strings.TrimSpace(decoded.Text) == "" {
		return nil, fmt.Errorf("TXT 文件为空")
	}
	lines := strings.Split(decoded.Text, "\n")
	title, author, skip := parseMetadata(lines)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	}

	b := &book.Book{
		Path:   name,
		Title:  title,
		Author: author,
		Format: book.FormatTXT,
		Unit:   "节",
	}
	if decoded.Warning != "" {
		b.Warnings = append(b.Warnings, decoded.Warning)
	}
	if decoded.Encoding != "" && !strings.HasPrefix(decoded.Encoding, "UTF-8") {
		b.Warnings = append(b.Warnings, "检测到文本编码: "+decoded.Encoding)
	}
	if truncated {
		b.Warnings = append(b.Warnings, fmt.Sprintf("文件超过 %d MiB，已截断读取", maxFileBytes>>20))
	}

	sections, hadHeadings := splitSections(lines, skip)
	if !hadHeadings {
		b.Warnings = append(b.Warnings, "没有识别到章节标题，已按长度自动分节")
	}
	for i, sec := range sections {
		ch := buildChapter(i, sec)
		b.Chapters = append(b.Chapters, ch)
	}
	if len(b.Chapters) == 0 {
		return nil, fmt.Errorf("TXT 中没有可阅读的正文")
	}
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

func parseMetadata(lines []string) (title, author string, skip map[int]bool) {
	skip = make(map[int]bool)
	limit := min(30, len(lines))
	for i := 0; i < limit; i++ {
		line := lines[i]
		if m := reTitleMeta.FindStringSubmatch(line); len(m) == 3 && title == "" {
			title = book.CollapseSpace(m[2])
			skip[i] = true
			continue
		}
		if m := reAuthMeta.FindStringSubmatch(line); len(m) == 3 && author == "" {
			author = book.CollapseSpace(m[2])
			skip[i] = true
		}
	}
	return title, author, skip
}

func splitSections(lines []string, skip map[int]bool) ([]section, bool) {
	lines = cleanLines(lines, skip)
	var headings []struct {
		index int
		title string
		level int
	}
	for i, line := range lines {
		if title, level, ok := detectHeading(line); ok {
			headings = append(headings, struct {
				index int
				title string
				level int
			}{i, title, level})
		}
	}
	if len(headings) >= 1 {
		var out []section
		if headings[0].index > 0 {
			preface := lines[:headings[0].index]
			if hasContent(preface) {
				out = append(out, section{title: "前言", level: 1, lines: preface})
			}
		}
		for i, h := range headings {
			end := len(lines)
			if i+1 < len(headings) {
				end = headings[i+1].index
			}
			body := lines[h.index+1 : end]
			out = append(out, section{title: h.title, level: h.level, lines: body})
		}
		return out, true
	}
	return chunkSections(lines), false
}

func detectHeading(line string) (string, int, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || len([]rune(trimmed)) > 80 {
		return "", 0, false
	}
	if m := reMarkdown.FindStringSubmatch(trimmed); len(m) == 3 {
		title := strings.TrimSpace(m[2])
		if title != "" {
			return title, len(m[1]), true
		}
	}
	if reSpecialCN.MatchString(trimmed) {
		return trimmed, 1, true
	}
	if reChapterCN.MatchString(trimmed) {
		return trimmed, 1, true
	}
	if len([]rune(trimmed)) <= 70 && reChapterEN.MatchString(trimmed) {
		return trimmed, 1, true
	}
	return "", 0, false
}

func cleanLines(lines []string, skip map[int]bool) []string {
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if skip[i] {
			out = append(out, "")
			continue
		}
		out = append(out, line)
	}
	return out
}

func hasContent(lines []string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			return true
		}
	}
	return false
}

func chunkSections(lines []string) []section {
	var out []section
	var cur []string
	bytes := 0
	flush := func() {
		if !hasContent(cur) {
			cur = nil
			bytes = 0
			return
		}
		out = append(out, section{
			title: fmt.Sprintf("第 %d 节", len(out)+1),
			level: 1,
			lines: cur,
		})
		cur = nil
		bytes = 0
	}
	for _, line := range lines {
		cur = append(cur, line)
		bytes += len(line) + 1
		if len(cur) >= maxChunkLine || bytes >= maxChunkByte {
			// Prefer to cut at a blank line so paragraphs stay intact, but do
			// not let a book without blank lines become one giant section.
			if strings.TrimSpace(line) == "" || len(cur) >= maxChunkLine+200 || bytes >= maxChunkByte+16<<10 {
				flush()
			}
		}
	}
	flush()
	return out
}

func buildChapter(index int, sec section) book.Chapter {
	href := fmt.Sprintf("txt:section-%04d", index+1)
	anchor := fmt.Sprintf("sec-%d", index+1)
	ch := book.Chapter{
		Index:   index,
		Href:    href,
		Title:   sec.title,
		Heading: sec.title,
		Anchors: map[string]int{anchor: 0},
		Blocks:  []book.Block{{Kind: book.BlockHeading, Level: max(1, sec.level), Spans: []book.Span{{Text: sec.title}}}},
	}
	ch.Blocks = append(ch.Blocks, buildBlocks(sec.lines)...)
	return ch
}

func buildBlocks(lines []string) []book.Block {
	var blocks []book.Block
	for i := 0; i < len(lines); {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}
		if start, end, ok := codeGroup(lines, i); ok {
			blocks = append(blocks, book.Block{Kind: book.BlockCode, Code: codeText(lines[start:end])})
			i = end
			continue
		}
		if _, ok := parseListItem(line); ok {
			for i < len(lines) {
				if _, ok := parseListItem(lines[i]); !ok {
					break
				}
				blocks = append(blocks, parseListItemBlock(lines[i]))
				i++
			}
			continue
		}
		if isQuoteLine(line) {
			var quote []string
			for i < len(lines) && isQuoteLine(lines[i]) {
				quote = append(quote, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), ">")))
				i++
			}
			text := joinParagraph(quote)
			if text != "" {
				blocks = append(blocks, book.Block{Kind: book.BlockParagraph, Quote: true, Spans: []book.Span{{Text: text}}})
			}
			continue
		}
		var para []string
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
			if len(para) > 0 {
				if _, ok := parseListItem(lines[i]); ok {
					break
				}
				if isQuoteLine(lines[i]) {
					break
				}
				if _, _, ok := codeGroup(lines, i); ok {
					break
				}
			}
			para = append(para, lines[i])
			i++
		}
		if text := joinParagraph(para); text != "" {
			blocks = append(blocks, book.Block{Kind: book.BlockParagraph, Spans: []book.Span{{Text: text}}})
		}
	}
	return blocks
}

func joinParagraph(lines []string) string {
	var out string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = book.JoinLines(out, line)
	}
	return book.SanitizeText(strings.TrimSpace(out))
}

func isQuoteLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), ">")
}

func parseListItem(line string) (int, bool) {
	m := reListItem.FindStringSubmatch(line)
	if len(m) != 4 {
		return 0, false
	}
	indent := len(strings.ReplaceAll(m[1], "\t", "    "))
	depth := indent/2 + 1
	if depth > 6 {
		depth = 6
	}
	return depth, true
}

func parseListItemBlock(line string) book.Block {
	m := reListItem.FindStringSubmatch(line)
	if len(m) != 4 {
		return book.Block{Kind: book.BlockParagraph, Spans: []book.Span{{Text: strings.TrimSpace(line)}}}
	}
	indent := len(strings.ReplaceAll(m[1], "\t", "    "))
	depth := indent/2 + 1
	if depth > 6 {
		depth = 6
	}
	marker := m[2]
	ordered := false
	number := 0
	if n, err := strconv.Atoi(strings.TrimRight(marker, ".)、")); err == nil {
		ordered = true
		number = n
	}
	return book.Block{
		Kind:    book.BlockListItem,
		Depth:   depth,
		Ordered: ordered,
		Number:  number,
		Spans:   []book.Span{{Text: book.SanitizeText(strings.TrimSpace(m[3]))}},
	}
}

func codeGroup(lines []string, start int) (int, int, bool) {
	if !isCodeLine(lines[start]) {
		return 0, 0, false
	}
	end := start
	indented := 0
	for end < len(lines) {
		line := lines[end]
		if strings.TrimSpace(line) == "" {
			// Allow one blank line inside a code block.
			if end+1 < len(lines) && isCodeLine(lines[end+1]) {
				end++
				continue
			}
			break
		}
		if !isCodeLine(line) {
			break
		}
		if hasIndent(line) {
			indented++
		}
		end++
	}
	if end-start < 2 {
		// A single indented line is usually prose; only treat it as code when
		// it also looks like source code.
		trimmed := strings.TrimSpace(lines[start])
		symbols := 0
		for _, r := range trimmed {
			switch r {
			case '{', '}', ';', '=', '(', ')', '[', ']', '<', '>':
				symbols++
			}
		}
		if indented == 0 || symbols < 2 {
			return 0, 0, false
		}
	}
	return start, end, true
}

func hasIndent(line string) bool {
	return strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ")
}

func isCodeLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	if hasIndent(line) {
		return true
	}
	switch {
	case strings.HasPrefix(trimmed, "//"), strings.HasPrefix(trimmed, "/*"),
		strings.HasPrefix(trimmed, "#include"), strings.HasPrefix(trimmed, "package "),
		strings.HasPrefix(trimmed, "func "), strings.HasPrefix(trimmed, "def "),
		strings.HasPrefix(trimmed, "class "), strings.HasPrefix(trimmed, "import "),
		strings.HasPrefix(trimmed, "$ "), strings.HasPrefix(trimmed, ">>> "):
		return true
	}
	symbols := 0
	for _, r := range trimmed {
		switch r {
		case '{', '}', ';', '=', '(', ')', '[', ']', '<', '>':
			symbols++
		}
	}
	return symbols >= 4 && symbols*4 >= len([]rune(trimmed))
}

func codeText(lines []string) string {
	minIndent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		n := 0
		for _, r := range line {
			if r == ' ' {
				n++
			} else if r == '\t' {
				n += 4
			} else {
				break
			}
		}
		if minIndent < 0 || n < minIndent {
			minIndent = n
		}
	}
	if minIndent < 0 {
		minIndent = 0
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.ReplaceAll(line, "\t", "    ")
		if len(line) >= minIndent {
			line = line[minIndent:]
		}
		out = append(out, strings.TrimRight(line, " \t"))
	}
	return book.SanitizeText(strings.Join(out, "\n"))
}
