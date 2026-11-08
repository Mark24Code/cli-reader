package book

import "fmt"

// ChooseHeadingLevel returns the shallowest heading level with at least two
// entries, or 0 when the document has no useful chapter headings.
func ChooseHeadingLevel(headings []HeadingRef) int {
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

// ChunkChapter splits a large chapter without useful headings into fixed-size
// sections. It keeps anchors pointing at the right chunk and is used by large
// HTML/Markdown/MOBI documents.
func ChunkChapter(ch *Chapter, maxBlocks int, baseHref string) []Chapter {
	if ch == nil || maxBlocks <= 0 || len(ch.Blocks) <= maxBlocks {
		return nil
	}
	var chapters []Chapter
	for start := 0; start < len(ch.Blocks); start += maxBlocks {
		end := start + maxBlocks
		if end > len(ch.Blocks) {
			end = len(ch.Blocks)
		}
		i := len(chapters)
		title := fmt.Sprintf("第 %d 节", i+1)
		href := fmt.Sprintf("%s#part%d", baseHref, i+1)
		if i == 0 {
			href = baseHref
		}
		chapters = append(chapters, Chapter{
			Index:    i,
			Href:     href,
			Title:    title,
			Heading:  title,
			DocTitle: ch.DocTitle,
			PageMode: ch.PageMode,
			Blocks:   append([]Block(nil), ch.Blocks[start:end]...),
			Anchors:  make(map[string]int),
		})
	}
	for name, blockIdx := range ch.Anchors {
		for i := range chapters {
			start := i * maxBlocks
			if blockIdx >= start && blockIdx < start+maxBlocks {
				chapters[i].Anchors[name] = blockIdx - start
				break
			}
		}
	}
	return chapters
}

// ChunkChapterPart materializes only one fixed-size chunk, avoiding a copy of
// every chunk when a lazy loader needs a single chapter.
func ChunkChapterPart(ch *Chapter, maxBlocks, index int, baseHref string) *Chapter {
	if ch == nil || maxBlocks <= 0 || index < 0 {
		return nil
	}
	start := index * maxBlocks
	if start >= len(ch.Blocks) {
		return nil
	}
	end := start + maxBlocks
	if end > len(ch.Blocks) {
		end = len(ch.Blocks)
	}
	title := fmt.Sprintf("第 %d 节", index+1)
	href := fmt.Sprintf("%s#part%d", baseHref, index+1)
	if index == 0 {
		href = baseHref
	}
	out := &Chapter{
		Index:    index,
		Href:     href,
		Title:    title,
		Heading:  title,
		DocTitle: ch.DocTitle,
		PageMode: ch.PageMode,
		Blocks:   append([]Block(nil), ch.Blocks[start:end]...),
		Anchors:  make(map[string]int),
	}
	for name, blockIdx := range ch.Anchors {
		if blockIdx >= start && blockIdx < end {
			out.Anchors[name] = blockIdx - start
		}
	}
	return out
}

// SplitPartByHeadings materializes only one heading-derived chapter.
func SplitPartByHeadings(ch *Chapter, headings []HeadingRef, level, index int, baseHref string) *Chapter {
	if ch == nil || level <= 0 || index < 0 {
		return nil
	}
	var splits []HeadingRef
	for _, h := range headings {
		if h.Level <= level {
			splits = append(splits, h)
		}
	}
	if len(splits) < 2 {
		return nil
	}
	type part struct {
		start, end int
		title      string
		heading    *HeadingRef
	}
	var parts []part
	if splits[0].Block > 0 {
		parts = append(parts, part{start: 0, end: splits[0].Block, title: "开篇"})
	}
	for i, h := range splits {
		end := len(ch.Blocks)
		if i+1 < len(splits) {
			end = splits[i+1].Block
		}
		hc := h
		parts = append(parts, part{start: h.Block, end: end, title: h.Title, heading: &hc})
	}
	if index >= len(parts) {
		return nil
	}
	p := parts[index]
	href := fmt.Sprintf("%s#part%d", baseHref, index+1)
	if index == 0 && p.heading == nil {
		href = baseHref
	}
	out := &Chapter{
		Index:    index,
		Href:     href,
		Title:    p.title,
		Heading:  p.title,
		DocTitle: ch.DocTitle,
		PageMode: ch.PageMode,
		Blocks:   append([]Block(nil), ch.Blocks[p.start:p.end]...),
		Anchors:  make(map[string]int),
	}
	for name, blockIdx := range ch.Anchors {
		if blockIdx >= p.start && blockIdx < p.end {
			out.Anchors[name] = blockIdx - p.start
		}
	}
	return out
}

// SplitByHeadings divides a single parsed document into chapters at headings of
// the given level. Blocks are copied into the resulting chapters and anchors are
// remapped. It is used for large HTML/Markdown files and MOBI documents.
func SplitByHeadings(ch *Chapter, headings []HeadingRef, level int, baseHref string) ([]Chapter, []TOCEntry) {
	if ch == nil || level <= 0 {
		return nil, nil
	}
	var splits []HeadingRef
	for _, h := range headings {
		if h.Level <= level {
			splits = append(splits, h)
		}
	}
	if len(splits) < 2 {
		return nil, nil
	}

	type part struct {
		start   int
		end     int
		title   string
		heading *HeadingRef
	}
	var parts []part
	if splits[0].Block > 0 {
		parts = append(parts, part{start: 0, end: splits[0].Block, title: "开篇"})
	}
	for i, h := range splits {
		end := len(ch.Blocks)
		if i+1 < len(splits) {
			end = splits[i+1].Block
		}
		hc := h
		parts = append(parts, part{start: h.Block, end: end, title: h.Title, heading: &hc})
	}

	chapters := make([]Chapter, len(parts))
	for i, p := range parts {
		href := fmt.Sprintf("%s#part%d", baseHref, i+1)
		if i == 0 && p.heading == nil {
			href = baseHref
		}
		blocks := append([]Block(nil), ch.Blocks[p.start:p.end]...)
		chapters[i] = Chapter{
			Index:    i,
			Href:     href,
			Title:    p.title,
			Heading:  p.title,
			DocTitle: ch.DocTitle,
			PageMode: ch.PageMode,
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

	var toc []TOCEntry
	if parts[0].heading == nil {
		toc = append(toc, TOCEntry{Title: parts[0].title, Chapter: 0})
	}
	for _, h := range headings {
		chapter, ok := anchorChapter[h.Anchor]
		if !ok {
			continue
		}
		depth := h.Level - 1
		if depth < 0 {
			depth = 0
		}
		toc = append(toc, TOCEntry{
			Title:    h.Title,
			Chapter:  chapter,
			Fragment: h.Anchor,
			Depth:    depth,
		})
	}
	return chapters, toc
}
