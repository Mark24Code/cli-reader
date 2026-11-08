// Package mobibook reads DRM-free MOBI and AZW3 (KF8) books.
package mobibook

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"cli-reader/internal/book"
	"cli-reader/internal/htmlimport"
	"cli-reader/internal/textdecode"
)

const (
	maxFileBytes       = 128 << 20
	maxTextBytes       = 64 << 20
	maxImageBytes      = 32 << 20
	maxTotalImageBytes = 128 << 20
	noImageIndex       = 0xFFFFFFFF
)

var (
	rePageBreak   = regexp.MustCompile(`(?i)<mbp:pagebreak\s*/?>`)
	reImgRec      = regexp.MustCompile(`(?i)(<img\b[^>]*?)\brecindex\s*=\s*["']?(\d+)["']?`)
	reKindleEmbed = regexp.MustCompile(`(?i)kindle:embed:([0-9A-V]+)(?:\?[^"'\s)>]*)?`)
	reGuide       = regexp.MustCompile(`(?is)<guide\b[^>]*>.*?</guide>`)
)

// Open reads a MOBI or AZW3 file into the shared book model.
func Open(name string) (*book.Book, error) {
	st, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxFileBytes {
		return nil, fmt.Errorf("MOBI/AZW3 超过 %d MiB 限制", maxFileBytes>>20)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	db, err := parsePalmDB(data)
	if err != nil {
		return nil, err
	}
	if len(db.Records) < 2 {
		return nil, fmt.Errorf("MOBI 记录不足")
	}
	rec0 := db.Records[0].Data
	pdh, err := parsePalmDocHeader(rec0)
	if err != nil {
		return nil, err
	}
	if pdh.Encryption != 0 {
		return nil, fmt.Errorf("%w: MOBI 内容已加密", book.ErrDRM)
	}
	mh, err := parseMOBIHeader(rec0)
	if err != nil {
		return nil, err
	}
	if mh != nil && mh.hasDRM() {
		return nil, fmt.Errorf("%w: MOBI 包含 DRM 记录", book.ErrDRM)
	}

	exth, err := parseEXTH(rec0, mh, textEncoding(mh))
	if err != nil {
		return nil, err
	}
	title := exth.Title
	if title == "" && mh != nil {
		title = mh.fullName(rec0)
	}
	if title == "" {
		title = db.Name
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	}
	author := strings.Join(exth.Authors, " / ")

	b := &book.Book{
		Path:       name,
		Title:      title,
		Author:     author,
		Language:   exth.Language,
		Identifier: exth.ASIN,
		Format:     book.FormatMOBI,
		Unit:       "章",
	}
	if exth.FixedLayout {
		b.Warnings = append(b.Warnings, "检测到固定版式 KF8，仅按文本流读取")
	}
	if exth.Rental != "" {
		b.Warnings = append(b.Warnings, "这本书带有租借/到期信息")
	}
	if exth.KF8Boundary {
		b.Warnings = append(b.Warnings, "检测到 MOBI7/KF8 混合书，优先读取前半部分")
	}

	rawText, err := decodeTextRecords(db, pdh, mh)
	if err != nil {
		return nil, err
	}
	htmlText := decodeBookText(rawText, textEncoding(mh))
	if strings.TrimSpace(htmlText) == "" {
		return nil, fmt.Errorf("MOBI 中没有可阅读的正文")
	}
	if mh != nil && mh.isKF8() && !strings.Contains(strings.ToLower(htmlText), "<html") {
		b.Warnings = append(b.Warnings, "KF8 内容可能不完整，已按文本流尽力解析")
	}
	htmlText = preprocessHTML(htmlText, mh)

	images, imageWarning := buildImages(db, mh)
	if imageWarning != "" {
		b.Warnings = append(b.Warnings, imageWarning)
	}
	b.SetImageProvider(images)

	baseHref := "mobi:content"
	res, err := htmlimport.Parse([]byte(htmlText), htmlimport.Options{
		Href:                   baseHref,
		GenerateHeadingAnchors: true,
		ResolveImage: func(ref *book.ImageRef) {
			if strings.HasPrefix(ref.Source, "mobi:img/") {
				ref.Path = ref.Source
				if ref.Alt == "" {
					ref.Alt = "图片"
				}
			}
		},
	})
	if err != nil {
		return nil, fmt.Errorf("解析 MOBI 正文失败: %w", err)
	}
	if len(res.Chapter.Blocks) == 0 {
		return nil, fmt.Errorf("MOBI 中没有可阅读的正文")
	}

	splitLevel := chooseSplitLevel(res.Headings)
	chapters, toc := splitChapter(res.Chapter, res.Headings, splitLevel, baseHref)
	if len(chapters) == 0 {
		chapters = []book.Chapter{*res.Chapter}
	}
	b.Chapters = chapters
	b.TOC = toc
	b.SetLinkResolver(func(base, raw string) book.LinkTarget {
		return resolveMOBILink(b, base, raw)
	})
	b.EnsureTOC()
	return b, nil
}

func textEncoding(mh *mobiHeader) uint32 {
	if mh == nil {
		return 1252
	}
	return mh.TextEncoding
}

func decodeTextRecords(db *palmDB, pdh palmDocHeader, mh *mobiHeader) ([]byte, error) {
	count := int(pdh.TextRecordCount)
	if count <= 0 || count >= len(db.Records) {
		return nil, fmt.Errorf("MOBI 文本记录数量无效")
	}
	flags := uint16(0)
	if mh != nil {
		flags = mh.ExtraRecordData
	}
	var huff *huffReader
	if pdh.Compression == 17480 {
		if mh == nil {
			return nil, fmt.Errorf("HUFF 压缩缺少 MOBI 头")
		}
		start := int(mh.HuffmanRecordOffset)
		n := int(mh.HuffmanRecordCount)
		if start <= 0 || n <= 0 || start+n > len(db.Records) {
			return nil, fmt.Errorf("HUFF/CDIC 记录范围无效")
		}
		cdics := make([][]byte, 0, n-1)
		for i := 1; i < n; i++ {
			cdics = append(cdics, db.Records[start+i].Data)
		}
		var err error
		huff, err = newHuffReader(db.Records[start].Data, cdics)
		if err != nil {
			return nil, err
		}
	}

	out := make([]byte, 0, pdh.TextLength)
	for i := 1; i <= count; i++ {
		payload := trimTrailingData(db.Records[i].Data, flags)
		var part []byte
		var err error
		switch pdh.Compression {
		case 1:
			part = payload
		case 2:
			part, err = palmDocDecompress(payload, maxTextBytes)
		case 17480:
			part, err = huff.unpack(payload)
		default:
			return nil, fmt.Errorf("不支持的 MOBI 压缩方式 %d", pdh.Compression)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, part...)
		if len(out) > maxTextBytes {
			out = out[:maxTextBytes]
			break
		}
	}
	if pdh.TextLength > 0 && uint32(len(out)) > pdh.TextLength {
		out = out[:pdh.TextLength]
	}
	return out, nil
}

func decodeBookText(data []byte, encoding uint32) string {
	switch encoding {
	case 65001:
		if utf8.Valid(data) {
			return string(data)
		}
	case 1252:
		if decoded, err := charmap.Windows1252.NewDecoder().Bytes(data); err == nil {
			return string(decoded)
		}
	}
	if utf8.Valid(data) {
		return string(data)
	}
	return textdecode.Decode(data).Text
}

func preprocessHTML(text string, mh *mobiHeader) string {
	text = rePageBreak.ReplaceAllString(text, "<hr/>")
	text = reGuide.ReplaceAllString(text, "")
	firstImage := uint32(noImageIndex)
	if mh != nil {
		firstImage = mh.FirstImageIndex
	}
	text = reImgRec.ReplaceAllStringFunc(text, func(match string) string {
		sub := reImgRec.FindStringSubmatch(match)
		if len(sub) != 3 {
			return match
		}
		n, err := strconv.Atoi(sub[2])
		if err != nil || n <= 0 {
			return match
		}
		key := "mobi:img/unknown"
		if firstImage != noImageIndex {
			key = fmt.Sprintf("mobi:img/%d", int(firstImage)+n-1)
		}
		return sub[1] + `src="` + key + `"`
	})
	text = reKindleEmbed.ReplaceAllStringFunc(text, func(match string) string {
		sub := reKindleEmbed.FindStringSubmatch(match)
		if len(sub) != 2 {
			return match
		}
		key := "mobi:img/unknown"
		if firstImage != noImageIndex {
			if n, err := strconv.ParseInt(sub[1], 32, 64); err == nil && n > 0 {
				key = fmt.Sprintf("mobi:img/%d", int64(firstImage)+n-1)
			}
		}
		return key
	})
	return text
}

func buildImages(db *palmDB, mh *mobiHeader) (*book.MapImages, string) {
	images := book.NewMapImages()
	if mh == nil || mh.FirstImageIndex == noImageIndex || int(mh.FirstImageIndex) >= len(db.Records) {
		return images, ""
	}
	total := 0
	skipped := 0
	for i := int(mh.FirstImageIndex); i < len(db.Records); i++ {
		data := db.Records[i].Data
		if len(data) < 4 || !isImageData(data) {
			continue
		}
		if len(data) > maxImageBytes {
			skipped++
			continue
		}
		if total+len(data) > maxTotalImageBytes {
			return images, "MOBI 图片总量超过限制，已跳过后续图片"
		}
		total += len(data)
		images.Put(fmt.Sprintf("mobi:img/%d", i), data)
	}
	if skipped > 0 {
		return images, fmt.Sprintf("%d 张图片超过大小限制，已跳过", skipped)
	}
	return images, ""
}

func isImageData(data []byte) bool {
	switch {
	case len(data) > 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		return true
	case len(data) > 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return true
	case len(data) > 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return true
	case len(data) > 2 && data[0] == 'B' && data[1] == 'M':
		return true
	case len(data) > 4 && (string(data[:4]) == "II*\x00" || string(data[:4]) == "MM\x00*"):
		return true
	case len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return true
	}
	return false
}

func chooseSplitLevel(headings []book.HeadingRef) int {
	for level := 1; level <= 3; level++ {
		n := 0
		for _, h := range headings {
			if h.Level <= level {
				n++
			}
		}
		if n >= 2 {
			return level
		}
	}
	return 0
}

type chapterPart struct {
	start   int
	end     int
	title   string
	heading *book.HeadingRef
}

func splitChapter(ch *book.Chapter, headings []book.HeadingRef, level int, baseHref string) ([]book.Chapter, []book.TOCEntry) {
	if level <= 0 || len(headings) == 0 {
		single := *ch
		if single.Title == "" {
			if len(headings) > 0 {
				single.Title = headings[0].Title
			} else {
				single.Title = single.DocTitle
			}
		}
		if single.Title == "" {
			single.Title = "正文"
		}
		single.Heading = single.Title
		toc := book.TOCEntriesFromHeadings(0, headings)
		if len(toc) == 0 {
			toc = nil
		}
		return []book.Chapter{single}, toc
	}

	var splits []book.HeadingRef
	for _, h := range headings {
		if h.Level <= level {
			splits = append(splits, h)
		}
	}
	if len(splits) < 2 {
		return splitChapter(ch, headings, 0, baseHref)
	}

	var parts []chapterPart
	if splits[0].Block > 0 {
		parts = append(parts, chapterPart{start: 0, end: splits[0].Block, title: "开篇"})
	}
	for i, h := range splits {
		end := len(ch.Blocks)
		if i+1 < len(splits) {
			end = splits[i+1].Block
		}
		hCopy := h
		parts = append(parts, chapterPart{start: h.Block, end: end, title: h.Title, heading: &hCopy})
	}

	chapters := make([]book.Chapter, len(parts))
	for i, p := range parts {
		href := fmt.Sprintf("%s#part%d", baseHref, i+1)
		if i == 0 && p.heading == nil {
			href = baseHref
		}
		blocks := append([]book.Block(nil), ch.Blocks[p.start:p.end]...)
		chapters[i] = book.Chapter{
			Index:    i,
			Href:     href,
			Title:    p.title,
			Heading:  p.title,
			DocTitle: ch.DocTitle,
			Blocks:   blocks,
			Anchors:  make(map[string]int),
		}
	}
	anchorChapter := make(map[string]int, len(ch.Anchors))
	for name, blockIdx := range ch.Anchors {
		for i, p := range parts {
			if blockIdx >= p.start && blockIdx < p.end {
				chapters[i].Anchors[name] = blockIdx - p.start
				anchorChapter[name] = i
				break
			}
		}
	}

	var toc []book.TOCEntry
	if parts[0].heading == nil {
		toc = append(toc, book.TOCEntry{Title: parts[0].title, Chapter: 0})
	}
	for _, h := range headings {
		chap, ok := anchorChapter[h.Anchor]
		if !ok {
			continue
		}
		depth := h.Level - 1
		if depth < 0 {
			depth = 0
		}
		toc = append(toc, book.TOCEntry{Title: h.Title, Chapter: chap, Fragment: h.Anchor, Depth: depth})
	}
	return chapters, toc
}

func resolveMOBILink(b *book.Book, base, raw string) book.LinkTarget {
	target := book.LinkTarget{Chapter: -1}
	raw = strings.TrimSpace(book.SanitizeText(raw))
	if raw == "" {
		target.Missing = true
		return target
	}
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		target.Fragment = raw[i+1:]
		raw = raw[:i]
	}
	if raw == "" {
		for i := range b.Chapters {
			if _, ok := b.Chapters[i].Anchors[target.Fragment]; ok {
				target.Chapter = i
				return target
			}
		}
		target.Chapter = b.ChapterByHref(base)
		target.Missing = true
		return target
	}
	if idx := b.ChapterByHref(raw); idx >= 0 {
		target.Chapter = idx
		return target
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "kindle:") || strings.HasPrefix(lower, "filepos:") {
		target.Missing = true
		return target
	}
	if strings.HasPrefix(raw, "//") {
		target.External = true
		return target
	}
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
		target.External = true
		return target
	}
	target.Missing = true
	return target
}
