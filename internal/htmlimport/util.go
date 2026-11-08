package htmlimport

import (
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"

	"cli-reader/internal/book"
)

func styledSpan(style inlineStyle, text string) book.Span {
	return book.Span{
		Text:      text,
		Bold:      style.bold,
		Italic:    style.italic,
		Code:      style.code,
		Underline: style.underline,
		Link:      style.link,
	}
}

// applyNodeStyle merges the small subset of inline CSS that maps directly to
// terminal attributes. Explicit normal/lighter values override inherited
// emphasis, matching CSS cascade behaviour for nested spans.
func applyNodeStyle(n *html.Node, style *inlineStyle) {
	if n == nil || style == nil {
		return
	}
	css := strings.ToLower(Attr(n, "style"))
	if css == "" {
		return
	}
	for _, decl := range strings.Split(css, ";") {
		key, val, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(strings.ReplaceAll(val, "!important", ""))
		switch key {
		case "font-weight":
			switch {
			case val == "bold" || val == "bolder":
				style.bold = true
			case val == "normal" || val == "lighter":
				style.bold = false
			default:
				if weight, err := strconv.Atoi(val); err == nil {
					style.bold = weight >= 600
				}
			}
		case "font-style":
			if strings.HasPrefix(val, "italic") || strings.HasPrefix(val, "oblique") {
				style.italic = true
			} else if val == "normal" {
				style.italic = false
			}
		case "text-decoration", "text-decoration-line":
			if strings.Contains(val, "underline") {
				style.underline = true
			} else if strings.Contains(val, "none") {
				style.underline = false
			}
		}
	}
}

func (p *chapterParser) addBlock(block book.Block, extraAnchors []string) int {
	idx := len(p.ch.Blocks)
	p.ch.Blocks = append(p.ch.Blocks, block)
	anchors := append(p.pending, extraAnchors...)
	p.pending = nil
	for _, name := range anchors {
		name = strings.TrimSpace(book.SanitizeText(name))
		if name == "" {
			continue
		}
		if _, exists := p.ch.Anchors[name]; !exists {
			p.ch.Anchors[name] = idx
		}
	}
	return idx
}

func elementAnchorNames(n *html.Node) []string {
	if n == nil || n.Type != html.ElementNode {
		return nil
	}
	var names []string
	if id := strings.TrimSpace(Attr(n, "id")); id != "" {
		names = append(names, id)
	}
	if id := strings.TrimSpace(Attr(n, "xml:id")); id != "" {
		names = append(names, id)
	}
	if n.Data == "a" {
		if name := strings.TrimSpace(Attr(n, "name")); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func sameStyle(a, b book.Span) bool {
	return a.Bold == b.Bold && a.Italic == b.Italic && a.Code == b.Code &&
		a.Underline == b.Underline && a.Link == b.Link
}

func appendStyled(spans []book.Span, sp book.Span) []book.Span {
	if sp.Text == "" {
		return spans
	}
	if len(spans) > 0 && sameStyle(spans[len(spans)-1], sp) {
		spans[len(spans)-1].Text += sp.Text
		return spans
	}
	return append(spans, sp)
}

// normalizeSpans collapses whitespace, trims line edges and merges adjacent
// runs with identical styling. Newlines are preserved as hard breaks.
func normalizeSpans(in []book.Span) []book.Span {
	var out []book.Span
	pendingSpace := false
	appendRune := func(style book.Span, r rune) {
		style.Text = string(r)
		if len(out) > 0 {
			last := &out[len(out)-1]
			if sameStyle(*last, style) {
				last.Text += string(r)
				return
			}
		}
		out = append(out, style)
	}
	trimTrailingSpace := func() {
		for len(out) > 0 {
			last := &out[len(out)-1]
			last.Text = strings.TrimRight(last.Text, " \t")
			if last.Text == "" {
				out = out[:len(out)-1]
				continue
			}
			break
		}
	}
	for _, sp := range in {
		if sp.Break {
			pendingSpace = false
			trimTrailingSpace()
			if len(out) > 0 {
				appendRune(sp, '\n')
			}
			continue
		}
		style := sp
		style.Text = ""
		for _, r := range book.SanitizeText(sp.Text) {
			switch {
			case unicode.IsSpace(r):
				pendingSpace = true
			default:
				if pendingSpace && len(out) > 0 && !strings.HasSuffix(out[len(out)-1].Text, "\n") {
					appendRune(style, ' ')
				}
				pendingSpace = false
				appendRune(style, r)
			}
		}
	}
	for len(out) > 0 {
		last := &out[len(out)-1]
		last.Text = strings.TrimRight(last.Text, " \t\n")
		if last.Text == "" {
			out = out[:len(out)-1]
			continue
		}
		break
	}
	return out
}

func hasVisibleText(spans []book.Span) bool {
	return strings.TrimSpace(spansText(spans)) != ""
}

func spansText(spans []book.Span) string {
	var b strings.Builder
	for _, sp := range spans {
		b.WriteString(sp.Text)
	}
	return b.String()
}

func findElement(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElement(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func preText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			switch c.Type {
			case html.TextNode:
				b.WriteString(book.SanitizeText(c.Data))
			case html.ElementNode:
				switch c.Data {
				case "br":
					b.WriteByte('\n')
				case "script", "style":
					continue
				default:
					walk(c)
				}
			}
		}
	}
	walk(n)
	return b.String()
}

// Attr returns an attribute value case-insensitively. The epub: prefix is
// accepted for attributes that some HTML parsers preserve and others strip.
func Attr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) || strings.EqualFold(a.Key, strings.TrimPrefix(key, "epub:")) {
			return a.Val
		}
	}
	return ""
}

// FirstChildElement returns the first direct child with one of the tags.
func FirstChildElement(n *html.Node, tags ...string) *html.Node {
	if n == nil {
		return nil
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		for _, tag := range tags {
			if c.Data == tag {
				return c
			}
		}
	}
	return nil
}

// TextContent returns the visible text of a node, skipping scripts/styles.
func TextContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
		case html.ElementNode:
			if n.Data == "script" || n.Data == "style" {
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// firstChildElement is the internal alias kept for parser readability.
func firstChildElement(n *html.Node, tags ...string) *html.Node {
	return FirstChildElement(n, tags...)
}

func textContent(n *html.Node) string { return TextContent(n) }

func attrValue(n *html.Node, key string) string { return Attr(n, key) }
