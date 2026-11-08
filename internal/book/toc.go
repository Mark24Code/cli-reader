package book

import (
	"fmt"
	"path"
	"strings"
)

// EnsureTOC resolves TOC chapter indices, fills in chapters that are missing
// from the table of contents, and assigns chapter titles.
func (b *Book) EnsureTOC() {
	if len(b.TOC) == 0 {
		b.TOC = b.fallbackTOC()
	} else {
		b.TOC = b.resolveTOC(b.TOC)
		b.TOC = b.fillMissingChapters(b.TOC)
	}
	b.assignChapterTitles()
}

func (b *Book) resolveTOC(entries []TOCEntry) []TOCEntry {
	out := make([]TOCEntry, 0, len(entries))
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
		out = append(out, TOCEntry{
			Title:   ChapterFallbackTitle(&b.Chapters[i], i),
			Href:    b.Chapters[i].Href,
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
			Title:   ChapterFallbackTitle(&b.Chapters[i], i),
			Href:    b.Chapters[i].Href,
			Chapter: i,
		})
	}
	return entries
}

func (b *Book) assignChapterTitles() {
	// Title priority: TOC, document <title>, first heading, filename.
	tocTitles := make(map[int]string)
	for _, entry := range b.TOC {
		if entry.Chapter >= 0 {
			if _, ok := tocTitles[entry.Chapter]; !ok && entry.Title != "" {
				tocTitles[entry.Chapter] = entry.Title
			}
		}
	}
	for i := range b.Chapters {
		ch := &b.Chapters[i]
		ch.Index = i
		switch {
		case tocTitles[i] != "":
			ch.Title = tocTitles[i]
		case ch.Heading != "":
			ch.Title = ch.Heading
		case ch.DocTitle != "":
			ch.Title = ch.DocTitle
		default:
			base := path.Base(ch.Href)
			ch.Title = strings.TrimSuffix(base, path.Ext(base))
			if ch.Title == "" || ch.Title == "." {
				ch.Title = fmt.Sprintf("第 %d 节", i+1)
			}
		}
	}
}

// ChapterFallbackTitle derives a display title for a chapter without a TOC
// entry.
func ChapterFallbackTitle(ch *Chapter, index int) string {
	switch {
	case ch.Title != "":
		return ch.Title
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

// TOCEntriesFromHeadings builds TOC entries for headings inside one chapter.
func TOCEntriesFromHeadings(chapter int, headings []HeadingRef) []TOCEntry {
	out := make([]TOCEntry, 0, len(headings))
	for _, h := range headings {
		title := strings.TrimSpace(h.Title)
		if title == "" {
			continue
		}
		entry := TOCEntry{Title: title, Chapter: chapter, Fragment: h.Anchor}
		if h.Level > 1 {
			entry.Depth = h.Level - 1
		}
		out = append(out, entry)
	}
	return out
}
