package epub

import (
	"bytes"
	"fmt"
	"path"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"cli-reader/internal/book"
	"cli-reader/internal/htmlimport"
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

func (l *loader) parseNavTOC(data []byte, navPath string) ([]book.TOCEntry, error) {
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
		t := strings.ToLower(htmlimport.Attr(nav, "type") + " " + htmlimport.Attr(nav, "epub:type"))
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
	list := htmlimport.FirstChildElement(chosen, "ol", "ul")
	if list == nil {
		return nil, fmt.Errorf("nav 中没有目录列表")
	}
	var entries []book.TOCEntry
	l.walkNavList(list, navPath, 0, &entries)
	return entries, nil
}

func navHasList(n *html.Node) bool {
	return htmlimport.FirstChildElement(n, "ol", "ul") != nil
}

func (l *loader) walkNavList(list *html.Node, navPath string, depth int, out *[]book.TOCEntry) {
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
			title := book.CollapseSpace(htmlimport.TextContent(anchor))
			raw := htmlimport.Attr(anchor, "href")
			if title == "" {
				title = book.CollapseSpace(raw)
			}
			if raw != "" {
				zp, frag, _, err := l.resolveHref(navPath, raw)
				entry := book.TOCEntry{Title: title, Fragment: frag, Chapter: -1, Depth: depth}
				if err == nil {
					entry.Href = zp
				}
				*out = append(*out, entry)
			}
		}
		for _, n := range nested {
			l.walkNavList(n, navPath, depth+1, out)
		}
	}
}

func (l *loader) parseNCXTOC(data []byte, ncxPath string) ([]book.TOCEntry, error) {
	var doc ncxDoc
	if err := decodeXML(data, &doc); err != nil {
		return nil, err
	}
	var out []book.TOCEntry
	var walk func(points []ncxNavPoint, depth int)
	walk = func(points []ncxNavPoint, depth int) {
		for _, np := range points {
			title := book.CollapseSpace(np.Label.Text)
			raw := strings.TrimSpace(book.SanitizeText(np.Content.Src))
			if raw != "" {
				if title == "" {
					title = strings.TrimSuffix(path.Base(raw), path.Ext(raw))
				}
				zp, frag, _, err := l.resolveHref(ncxPath, raw)
				entry := book.TOCEntry{Title: title, Fragment: frag, Chapter: -1, Depth: depth}
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
