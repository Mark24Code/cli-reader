// Package pdfbook extracts reflowable text and page images from PDF files.
package pdfbook

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"cli-reader/internal/book"
)

const (
	maxFileBytes       = 512 << 20
	maxPages           = 2000
	maxPageTextBytes   = 1 << 20
	maxPageTextTotal   = 128 << 20
	maxImageBytes      = 32 << 20
	maxTotalImageBytes = 64 << 20
)

var (
	reMonoFont    = regexp.MustCompile(`(?i)(mono|courier|consolas|menlo|monaco|source.?code|fira.?code|dejavu.?sans.?mono|inconsolata|CMTT|SFMono)`)
	reCJKFont     = regexp.MustCompile(`(?i)(song|hei|kai|ming|fang|yahei|simsun|simhei|pingfang|hiragino|noto.*cjk|source.*han|fz|gb)`)
	reMathFont    = regexp.MustCompile(`(?i)(TimesNewRoman|SymbolMT|CambriaMath|STIX|LatinModernMath)`)
	reMathVarFont = regexp.MustCompile(`(?i)(ItalicMT|SymbolMT|CambriaMath|STIX|LatinModernMath)`)
)

type textCell struct {
	text string
	x    float64
}

type textRow struct {
	text     string
	mono     bool
	x        float64
	y        float64
	fontSize float64
	cells    []textCell
}

// Open reads a PDF into the shared book model.
func Open(name string) (*book.Book, error) {
	st, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxFileBytes {
		return nil, fmt.Errorf("PDF 超过 %d MiB 限制", maxFileBytes>>20)
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	closeOnReturn := true
	defer func() {
		if closeOnReturn {
			_ = f.Close()
		}
	}()
	r, err := pdf.NewReader(f, st.Size())
	if err != nil {
		lower := strings.ToLower(err.Error())
		if err == pdf.ErrInvalidPassword || strings.Contains(lower, "encrypt") ||
			strings.Contains(lower, "password") || strings.Contains(lower, "decrypt") {
			return nil, fmt.Errorf("%w: PDF 已加密或需要密码", book.ErrDRM)
		}
		return nil, fmt.Errorf("无法解析 PDF: %w", err)
	}

	numPages := r.NumPage()
	if numPages <= 0 {
		return nil, fmt.Errorf("PDF 没有页面")
	}
	title, author := pdfMetadata(r)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	}
	b := &book.Book{
		Path:   name,
		Title:  title,
		Author: author,
		Format: book.FormatPDF,
		Unit:   "页",
		Mode:   book.ModeText,
	}

	if numPages > maxPages {
		b.Warnings = append(b.Warnings, fmt.Sprintf("PDF 有 %d 页，超过上限，仅处理前 %d 页", numPages, maxPages))
		numPages = maxPages
	}

	images, imageRefs, imageWarnings := extractImages(name, numPages)
	b.Warnings = append(b.Warnings, imageWarnings...)
	if images != nil {
		b.SetImageProvider(images)
	}

	// Large PDFs keep the reader open and extract page text on demand, so only
	// the pages around the current one occupy memory. Small PDFs stay eager to
	// keep tests and simple documents trivially inspectable.
	if numPages > 30 {
		for n := 1; n <= numPages; n++ {
			b.Chapters = append(b.Chapters, book.Chapter{
				Index: n - 1,
				Href:  fmt.Sprintf("pdf:page-%04d", n),
				Title: fmt.Sprintf("第 %d 页", n),
			})
		}
		b.SetChapterLoader(func(i int) error {
			n := i + 1
			blocks, textLen, garbled := extractPage(r, n)
			for _, ref := range imageRefs[n] {
				refCopy := ref
				blocks = append(blocks, book.Block{Kind: book.BlockImage, Image: &refCopy})
			}
			if len(blocks) == 0 {
				msg := "（本页没有可提取的文字）"
				if garbled {
					msg = "（本页文本编码无法识别，已跳过）"
				}
				blocks = append(blocks, book.Block{
					Kind:  book.BlockParagraph,
					Spans: []book.Span{{Text: msg}},
				})
			}
			target := &b.Chapters[i]
			target.Blocks = blocks
			target.PageMode = textLen == 0 && len(imageRefs[n]) > 0
			return nil
		})
		b.SetCloser(f.Close)
		closeOnReturn = false
		b.EnsureTOC()
		return b, nil
	}

	totalText := 0
	emptyPages := 0
	garbledPages := 0
	for n := 1; n <= numPages; n++ {
		if totalText >= maxPageTextTotal {
			b.Warnings = append(b.Warnings, "PDF 文本超过总量限制，已停止处理后续页面")
			break
		}
		blocks, textLen, garbled := extractPage(r, n)
		if textLen == 0 {
			emptyPages++
		}
		if garbled {
			garbledPages++
		}
		totalText += textLen

		for _, ref := range imageRefs[n] {
			refCopy := ref
			blocks = append(blocks, book.Block{Kind: book.BlockImage, Image: &refCopy})
		}
		pageMode := textLen == 0 && len(imageRefs[n]) > 0
		if len(blocks) == 0 {
			msg := "（本页没有可提取的文字）"
			if garbled {
				msg = "（本页文本编码无法识别，已跳过）"
			}
			blocks = append(blocks, book.Block{
				Kind:  book.BlockParagraph,
				Spans: []book.Span{{Text: msg}},
			})
		}
		b.Chapters = append(b.Chapters, book.Chapter{
			Index:    n - 1,
			Href:     fmt.Sprintf("pdf:page-%04d", n),
			Title:    fmt.Sprintf("第 %d 页", n),
			PageMode: pageMode,
			Blocks:   blocks,
		})
	}
	if len(b.Chapters) == 0 {
		return nil, fmt.Errorf("PDF 中没有可阅读的页面")
	}
	if emptyPages > 0 {
		b.Warnings = append(b.Warnings, fmt.Sprintf("%d 页没有可提取文字（可能是扫描页）", emptyPages))
	}
	if garbledPages > 0 {
		b.Warnings = append(b.Warnings,
			fmt.Sprintf("%d 页文本编码无法可靠解析，已跳过乱码；有图片的页面按图片显示", garbledPages))
	}
	b.EnsureTOC()
	return b, nil
}

func pdfMetadata(r *pdf.Reader) (title, author string) {
	defer func() { _ = recover() }()
	info := r.Trailer().Key("Info")
	if info.IsNull() {
		return "", ""
	}
	clean := func(s string) string {
		s = strings.TrimSpace(s)
		if s == "<nil>" || strings.EqualFold(s, "null") {
			return ""
		}
		return s
	}
	title = clean(info.Key("Title").Text())
	author = clean(info.Key("Author").Text())
	if title == "" {
		title = clean(info.Key("Title").String())
	}
	if author == "" {
		author = clean(info.Key("Author").String())
	}
	return title, author
}

func extractPage(r *pdf.Reader, n int) (blocks []book.Block, textLen int, garbled bool) {
	defer func() { _ = recover() }()
	page := r.Page(n)
	rows, hadText := pageRows(page)
	if len(rows) > 0 {
		blocks = rowsToBlocks(rows)
		textLen = blocksTextLen(blocks)
		if textLen > maxPageTextBytes {
			blocks = truncateBlocks(blocks, maxPageTextBytes)
			textLen = maxPageTextBytes
		}
		return blocks, textLen, false
	}
	if hadText {
		// The content stream had text, but every usable glyph was filtered out
		// (for example an encrypted watermark font). Do not fall back to
		// GetPlainText, which would bring the same garbage back.
		return nil, 0, true
	}
	plain, err := page.GetPlainText(nil)
	if err != nil {
		return nil, 0, false
	}
	if bad, total := replacementStats(plain); total > 10 && bad*5 > total {
		return nil, 0, true
	}
	plain = strings.ReplaceAll(plain, "�", "")
	blocks = plainToBlocks(plain)
	textLen = blocksTextLen(blocks)
	return blocks, textLen, false
}

func replacementStats(s string) (bad, total int) {
	for _, r := range s {
		if r == '�' {
			bad++
		}
		if !unicode.IsSpace(r) {
			total++
		}
	}
	return bad, total
}

func pageRows(page pdf.Page) ([]textRow, bool) {
	texts := page.Content().Text
	if len(texts) == 0 {
		return nil, false
	}
	hadText := false
	type fontStat struct{ total, bad int }
	stats := make(map[string]*fontStat)
	for _, t := range texts {
		if t.S == "" {
			continue
		}
		st := stats[t.Font]
		if st == nil {
			st = &fontStat{}
			stats[t.Font] = st
		}
		st.total++
		if isBadGlyph(t.S, t.Font) {
			st.bad++
		}
		if strings.TrimSpace(t.S) != "" {
			hadText = true
		}
	}
	// Drop fonts that are mostly unmapped. Scanned/watermarked PDFs sometimes
	// carry a hidden text layer using a font without a usable ToUnicode map.
	filtered := make([]pdf.Text, 0, len(texts))
	for _, t := range texts {
		if t.S == "" {
			continue
		}
		if st := stats[t.Font]; st != nil && st.total >= 4 && st.bad*2 > st.total {
			continue
		}
		t.S = cleanGlyphText(t.S, t.Font)
		if t.S == "" {
			continue
		}
		filtered = append(filtered, t)
	}
	texts = filtered
	if len(texts) == 0 {
		return nil, hadText
	}

	// Group glyphs by vertical position while preserving content-stream order
	// inside each row. Some PDF generators lay out CJK runs with a decreasing
	// X coordinate; sorting those by X would reverse the text.
	type rowGroup struct {
		y     float64
		fs    float64
		items []pdf.Text
	}
	var groups []rowGroup
	for _, t := range texts {
		idx := -1
		for i := range groups {
			tol := 1.5
			if groups[i].fs > tol {
				tol = math.Max(tol, groups[i].fs*0.35)
			}
			if math.Abs(t.Y-groups[i].y) <= tol {
				idx = i
				break
			}
		}
		if idx >= 0 {
			groups[idx].items = append(groups[idx].items, t)
			if t.FontSize > groups[idx].fs {
				groups[idx].fs = t.FontSize
			}
		} else {
			groups = append(groups, rowGroup{y: t.Y, fs: t.FontSize, items: []pdf.Text{t}})
		}
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].y > groups[j].y })

	rows := make([]textRow, 0, len(groups))
	monoRows := 0
	for _, g := range groups {
		row, ok := buildTextRow(g.items)
		if !ok {
			continue
		}
		if row.mono {
			monoRows++
		}
		rows = append(rows, row)
	}
	// A genuinely monospaced body font should not turn the whole page into
	// code, but a page that is entirely code should stay code.
	if len(rows) > 0 && monoRows*10 > len(rows)*8 {
		codey := 0
		for _, r := range rows {
			if looksCodeLike(r.text) {
				codey++
			}
		}
		if codey*2 < len(rows) {
			for i := range rows {
				rows[i].mono = false
			}
		}
	}
	return rows, hadText
}

func looksCodeLike(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	symbols := 0
	for _, r := range trimmed {
		switch r {
		case '{', '}', ';', '=', '(', ')', '[', ']', '<', '>', '\\', '/':
			symbols++
		}
	}
	if symbols >= 3 {
		return true
	}
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{
		"func ", "def ", "class ", "import ", "package ", "#include", "//", "/*",
		"public ", "private ", "const ", "var ", "let ", "return ", "select ", "from ",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return strings.HasPrefix(s, "    ") || strings.HasPrefix(s, "\t")
}

func isBadGlyph(s, font string) bool {
	cjkFont := reCJKFont.MatchString(font)
	for _, r := range s {
		switch {
		case r == '�' || r == '￼':
			return true
		case r >= 0x80 && r <= 0x9f:
			return true
		case r >= 0xe000 && r <= 0xf8ff:
			return true
		case cjkFont && r >= 0xa0 && r <= 0xff:
			return true
		}
	}
	return false
}

func cleanGlyphText(s, font string) string {
	cjkFont := reCJKFont.MatchString(font)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '�' || r == '￼':
			continue
		case r == '\uf04c':
			b.WriteRune('…')
			continue
		case r >= 0x80 && r <= 0x9f:
			continue
		case r >= 0xe000 && r <= 0xf8ff:
			continue
		case cjkFont && r >= 0xa0 && r <= 0xff:
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// suppressGaps detects rows whose glyphs are mostly laid out right-to-left
// (some CJK PDF generators use a decreasing X coordinate). For those rows the
// content-stream order is the reading order, and gap-based spaces would insert
// noise between text runs.
func suppressGaps(items []pdf.Text) bool {
	xs := make([]float64, 0, len(items))
	for _, t := range items {
		if strings.TrimSpace(t.S) != "" {
			xs = append(xs, t.X)
		}
	}
	if len(xs) < 3 {
		return false
	}
	increases, decreases := 0, 0
	for i := 1; i < len(xs); i++ {
		d := xs[i] - xs[i-1]
		if d > 0.2 {
			increases++
		} else if d < -0.2 {
			decreases++
		}
	}
	return decreases >= 2 && decreases > increases
}

func isFormulaRow(items []pdf.Text) bool {
	if len(items) < 3 {
		return false
	}
	visible, mathFonts, mathVars, operators, cjk := 0, 0, 0, 0, 0
	for _, t := range items {
		s := book.SanitizeText(t.S)
		if strings.TrimSpace(s) == "" {
			continue
		}
		visible++
		if reMathFont.MatchString(t.Font) {
			mathFonts++
		}
		if reMathVarFont.MatchString(t.Font) {
			mathVars++
		}
		for _, r := range s {
			if book.IsCJK(r) {
				cjk++
			}
			if strings.ContainsRune("=+-<>≤≥×÷±≈≠()[]{}", r) {
				operators++
			}
		}
	}
	return cjk == 0 && visible >= 3 && mathVars >= 2 &&
		mathFonts*2 >= visible && operators >= 1
}

type formulaPart struct {
	base pdf.Text
	sub  string
	sup  string
}

// buildFormulaRow reconstructs a math line: glyphs are ordered by X, and
// smaller glyphs whose baseline is offset are attached as sub/superscripts.
func buildFormulaRow(items []pdf.Text) (string, bool) {
	if !isFormulaRow(items) {
		return "", false
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].X < items[j].X })
	baseFS := 0.0
	for _, t := range items {
		if t.FontSize > baseFS {
			baseFS = t.FontSize
		}
	}
	var parts []formulaPart
	for _, t := range items {
		s := book.SanitizeText(t.S)
		if strings.TrimSpace(s) == "" {
			continue
		}
		if t.FontSize > 0 && t.FontSize < baseFS*0.8 && len(parts) > 0 {
			last := &parts[len(parts)-1]
			baseY := last.base.Y
			switch {
			case t.Y < baseY-0.8:
				last.sub += s
			case t.Y > baseY+0.8:
				last.sup += s
			default:
				last.base.S += s
			}
			continue
		}
		parts = append(parts, formulaPart{base: t})
	}
	parts = reorderFormulaOperators(parts, baseFS)
	if linear, ok := linearFormulaOrder(parts, baseFS); ok {
		parts = linear
	}
	var sb strings.Builder
	for i, p := range parts {
		text := strings.TrimSpace(book.SanitizeText(p.base.S))
		if text == "" {
			continue
		}
		if sb.Len() > 0 && formulaNeedsSpace(parts, i) {
			sb.WriteString(" ")
		}
		sb.WriteString(text)
		if p.sub != "" {
			sb.WriteString(toSubscriptScript(p.sub))
		}
		if p.sup != "" {
			sb.WriteString(toSuperscriptScript(p.sup))
		}
	}
	out := strings.TrimSpace(sb.String())
	return out, out != ""
}

// reorderFormulaOperators fixes operators that the PDF positions inside a
// term. For example, some generators place the second "+" of "w3 + x3" at
// w3's X coordinate, so an X sort yields "w3 + x3" instead of "w3x3 +".
func reorderFormulaOperators(parts []formulaPart, baseFS float64) []formulaPart {
	if len(parts) < 3 {
		return parts
	}
	termGap := 1.1 * baseFS
	if termGap < 8 {
		termGap = 8
	}
	for i := 1; i < len(parts); i++ {
		op := strings.TrimSpace(parts[i].base.S)
		if (op == "×" || op == "÷") && i+2 < len(parts) &&
			isOperandPart(parts[i+1]) && isNumberPart(parts[i+2]) &&
			parts[i+2].base.X-parts[i+1].base.X <= termGap*1.5 {
			// "× b 1" -> "b × 1"
			parts[i], parts[i+1] = parts[i+1], parts[i]
			i++
			continue
		}
		if op != "+" && op != "-" {
			continue
		}
		if i+1 < len(parts) && isEllipsisPart(parts[i+1]) {
			// "x2 + …" was emitted as "x2 + …" already, but a following plus
			// may be at the wrong side of the ellipsis.
			parts[i], parts[i+1] = parts[i+1], parts[i]
			i++
			continue
		}
		force := isEllipsisPart(parts[i-1])
		if !force {
			if !isOperandPart(parts[i-1]) || !isOperandPart(parts[i+1]) {
				continue
			}
			// Only move the operator when its two operands are close enough to
			// be one term (e.g. w3 + x3). If they are two terms (x1 + w2),
			// leave it.
			if parts[i+1].base.X-parts[i-1].base.X > termGap {
				continue
			}
		} else if !isOperandPart(parts[i+1]) {
			continue
		}
		end := i + 1
		for end+1 < len(parts) && isOperandPart(parts[end+1]) {
			if parts[end+1].base.X-parts[end].base.X > termGap {
				break
			}
			end++
		}
		if end == i {
			continue
		}
		opPart := parts[i]
		copy(parts[i:end], parts[i+1:end+1])
		parts[end] = opPart
		i = end
	}
	return parts
}

// linearFormulaOrder rebuilds simple linear formulas from their terms instead
// of trusting operator X positions. It is used only when the formula has no
// grouping parentheses (a trailing equation number is allowed).
func linearFormulaOrder(parts []formulaPart, baseFS float64) ([]formulaPart, bool) {
	core, suffix, ok := stripEquationNumber(parts)
	if !ok {
		return nil, false
	}
	for _, p := range core {
		s := strings.TrimSpace(p.base.S)
		if s == "(" || s == ")" || s == "[" || s == "]" {
			return nil, false
		}
	}
	termGap := 1.1 * baseFS
	if termGap < 8 {
		termGap = 8
	}

	var terms [][]formulaPart
	var binaryOps []formulaPart
	var eqPart *formulaPart
	for i := 0; i < len(core); {
		p := core[i]
		s := strings.TrimSpace(p.base.S)
		switch {
		case s == "=":
			cp := p
			eqPart = &cp
			i++
		case isAddOperator(s):
			binaryOps = append(binaryOps, p)
			i++
		case s == "×" || s == "÷" || isComparison(s) || s == ",":
			return nil, false
		default:
			if !isOperandPart(p) {
				return nil, false
			}
			term := []formulaPart{p}
			lastX := p.base.X
			i++
			for i < len(core) {
				q := core[i]
				qs := strings.TrimSpace(q.base.S)
				if qs == "×" || qs == "÷" {
					if i+1 < len(core) && isOperandPart(core[i+1]) &&
						core[i+1].base.X-lastX <= termGap*1.8 {
						term = append(term, q, core[i+1])
						lastX = core[i+1].base.X
						i += 2
						continue
					}
					break
				}
				if !isOperandPart(q) || q.base.X-lastX > termGap {
					break
				}
				term = append(term, q)
				lastX = q.base.X
				i++
			}
			terms = append(terms, term)
		}
	}
	if len(terms) < 2 {
		return nil, false
	}

	lhsCount := 0
	if eqPart != nil {
		for _, term := range terms {
			if term[0].base.X < eqPart.base.X {
				lhsCount++
			}
		}
		if lhsCount != 1 {
			return nil, false
		}
	}
	expectedOps := len(terms) - 1
	if eqPart != nil {
		expectedOps--
	}
	if expectedOps < 0 || len(binaryOps) != expectedOps {
		return nil, false
	}

	out := make([]formulaPart, 0, len(parts))
	out = append(out, terms[0]...)
	start := 1
	if eqPart != nil {
		out = append(out, *eqPart)
		if len(terms) < 2 {
			return nil, false
		}
		out = append(out, terms[1]...)
		start = 2
	}
	opIdx := 0
	for i := start; i < len(terms); i++ {
		if opIdx >= len(binaryOps) {
			return nil, false
		}
		out = append(out, binaryOps[opIdx])
		opIdx++
		out = append(out, terms[i]...)
	}
	out = append(out, suffix...)
	return out, true
}

func stripEquationNumber(parts []formulaPart) (core, suffix []formulaPart, ok bool) {
	n := len(parts)
	if n >= 3 && strings.TrimSpace(parts[n-1].base.S) == ")" &&
		isNumberPart(parts[n-2]) && strings.TrimSpace(parts[n-3].base.S) == "(" {
		return parts[:n-3], parts[n-3:], true
	}
	return parts, nil, true
}

func isAddOperator(s string) bool {
	return s == "+" || s == "-" || s == "±" || s == "∓"
}

func isComparison(s string) bool {
	switch s {
	case "<", ">", "≤", "≥", "≠", "≈":
		return true
	}
	return false
}

func isOperandPart(p formulaPart) bool {
	s := strings.TrimSpace(p.base.S)
	if s == "" {
		return false
	}
	for _, r := range s {
		if strings.ContainsRune("=+-<>≤≥×÷±≈≠()[]{},;", r) {
			return false
		}
	}
	return true
}

func isNumberPart(p formulaPart) bool {
	return isAllDigits(strings.TrimSpace(p.base.S))
}

func isEllipsisPart(p formulaPart) bool {
	s := strings.TrimSpace(p.base.S)
	return strings.Contains(s, "…") || strings.Contains(s, "...") || strings.Contains(s, "⋯")
}

func formulaNeedsSpace(parts []formulaPart, i int) bool {
	cur := strings.TrimSpace(parts[i].base.S)
	prev := strings.TrimSpace(parts[i-1].base.S)
	if isMathOperator(prev) || isMathOperator(cur) {
		return true
	}
	if cur == "(" || cur == "[" {
		// Function calls such as a(...) stay tight; equation numbers like (4)
		// get a separating space.
		if i+2 < len(parts) && isAllDigits(parts[i+1].base.S) && (parts[i+2].base.S == ")" || parts[i+2].base.S == "]") {
			return true
		}
		return false
	}
	if cur == ")" || cur == "]" || cur == "," || cur == ";" {
		return false
	}
	if prev == "(" || prev == "[" || prev == "," {
		return false
	}
	return false
}

func isMathOperator(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("=+-<>≤≥×÷±≈≠", r) {
			return false
		}
	}
	return true
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

var subscriptMap = map[rune]rune{
	'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄',
	'5': '₅', '6': '₆', '7': '₇', '8': '₈', '9': '₉',
	'+': '₊', '-': '₋', '=': '₌', '(': '₍', ')': '₎',
	'a': 'ₐ', 'e': 'ₑ', 'h': 'ₕ', 'i': 'ᵢ', 'j': 'ⱼ',
	'k': 'ₖ', 'l': 'ₗ', 'm': 'ₘ', 'n': 'ₙ', 'o': 'ₒ',
	'p': 'ₚ', 'r': 'ᵣ', 's': 'ₛ', 't': 'ₜ', 'u': 'ᵤ',
	'v': 'ᵥ', 'x': 'ₓ',
}

var superscriptMap = map[rune]rune{
	'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴',
	'5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
	'+': '⁺', '-': '⁻', '=': '⁼', '(': '⁽', ')': '⁾',
	'n': 'ⁿ', 'i': 'ⁱ',
}

func toSubscriptScript(s string) string {
	var b strings.Builder
	for _, r := range s {
		if m, ok := subscriptMap[r]; ok {
			b.WriteRune(m)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func toSuperscriptScript(s string) string {
	var b strings.Builder
	for _, r := range s {
		if m, ok := superscriptMap[r]; ok {
			b.WriteRune(m)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func buildTextRow(items []pdf.Text) (textRow, bool) {
	if len(items) == 0 {
		return textRow{}, false
	}
	if text, ok := buildFormulaRow(items); ok {
		x := math.Inf(1)
		y := items[0].Y
		fontSize := 0.0
		for _, t := range items {
			if t.X < x {
				x = t.X
			}
			if t.FontSize > fontSize {
				fontSize = t.FontSize
			}
		}
		if math.IsInf(x, 1) {
			x = 0
		}
		return textRow{text: text, x: x, y: y, fontSize: fontSize}, true
	}
	// Keep content-stream order. It is the reading order for most generators,
	// including CJK layouts with decreasing X coordinates.
	noGapSpaces := suppressGaps(items)
	var sb strings.Builder
	var cells []textCell
	var cellText strings.Builder
	cellX := 0.0
	cellStarted := false
	flushCell := func() {
		txt := strings.TrimSpace(cellText.String())
		if txt != "" {
			cells = append(cells, textCell{text: txt, x: cellX})
		}
		cellText.Reset()
		cellStarted = false
	}
	prevEnd := math.Inf(-1)
	fontSize := 0.0
	mono, total := 0, 0
	x := math.Inf(1)
	y := items[0].Y
	for _, t := range items {
		s := book.SanitizeText(t.S)
		if s == "" {
			continue
		}
		if t.FontSize > 0 && fontSize == 0 {
			fontSize = t.FontSize
		}
		total++
		if reMonoFont.MatchString(t.Font) {
			mono++
		}
		if t.X < x {
			x = t.X
		}
		fs := fontSize
		if fs <= 0 {
			fs = 10
		}
		if !noGapSpaces && !math.IsInf(prevEnd, -1) {
			gap := t.X - prevEnd
			if gap > maxFloat(fs*0.7, 6.0) {
				flushCell()
				if sb.Len() > 0 {
					sb.WriteString(" ")
				}
				cellX = t.X
				cellStarted = true
			} else if gap > maxFloat(0.3*fs, 0.6) {
				n := int(math.Round(gap / maxFloat(fs*0.5, 0.5)))
				if n < 1 {
					n = 1
				}
				if n > 16 {
					n = 16
				}
				spaces := strings.Repeat(" ", n)
				sb.WriteString(spaces)
				cellText.WriteString(spaces)
			}
		}
		if !cellStarted {
			cellX = t.X
			cellStarted = true
		}
		sb.WriteString(s)
		cellText.WriteString(s)
		if t.W > 0 {
			prevEnd = t.X + t.W
		} else {
			prevEnd = t.X + maxFloat(fs*0.5, 0.5)
		}
	}
	flushCell()
	if noGapSpaces {
		cells = nil
	}
	text := strings.TrimRight(sb.String(), " ")
	if strings.TrimSpace(text) == "" {
		return textRow{}, false
	}
	return textRow{
		text:     text,
		mono:     total > 0 && mono*2 > total,
		x:        x,
		y:        y,
		fontSize: fontSize,
		cells:    cells,
	}, true
}

func rowsToBlocks(rows []textRow) []book.Block {
	var blocks []book.Block
	for i := 0; i < len(rows); {
		if end, table, ok := detectTable(rows, i); ok {
			blocks = append(blocks, table)
			i = end
			continue
		}
		if rows[i].mono {
			start := i
			minX := rows[i].x
			for i < len(rows) && rows[i].mono {
				if rows[i].x < minX {
					minX = rows[i].x
				}
				i++
			}
			code := codeFromRows(rows[start:i], minX)
			if strings.TrimSpace(code) != "" {
				blocks = append(blocks, book.Block{Kind: book.BlockCode, Code: code})
			}
			continue
		}

		var text string
		var prev *textRow
		for i < len(rows) && !rows[i].mono {
			r := rows[i]
			if prev != nil {
				lineHeight := maxFloat(prev.fontSize, r.fontSize)
				if lineHeight <= 0 {
					lineHeight = 10
				}
				if prev.y-r.y > lineHeight*1.8 {
					break
				}
			}
			text = book.JoinLines(text, book.CollapseSpace(r.text))
			prev = &rows[i]
			i++
		}
		text = book.SanitizeText(strings.TrimSpace(text))
		if text != "" {
			blocks = append(blocks, book.Block{Kind: book.BlockParagraph, Spans: []book.Span{{Text: text}}})
		}
	}
	return blocks
}

// detectTable recognises simple two-column tables from glyph coordinates.
// It looks for repeated short first-column cells and a consistent second-column
// start, then groups multi-line cell content by the nearest first-column row
// anchor.
func detectTable(rows []textRow, start int) (int, book.Block, bool) {
	if start >= len(rows) || len(rows[start].cells) != 2 {
		return 0, book.Block{}, false
	}

	// Collect a window of nearby rows. A large vertical gap before a
	// single-cell row usually marks the end of the table.
	windowEnd := start
	prevY := rows[start].y
	prevFS := rows[start].fontSize
	for j := start; j < len(rows) && j < start+80; j++ {
		if j > start {
			fs := maxFloat(prevFS, rows[j].fontSize)
			if fs <= 0 {
				fs = 10
			}
			if prevY-rows[j].y > fs*2.4 && len(rows[j].cells) < 2 {
				break
			}
			prevY = rows[j].y
			prevFS = rows[j].fontSize
		}
		if len(rows[j].cells) == 0 {
			break
		}
		windowEnd = j + 1
	}
	window := rows[start:windowEnd]

	type columnPair struct {
		first, second float64
	}
	var pairs []columnPair
	for _, r := range window {
		if len(r.cells) != 2 {
			continue
		}
		if utf8.RuneCountInString(r.cells[0].text) > 24 {
			continue
		}
		pairs = append(pairs, columnPair{first: r.cells[0].x, second: r.cells[1].x})
	}
	if len(pairs) < 2 {
		return 0, book.Block{}, false
	}

	// Pick the largest cluster of second-column starts (header cells are often
	// centred and land elsewhere).
	best := []columnPair{}
	for _, p := range pairs {
		var group []columnPair
		for _, q := range pairs {
			if math.Abs(q.second-p.second) <= 12 {
				group = append(group, q)
			}
		}
		if len(group) > len(best) {
			best = group
		}
	}
	if len(best) < 2 {
		return 0, book.Block{}, false
	}
	col0Start := math.Inf(1)
	col1Start := math.Inf(1)
	for _, p := range best {
		if p.first < col0Start {
			col0Start = p.first
		}
		if p.second < col1Start {
			col1Start = p.second
		}
	}
	for _, p := range best {
		if math.Abs(p.first-col0Start) > 10 {
			return 0, book.Block{}, false
		}
	}

	// Keep the contiguous rows that belong to either column.
	kept := make([]textRow, 0, len(window))
	for _, r := range window {
		switch len(r.cells) {
		case 2:
			kept = append(kept, r)
		case 1:
			x := r.cells[0].x
			if math.Abs(x-col0Start) <= 6 || x >= col1Start-0.5 {
				kept = append(kept, r)
				continue
			}
			windowEnd = 0 // stop signal
		default:
			windowEnd = 0
		}
		if windowEnd == 0 {
			break
		}
	}
	if len(kept) < 2 {
		return 0, book.Block{}, false
	}

	type tableLine struct {
		y     float64
		texts [2]string
		has   [2]bool
	}
	lines := make([]tableLine, 0, len(kept))
	anchors := make([]int, 0, len(kept))
	for _, r := range kept {
		ln := tableLine{y: r.y}
		switch len(r.cells) {
		case 2:
			ln.texts[0], ln.has[0] = r.cells[0].text, true
			ln.texts[1], ln.has[1] = r.cells[1].text, true
			anchors = append(anchors, len(lines))
		case 1:
			col := 1
			if math.Abs(r.cells[0].x-col0Start) <= 6 {
				col = 0
				anchors = append(anchors, len(lines))
			}
			ln.texts[col], ln.has[col] = r.cells[0].text, true
		}
		lines = append(lines, ln)
	}
	if len(anchors) < 2 {
		return 0, book.Block{}, false
	}

	groups := make([][2][]string, len(anchors))
	for _, ln := range lines {
		bestIdx := 0
		bestDist := math.Inf(1)
		for ai, idx := range anchors {
			if d := math.Abs(lines[idx].y - ln.y); d < bestDist {
				bestDist = d
				bestIdx = ai
			}
		}
		for k := 0; k < 2; k++ {
			if ln.has[k] {
				groups[bestIdx][k] = append(groups[bestIdx][k], ln.texts[k])
			}
		}
	}

	table := make([][]book.TableCell, 0, len(groups))
	fullRows := 0
	for gi, g := range groups {
		row := make([]book.TableCell, 2)
		for k := 0; k < 2; k++ {
			text := joinCellLines(g[k])
			if text != "" {
				row[k] = book.TableCell{Spans: []book.Span{{Text: text}}}
			}
		}
		if len(row[0].Spans) > 0 && len(row[1].Spans) > 0 {
			fullRows++
		}
		if gi == 0 && len(kept[0].cells) == 2 {
			row[0].Header = true
			row[1].Header = true
		}
		table = append(table, row)
	}
	if len(table) < 3 || fullRows != len(table) {
		return 0, book.Block{}, false
	}
	// Real tables here have a narrow label column and a wider content column.
	// These checks keep large word gaps and page headers from becoming tables.
	headerCol0, headerCol1 := cellRuneLen(table[0][0]), cellRuneLen(table[0][1])
	if headerCol0 > 8 || headerCol1 > 8 {
		return 0, book.Block{}, false
	}
	longSecond := 0
	total0, total1 := 0, 0
	for _, row := range table {
		l0, l1 := cellRuneLen(row[0]), cellRuneLen(row[1])
		total0 += l0
		total1 += l1
		if l0 > 8 {
			return 0, book.Block{}, false
		}
		if l1 >= 10 {
			longSecond++
		}
	}
	if longSecond < 2 || float64(total1) < float64(total0)*1.5 {
		return 0, book.Block{}, false
	}
	return start + len(kept), book.Block{Kind: book.BlockTable, Table: table}, true
}

func cellRuneLen(c book.TableCell) int {
	n := 0
	for _, sp := range c.Spans {
		n += utf8.RuneCountInString(sp.Text)
	}
	return n
}

func joinCellLines(lines []string) string {
	out := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = book.JoinLines(out, line)
	}
	return book.SanitizeText(strings.TrimSpace(out))
}

func codeFromRows(rows []textRow, minX float64) string {
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		fs := r.fontSize
		if fs <= 0 {
			fs = 10
		}
		indent := int(math.Round((r.x - minX) / maxFloat(fs*0.5, 0.5)))
		if indent < 0 {
			indent = 0
		}
		if indent > 40 {
			indent = 40
		}
		lines = append(lines, strings.Repeat(" ", indent)+strings.TrimRight(r.text, " "))
	}
	return book.SanitizeText(strings.Join(lines, "\n"))
}

func plainToBlocks(text string) []book.Block {
	var blocks []book.Block
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); {
		if strings.TrimSpace(lines[i]) == "" {
			i++
			continue
		}
		var para []string
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
			para = append(para, lines[i])
			i++
		}
		joined := ""
		for _, line := range para {
			joined = book.JoinLines(joined, strings.TrimSpace(line))
		}
		joined = book.SanitizeText(strings.TrimSpace(joined))
		if joined != "" {
			blocks = append(blocks, book.Block{Kind: book.BlockParagraph, Spans: []book.Span{{Text: joined}}})
		}
	}
	return blocks
}

func blocksTextLen(blocks []book.Block) int {
	n := 0
	for _, b := range blocks {
		switch b.Kind {
		case book.BlockCode:
			n += len(b.Code)
		case book.BlockParagraph, book.BlockHeading, book.BlockListItem:
			for _, sp := range b.Spans {
				n += len(sp.Text)
			}
		}
	}
	return n
}

func truncateBlocks(blocks []book.Block, limit int) []book.Block {
	used := 0
	out := make([]book.Block, 0, len(blocks))
	for _, b := range blocks {
		size := blocksTextLen([]book.Block{b})
		if used+size > limit {
			break
		}
		used += size
		out = append(out, b)
	}
	out = append(out, book.Block{
		Kind:  book.BlockParagraph,
		Spans: []book.Span{{Text: "（本页文本过长，已截断）"}},
	})
	return out
}

func extractImages(name string, pageCount int) (*book.MapImages, map[int][]book.ImageRef, []string) {
	var warnings []string
	f, err := os.Open(name)
	if err != nil {
		return nil, nil, []string{"无法读取 PDF 图片: " + err.Error()}
	}
	defer f.Close()

	res, err := extractImagesRaw(f)
	if err != nil {
		return nil, nil, []string{"PDF 图片抽取失败: " + err.Error()}
	}
	images := book.NewMapImages()
	refs := make(map[int][]book.ImageRef)
	imgIndexByPage := make(map[int]int)
	total := 0
	seenJPX := false
	// ExtractImagesRaw returns pages in map-iteration order, so use each
	// image's PageNr instead of the slice index.
	for _, pageMap := range res {
		objNrs := make([]int, 0, len(pageMap))
		for objNr := range pageMap {
			objNrs = append(objNrs, objNr)
		}
		sort.Ints(objNrs)
		for _, objNr := range objNrs {
			img := pageMap[objNr]
			pageNo := img.PageNr
			if pageNo <= 0 || pageNo > pageCount {
				continue
			}
			if img.Reader == nil || img.Thumb || img.IsImgMask {
				continue
			}
			if strings.EqualFold(img.FileType, "jpx") {
				if !seenJPX {
					warnings = append(warnings, "部分 PDF 图片使用 JPEG 2000，暂不支持显示")
					seenJPX = true
				}
				continue
			}
			if img.Size > maxImageBytes {
				warnings = append(warnings, fmt.Sprintf("第 %d 页有图片超过大小限制，已跳过", pageNo))
				continue
			}
			data, err := io.ReadAll(io.LimitReader(img.Reader, maxImageBytes+1))
			if err != nil || len(data) == 0 || len(data) > maxImageBytes {
				continue
			}
			mt := book.MediaTypeFor("."+img.FileType, data)
			if mt == "application/octet-stream" || mt == "image/svg+xml" {
				continue
			}
			if total+len(data) > maxTotalImageBytes {
				warnings = append(warnings, "PDF 图片总量超过限制，已跳过后续图片")
				return images, refs, warnings
			}
			total += len(data)
			key := fmt.Sprintf("pdf/img/%d-%d", pageNo, objNr)
			images.Put(key, data)
			imgIndexByPage[pageNo]++
			refs[pageNo] = append(refs[pageNo], book.ImageRef{
				Path:   key,
				Source: img.Name,
				Alt:    fmt.Sprintf("第 %d 页图片 %d", pageNo, imgIndexByPage[pageNo]),
			})
		}
	}
	return images, refs, warnings
}

func extractImagesRaw(f *os.File) (res []map[int]model.Image, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			res, err = nil, fmt.Errorf("%v", rec)
		}
	}()
	return api.ExtractImagesRaw(f, nil, model.NewDefaultConfiguration())
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
