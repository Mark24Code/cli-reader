package epub

import (
	"bytes"
	"fmt"
	"path"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

type ncxDoc struct {
	NavMap ncxNavMap `xml:"navMap"`
}

type ncxNavMap struct {
	Points []ncxNavPoint `xml:"navPoint"`
}

type ncxNavPoint struct {
	Label struct {
		Text string `xml:"text"`
	} `xml:"navLabel"`
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Children []ncxNavPoint `xml:"navPoint"`
}

func (b *Book) parseNavTOC(data []byte, navPath string) ([]TOCEntry, error) {
	reader, err := charset.NewReader(bytes.NewReader(data), "application/xhtml+xml")
	if err != nil {
		reader = bytes.NewReader(data)
	}
	doc, err := html.Parse(reader)
	if err != nil {
		return nil, err
	}

	var navs []*html.Node
	var walkNavs func(*html.Node)
	walkNavs = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "nav" {
			navs = append(navs, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkNavs(c)
		}
	}
	walkNavs(doc)
	if len(navs) == 0 {
		return nil, fmt.Errorf("nav 文档中没有 <nav>")
	}

	chosen := navs[0]
	for _, nav := range navs {
		t := strings.ToLower(attrValue(nav, "type") + " " + attrValue(nav, "epub:type"))
		if strings.Contains(t, "toc") {
			chosen = nav
			break
		}
	}
	if !navHasList(chosen) {
		for _, nav := range navs {
			if navHasList(nav) {
				chosen = nav
				break
			}
		}
	}
	list := firstChildElement(chosen, "ol", "ul")
	if list == nil {
		return nil, fmt.Errorf("nav 中没有目录列表")
	}
	var entries []TOCEntry
	b.walkNavList(list, navPath, 0, &entries)
	return entries, nil
}

func navHasList(n *html.Node) bool {
	return firstChildElement(n, "ol", "ul") != nil
}

func (b *Book) walkNavList(list *html.Node, navPath string, depth int, out *[]TOCEntry) {
	for li := list.FirstChild; li != nil; li = li.NextSibling {
		if li.Type != html.ElementNode || li.Data != "li" {
			continue
		}
		var anchor *html.Node
		var nested []*html.Node
		for c := li.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.Data {
			case "a":
				if anchor == nil {
					anchor = c
				}
			case "ol", "ul":
				nested = append(nested, c)
			}
		}
		if anchor != nil {
			title := CollapseSpace(textContent(anchor))
			raw := attrValue(anchor, "href")
			if title == "" {
				title = CollapseSpace(raw)
			}
			if raw != "" {
				zp, frag, _, err := b.resolveHref(navPath, raw)
				entry := TOCEntry{Title: title, Fragment: frag, Chapter: -1, Depth: depth}
				if err == nil {
					entry.Href = zp
				}
				*out = append(*out, entry)
			}
		}
		for _, n := range nested {
			b.walkNavList(n, navPath, depth+1, out)
		}
	}
}

func (b *Book) parseNCXTOC(data []byte, ncxPath string) ([]TOCEntry, error) {
	var doc ncxDoc
	if err := decodeXML(data, &doc); err != nil {
		return nil, err
	}
	var out []TOCEntry
	var walk func(points []ncxNavPoint, depth int)
	walk = func(points []ncxNavPoint, depth int) {
		for _, np := range points {
			title := CollapseSpace(np.Label.Text)
			raw := strings.TrimSpace(SanitizeText(np.Content.Src))
			if raw != "" {
				if title == "" {
					title = strings.TrimSuffix(path.Base(raw), path.Ext(raw))
				}
				zp, frag, _, err := b.resolveHref(ncxPath, raw)
				entry := TOCEntry{Title: title, Fragment: frag, Chapter: -1, Depth: depth}
				if err == nil {
					entry.Href = zp
				}
				out = append(out, entry)
			}
			walk(np.Children, depth+1)
		}
	}
	walk(doc.NavMap.Points, 0)
	return out, nil
}

func (b *Book) resolveTOC(entries []TOCEntry) []TOCEntry {
	out := entries[:0]
	for _, e := range entries {
		if e.Href != "" {
			e.Chapter = b.ChapterByHref(e.Href)
		}
		if e.Chapter < 0 {
			e.Chapter = -1
		}
		out = append(out, e)
	}
	return out
}

func (b *Book) fallbackTOC() []TOCEntry {
	out := make([]TOCEntry, 0, len(b.Chapters))
	for i := range b.Chapters {
		ch := &b.Chapters[i]
		out = append(out, TOCEntry{
			Title:   chapterFallbackTitle(ch, i),
			Href:    ch.Href,
			Chapter: i,
		})
	}
	return out
}

func (b *Book) fillMissingChapters(entries []TOCEntry) []TOCEntry {
	seen := make(map[int]bool)
	for _, e := range entries {
		if e.Chapter >= 0 {
			seen[e.Chapter] = true
		}
	}
	for i := range b.Chapters {
		if seen[i] {
			continue
		}
		entries = append(entries, TOCEntry{
			Title:   chapterFallbackTitle(&b.Chapters[i], i),
			Href:    b.Chapters[i].Href,
			Chapter: i,
		})
	}
	return entries
}

func chapterFallbackTitle(ch *Chapter, index int) string {
	switch {
	case ch.Heading != "":
		return ch.Heading
	case ch.DocTitle != "":
		return ch.DocTitle
	case path.Base(ch.Href) != "." && path.Base(ch.Href) != "":
		base := path.Base(ch.Href)
		return strings.TrimSuffix(base, path.Ext(base))
	default:
		return fmt.Sprintf("第 %d 节", index+1)
	}
}

func attrValue(n *html.Node, key string) string {
	for _, a := range n.Attr {
		// x/net/html keeps foreign attributes such as epub:type with their
		// prefix in some documents and strips it in others; accept both.
		if strings.EqualFold(a.Key, key) || strings.EqualFold(a.Key, strings.TrimPrefix(key, "epub:")) {
			return a.Val
		}
	}
	return ""
}

func firstChildElement(n *html.Node, tags ...string) *html.Node {
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

func textContent(n *html.Node) string {
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
