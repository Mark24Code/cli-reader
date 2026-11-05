package epub

import (
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

func (p *chapterParser) addBlock(block Block, extraAnchors []string) int {
	idx := len(p.ch.Blocks)
	p.ch.Blocks = append(p.ch.Blocks, block)
	anchors := append(p.pending, extraAnchors...)
	p.pending = nil
	for _, name := range anchors {
		name = strings.TrimSpace(SanitizeText(name))
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
	if id := strings.TrimSpace(attrValue(n, "id")); id != "" {
		names = append(names, id)
	}
	if id := strings.TrimSpace(attrValue(n, "xml:id")); id != "" {
		names = append(names, id)
	}
	if n.Data == "a" {
		if name := strings.TrimSpace(attrValue(n, "name")); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func sameStyle(a, b Span) bool {
	return a.Bold == b.Bold && a.Italic == b.Italic && a.Code == b.Code &&
		a.Underline == b.Underline && a.Link == b.Link
}

func appendStyled(spans []Span, sp Span) []Span {
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
func normalizeSpans(in []Span) []Span {
	var out []Span
	pendingSpace := false
	appendRune := func(style Span, r rune) {
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
		for _, r := range SanitizeText(sp.Text) {
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

func hasVisibleText(spans []Span) bool {
	return strings.TrimSpace(spansText(spans)) != ""
}

func spansText(spans []Span) string {
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
				b.WriteString(SanitizeText(c.Data))
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
