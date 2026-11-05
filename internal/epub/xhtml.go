package epub

import (
	"bytes"
	"path"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

type inlineStyle struct {
	bold, italic, code, underline bool
	link                          string
}

type flowCtx struct {
	quote bool
}

type chapterParser struct {
	b       *Book
	ch      *Chapter
	pending []string
}

func (b *Book) parseChapter(data []byte, index int, href string) (*Chapter, error) {
	reader, err := charset.NewReader(bytes.NewReader(data), "application/xhtml+xml")
	if err != nil {
		reader = bytes.NewReader(data)
	}
	doc, err := html.Parse(reader)
	if err != nil {
		return nil, err
	}
	ch := &Chapter{
		Index:   index,
		Href:    href,
		Anchors: make(map[string]int),
	}
	if title := findElement(doc, "title"); title != nil {
		ch.DocTitle = CollapseSpace(textContent(title))
	}
	root := findElement(doc, "body")
	if root == nil {
		root = doc
	}
	p := &chapterParser{b: b, ch: ch}
	p.parseFlow(root, flowCtx{})
	return ch, nil
}

func (p *chapterParser) parseFlow(n *html.Node, ctx flowCtx) {
	var spans []Span
	var anchors []string
	base := inlineStyle{}
	applyNodeStyle(n, &base)
	flush := func() {
		spans = normalizeSpans(spans)
		if hasVisibleText(spans) {
			p.addBlock(Block{Kind: BlockParagraph, Spans: spans, Quote: ctx.quote}, anchors)
		} else if len(anchors) > 0 {
			p.pending = append(p.pending, anchors...)
		}
		spans = nil
		anchors = nil
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			if text := SanitizeText(c.Data); text != "" {
				spans = append(spans, styledSpan(base, text))
			}
		case html.ElementNode:
			if isBlockElement(c.Data) {
				flush()
				p.handleBlock(c, ctx)
				continue
			}
			if containsBlockElement(c) {
				// Malformed or XML-style self-closing tags (for example
				// <a id="x"/>) can leave an inline element wrapping the rest of
				// the document. Do not flatten that subtree into one paragraph:
				// recurse so block boundaries are preserved.
				flush()
				p.pending = append(p.pending, elementAnchorNames(c)...)
				p.parseFlow(c, ctx)
				continue
			}
			p.collectInline(c, base, &spans, &anchors)
		}
	}
	flush()
}

func isBlockElement(tag string) bool {
	switch tag {
	case "p", "div", "section", "article", "main", "aside", "header", "footer",
		"figure", "figcaption", "h1", "h2", "h3", "h4", "h5", "h6",
		"ul", "ol", "li", "blockquote", "pre", "table", "hr", "img", "svg",
		"nav", "script", "style", "head", "title", "template":
		return true
	}
	return false
}

func containsBlockElement(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if isBlockElement(c.Data) {
			return true
		}
		if containsBlockElement(c) {
			return true
		}
	}
	return false
}

func (p *chapterParser) handleBlock(n *html.Node, ctx flowCtx) {
	ownAnchors := elementAnchorNames(n)
	p.pending = append(p.pending, ownAnchors...)

	switch n.Data {
	case "p", "div", "section", "article", "main", "aside", "header", "footer",
		"figure", "figcaption":
		p.parseFlow(n, ctx)
	case "h1", "h2", "h3", "h4", "h5", "h6":
		spans, anchors := p.collectInlineChildren(n, inlineStyle{})
		spans = normalizeSpans(spans)
		if !hasVisibleText(spans) {
			p.pending = append(p.pending, anchors...)
			return
		}
		level := int(n.Data[1] - '0')
		p.addBlock(Block{Kind: BlockHeading, Level: level, Spans: spans, Quote: ctx.quote}, anchors)
		if p.ch.Heading == "" {
			p.ch.Heading = CollapseSpace(spansText(spans))
		}
	case "blockquote":
		p.parseFlow(n, flowCtx{quote: true})
	case "pre":
		p.parsePre(n)
	case "ul":
		p.parseList(n, ctx, false, 1)
	case "ol":
		p.parseList(n, ctx, true, 1)
	case "li":
		p.parseList(n, ctx, false, 1)
	case "table":
		p.parseTable(n, ctx)
	case "hr":
		p.addBlock(Block{Kind: BlockRule, Quote: ctx.quote}, nil)
	case "img":
		p.addImage(n, ctx.quote)
	case "svg":
		p.parseSVG(n, ctx.quote)
	case "nav", "script", "style", "head", "title", "template":
		// Navigation documents and scripts are not reading content.
		p.pending = p.pending[:len(p.pending)-len(ownAnchors)]
	}
}

func (p *chapterParser) collectInlineChildren(n *html.Node, style inlineStyle) ([]Span, []string) {
	applyNodeStyle(n, &style)
	var spans []Span
	var anchors []string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			spans = append(spans, styledSpan(style, SanitizeText(c.Data)))
		case html.ElementNode:
			p.collectInline(c, style, &spans, &anchors)
		}
	}
	return spans, anchors
}

func (p *chapterParser) collectInline(n *html.Node, style inlineStyle, spans *[]Span, anchors *[]string) {
	*anchors = append(*anchors, elementAnchorNames(n)...)
	switch n.Data {
	case "br":
		*spans = append(*spans, Span{Break: true})
		return
	case "img":
		alt := CollapseSpace(attrValue(n, "alt"))
		if alt == "" {
			alt = "图片"
		}
		*spans = appendStyled(*spans, styledSpan(style, " [图片: "+alt+"] "))
		return
	case "svg", "script", "style", "template":
		return
	case "strong", "b":
		style.bold = true
	case "em", "i", "cite":
		style.italic = true
	case "code", "kbd", "samp":
		style.code = true
	case "u":
		style.underline = true
	case "a":
		style.underline = true
		style.link = attrValue(n, "href")
	}
	applyNodeStyle(n, &style)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			*spans = appendStyled(*spans, styledSpan(style, SanitizeText(c.Data)))
		case html.ElementNode:
			p.collectInline(c, style, spans, anchors)
		}
	}
}

func (p *chapterParser) parseList(n *html.Node, ctx flowCtx, ordered bool, depth int) {
	number := 0
	for li := n.FirstChild; li != nil; li = li.NextSibling {
		if li.Type != html.ElementNode || li.Data != "li" {
			continue
		}
		number++
		liAnchors := elementAnchorNames(li)
		spans, inlineAnchors := p.collectListItemInline(li)
		spans = normalizeSpans(spans)
		if hasVisibleText(spans) {
			p.addBlock(Block{
				Kind:    BlockListItem,
				Spans:   spans,
				Depth:   depth,
				Ordered: ordered,
				Number:  number,
				Quote:   ctx.quote,
			}, append(liAnchors, inlineAnchors...))
		} else {
			p.pending = append(p.pending, append(liAnchors, inlineAnchors...)...)
		}
		for _, nested := range descendantLists(li) {
			childOrdered := nested.Data == "ol"
			p.parseList(nested, ctx, childOrdered, depth+1)
		}
	}
}

func (p *chapterParser) collectListItemInline(li *html.Node) ([]Span, []string) {
	var spans []Span
	var anchors []string
	base := inlineStyle{}
	applyNodeStyle(li, &base)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch c.Type {
			case html.TextNode:
				spans = append(spans, styledSpan(base, SanitizeText(c.Data)))
			case html.ElementNode:
				switch c.Data {
				case "ul", "ol", "script", "style", "template":
					continue
				case "br":
					spans = append(spans, Span{Break: true})
				case "img":
					alt := CollapseSpace(attrValue(c, "alt"))
					if alt == "" {
						alt = "图片"
					}
					spans = appendStyled(spans, styledSpan(base, " [图片: "+alt+"] "))
				default:
					p.collectInline(c, base, &spans, &anchors)
				}
			}
		}
	}
	walk(li)
	return spans, anchors
}

func descendantLists(n *html.Node) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if c.Data == "ul" || c.Data == "ol" {
				out = append(out, c)
				continue
			}
			walk(c)
		}
	}
	walk(n)
	return out
}

func (p *chapterParser) parsePre(n *html.Node) {
	lang := codeLanguage(n)
	var codeNode *html.Node
	var find func(*html.Node)
	find = func(cur *html.Node) {
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "code" && codeNode == nil {
				codeNode = c
				return
			}
			find(c)
		}
	}
	find(n)
	if codeNode != nil {
		if l := codeLanguage(codeNode); l != "" {
			lang = l
		}
	}
	source := strings.Trim(preText(n), "\n")
	if strings.TrimSpace(source) == "" {
		return
	}
	if len(source) > 512<<10 {
		source = source[:512<<10] + "\n…（代码过长，已截断）"
	}
	p.addBlock(Block{Kind: BlockCode, Code: source, Lang: lang}, nil)
}

func codeLanguage(n *html.Node) string {
	lang := strings.TrimSpace(attrValue(n, "data-language"))
	if lang != "" {
		return strings.ToLower(CollapseSpace(lang))
	}
	for _, class := range strings.Fields(attrValue(n, "class")) {
		lower := strings.ToLower(class)
		for _, prefix := range []string{"language-", "lang-", "highlight-source-"} {
			if strings.HasPrefix(lower, prefix) {
				return strings.TrimSpace(strings.TrimPrefix(lower, prefix))
			}
		}
	}
	return ""
}

func (p *chapterParser) parseTable(n *html.Node, ctx flowCtx) {
	var rows [][]TableCell
	var anchors []string
	var walkRows func(*html.Node, bool)
	walkRows = func(cur *html.Node, header bool) {
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.Data {
			case "tr":
				cells := p.parseTableRow(c, header, &anchors)
				if len(cells) > 0 {
					rows = append(rows, cells)
				}
			case "thead":
				walkRows(c, true)
			case "tbody", "tfoot":
				walkRows(c, header)
			case "caption", "colgroup", "col", "script", "style", "template":
				// Captions are handled separately; presentation-only markup is skipped.
			default:
				// Tolerate malformed rows wrapped in extra containers, but never
				// descend into a nested table: its rows belong to the inner table.
				walkRows(c, header)
			}
		}
	}
	walkRows(n, false)

	var caption []Span
	if cn := firstChildElement(n, "caption"); cn != nil {
		var captionAnchors []string
		caption, captionAnchors = p.collectInlineChildren(cn, inlineStyle{})
		caption = normalizeSpans(caption)
		if !hasVisibleText(caption) {
			caption = nil
		} else {
			anchors = append(anchors, captionAnchors...)
		}
	}

	if len(rows) == 0 {
		// A table without any rows still holds readable text in pathological
		// documents; preserve it as a paragraph instead of dropping it.
		spans, cellAnchors := p.collectTableCellContent(n)
		spans = normalizeSpans(spans)
		if hasVisibleText(spans) {
			p.addBlock(Block{Kind: BlockParagraph, Spans: spans, Quote: ctx.quote}, append(anchors, cellAnchors...))
		}
		return
	}
	p.addBlock(Block{Kind: BlockTable, Table: rows, TableCaption: caption, Quote: ctx.quote}, anchors)
}

func (p *chapterParser) parseTableRow(tr *html.Node, header bool, anchors *[]string) []TableCell {
	var cells []TableCell
	for td := tr.FirstChild; td != nil; td = td.NextSibling {
		if td.Type != html.ElementNode || (td.Data != "td" && td.Data != "th") {
			continue
		}
		*anchors = append(*anchors, elementAnchorNames(td)...)
		spans, cellAnchors := p.collectTableCellContent(td)
		*anchors = append(*anchors, cellAnchors...)
		cell := TableCell{
			Spans:   normalizeSpans(spans),
			Header:  header || td.Data == "th",
			ColSpan: tableSpanAttr(td, "colspan"),
			RowSpan: tableSpanAttr(td, "rowspan"),
			Align:   tableCellAlign(td),
		}
		cells = append(cells, cell)
	}
	return cells
}

// collectTableCellContent flattens the block structure of a cell into spans,
// using hard line breaks between paragraphs, list items and <br> elements.
// Inline styling, links and anchors from the original markup are preserved.
func (p *chapterParser) collectTableCellContent(cell *html.Node) ([]Span, []string) {
	var spans []Span
	var anchors []string
	base := inlineStyle{}
	applyNodeStyle(cell, &base)

	var walk func(*html.Node, inlineStyle)
	walk = func(n *html.Node, st inlineStyle) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				if text := SanitizeText(c.Data); text != "" {
					spans = append(spans, styledSpan(st, text))
				}
				continue
			}
			if c.Type != html.ElementNode {
				continue
			}
			anchors = append(anchors, elementAnchorNames(c)...)
			switch c.Data {
			case "br":
				spans = append(spans, Span{Break: true})
			case "ul", "ol":
				p.collectTableCellList(c, st, 0, &spans, &anchors)
			case "table":
				p.collectNestedTableText(c, &spans)
			case "pre":
				text := strings.Trim(preText(c), "\n")
				if text == "" {
					continue
				}
				spans = append(spans, Span{Break: true})
				for i, line := range strings.Split(text, "\n") {
					if i > 0 {
						spans = append(spans, Span{Break: true})
					}
					spans = append(spans, Span{Text: line, Code: true, Italic: st.italic})
				}
				spans = append(spans, Span{Break: true})
			case "p", "div", "section", "article", "main", "aside", "header",
				"footer", "figure", "figcaption", "blockquote":
				childStyle := st
				applyNodeStyle(c, &childStyle)
				if c.Data == "blockquote" {
					childStyle.italic = true
				}
				spans = append(spans, Span{Break: true})
				walk(c, childStyle)
				spans = append(spans, Span{Break: true})
			case "script", "style", "template", "head":
				// Not reading content.
			default:
				p.collectInline(c, st, &spans, &anchors)
			}
		}
	}
	walk(cell, base)
	return spans, anchors
}

func (p *chapterParser) collectTableCellList(n *html.Node, st inlineStyle, depth int, spans *[]Span, anchors *[]string) {
	ordered := n.Data == "ol"
	number := 0
	indent := strings.Repeat("  ", depth)
	for li := n.FirstChild; li != nil; li = li.NextSibling {
		if li.Type != html.ElementNode || li.Data != "li" {
			continue
		}
		number++
		*anchors = append(*anchors, elementAnchorNames(li)...)
		marker := "• "
		if ordered {
			marker = strconv.Itoa(number) + ". "
		}
		*spans = append(*spans, Span{Break: true}, styledSpan(st, indent+marker))
		var nested []*html.Node
		for c := li.FirstChild; c != nil; c = c.NextSibling {
			switch c.Type {
			case html.TextNode:
				if text := SanitizeText(c.Data); text != "" {
					*spans = append(*spans, styledSpan(st, text))
				}
			case html.ElementNode:
				switch c.Data {
				case "ul", "ol":
					nested = append(nested, c)
				case "br":
					*spans = append(*spans, Span{Break: true})
				case "p", "div":
					*spans = append(*spans, Span{Break: true})
					p.collectInline(c, st, spans, anchors)
					*spans = append(*spans, Span{Break: true})
				case "script", "style", "template":
				default:
					p.collectInline(c, st, spans, anchors)
				}
			}
		}
		for _, sub := range nested {
			p.collectTableCellList(sub, st, depth+1, spans, anchors)
		}
	}
	*spans = append(*spans, Span{Break: true})
}

// collectNestedTableText turns a table nested inside a cell into readable
// one-row-per-line text, since the terminal cell cannot host another grid.
func (p *chapterParser) collectNestedTableText(table *html.Node, spans *[]Span) {
	var rows [][]string
	var walkRows func(*html.Node)
	walkRows = func(cur *html.Node) {
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.Data {
			case "tr":
				var cells []string
				for td := c.FirstChild; td != nil; td = td.NextSibling {
					if td.Type != html.ElementNode || (td.Data != "td" && td.Data != "th") {
						continue
					}
					if text := CollapseSpace(textContent(td)); text != "" {
						cells = append(cells, text)
					}
				}
				if len(cells) > 0 {
					rows = append(rows, cells)
				}
			case "thead", "tbody", "tfoot":
				walkRows(c)
			}
		}
	}
	walkRows(table)
	if len(rows) == 0 {
		return
	}
	*spans = append(*spans, Span{Break: true})
	for _, row := range rows {
		*spans = append(*spans, Span{Text: strings.Join(row, " │ ")})
		*spans = append(*spans, Span{Break: true})
	}
}

func tableSpanAttr(n *html.Node, key string) int {
	v := strings.TrimSpace(attrValue(n, key))
	if v == "" {
		return 1
	}
	nv, err := strconv.Atoi(v)
	if err != nil || nv < 1 {
		return 1
	}
	if nv > 100 {
		nv = 100
	}
	return nv
}

func tableCellAlign(n *html.Node) string {
	align := strings.ToLower(strings.TrimSpace(attrValue(n, "align")))
	if align == "" {
		css := strings.ToLower(attrValue(n, "style"))
		for _, decl := range strings.Split(css, ";") {
			key, val, ok := strings.Cut(decl, ":")
			if !ok || strings.TrimSpace(key) != "text-align" {
				continue
			}
			align = strings.TrimSpace(strings.ReplaceAll(val, "!important", ""))
			break
		}
	}
	switch align {
	case "center", "middle":
		return "center"
	case "right", "end":
		return "right"
	case "left", "start", "justify":
		return "left"
	}
	return ""
}

func (p *chapterParser) addImage(n *html.Node, quote bool) {
	src := attrValue(n, "src")
	if src == "" {
		src = attrValue(n, "data-src")
	}
	alt := CollapseSpace(attrValue(n, "alt"))
	title := CollapseSpace(attrValue(n, "title"))
	ref := p.imageRef(src, alt, title)
	p.addBlock(Block{Kind: BlockImage, Image: ref, Quote: quote}, nil)
}

func (p *chapterParser) parseSVG(n *html.Node, quote bool) {
	var image *html.Node
	var find func(*html.Node)
	find = func(cur *html.Node) {
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "image" && image == nil {
				image = c
				return
			}
			find(c)
		}
	}
	find(n)
	alt := "SVG 图片"
	if t := findElement(n, "title"); t != nil {
		if s := CollapseSpace(textContent(t)); s != "" {
			alt = s
		}
	}
	if image != nil {
		src := attrValue(image, "href")
		if src == "" {
			src = attrValue(image, "xlink:href")
		}
		p.addBlock(Block{Kind: BlockImage, Image: p.imageRef(src, alt, ""), Quote: quote}, nil)
		return
	}
	p.addBlock(Block{Kind: BlockImage, Image: &ImageRef{Alt: alt}, Quote: quote}, nil)
}

func (p *chapterParser) imageRef(src, alt, title string) *ImageRef {
	ref := &ImageRef{Source: src, Alt: alt, Title: title}
	if src == "" {
		return ref
	}
	if strings.HasPrefix(strings.ToLower(src), "data:") ||
		strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") ||
		strings.HasPrefix(src, "//") {
		ref.External = true
		return ref
	}
	zp, _, external, err := p.b.resolveHref(p.ch.Href, src)
	if err != nil || external {
		ref.External = external
		return ref
	}
	ref.Path = zp
	if ref.Alt == "" {
		ref.Alt = strings.TrimSuffix(path.Base(zp), path.Ext(zp))
	}
	return ref
}
