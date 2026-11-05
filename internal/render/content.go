package render

import (
	"fmt"
	"image"
	"math"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"

	"claude-epub-reader/internal/epub"
	"claude-epub-reader/internal/theme"
)

// ImageProvider supplies embedded image bytes.
type ImageProvider interface {
	ImageBytes(path string) ([]byte, string, error)
}

// ImageSpan records where an image occupies the rendered chapter.
type ImageSpan struct {
	Index     int
	StartLine int
	EndLine   int // exclusive
	Ref       epub.ImageRef
	PixelW    int
	PixelH    int
}

// LinkSpan marks a clickable link region on a single rendered line. Columns
// are zero-based display cells and EndCol is exclusive.
type LinkSpan struct {
	Line     int
	StartCol int
	EndCol   int
	Href     string
}

// RenderedChapter is the final, width-dependent terminal representation of a
// chapter. All Lines are guaranteed not to exceed Width display cells.
type RenderedChapter struct {
	ChapterIndex int
	Width        int
	Lines        []string
	BlockStart   []int
	Images       []ImageSpan
	Links        []LinkSpan
	Anchors      map[string]int
}

// Options controls image sizes during chapter rendering.
type Options struct {
	MaxImageCols int
	MaxImageRows int
}

// Renderer owns width-independent image caches.
type Renderer struct {
	images ImageProvider

	mu          sync.Mutex
	cache       map[string]image.Image
	cachePixels int64
	thumbs      map[string]thumbnail
}

func NewRenderer(images ImageProvider) *Renderer {
	return &Renderer{
		images: images,
		cache:  make(map[string]image.Image),
		thumbs: make(map[string]thumbnail),
	}
}

// renderedBlock is one block's output plus per-line link runs. Links is
// parallel to Lines.
type renderedBlock struct {
	Lines []string
	Links [][]lineLink
}

func singleBlock(lines []string) renderedBlock {
	return renderedBlock{Lines: lines}
}

func (r *Renderer) RenderChapter(ch *epub.Chapter, width int, opts Options) *RenderedChapter {
	if width < 20 {
		width = 20
	}
	if opts.MaxImageCols <= 0 {
		opts.MaxImageCols = 48
	}
	if opts.MaxImageRows <= 0 {
		opts.MaxImageRows = 14
	}
	rc := &RenderedChapter{
		ChapterIndex: ch.Index,
		Width:        width,
		BlockStart:   make([]int, len(ch.Blocks)),
		Anchors:      make(map[string]int),
	}

	margin := "  "
	rc.Lines = append(rc.Lines, margin+Style{FG: theme.Accent, Bold: true}.Render("✻ Claude")+
		Style{FG: theme.Muted}.Render("  ·  正在阅读"))
	rc.Lines = append(rc.Lines, "")
	if ch.Heading == "" && ch.Title != "" {
		titleSpans := []epub.Span{{Text: ch.Title, Bold: true}}
		titleLines := wrapStyledLines(titleSpans, width, Style{FG: theme.Accent, Bold: true}, margin, margin)
		rc.Lines = append(rc.Lines, titleLines...)
		rc.Lines = append(rc.Lines, "")
	}

	imageCount := 0
	for i := range ch.Blocks {
		if ch.Blocks[i].Kind == epub.BlockImage {
			imageCount++
		}
	}
	imageIndex := 0
	prevKind := epub.BlockParagraph
	haveBlock := false
	for i := range ch.Blocks {
		block := &ch.Blocks[i]
		consecutiveList := haveBlock && prevKind == epub.BlockListItem && block.Kind == epub.BlockListItem
		if !consecutiveList {
			addBlankLine(&rc.Lines)
		}
		rc.BlockStart[i] = len(rc.Lines)
		prevKind = block.Kind
		haveBlock = true
		var rendered renderedBlock
		switch block.Kind {
		case epub.BlockHeading:
			rendered = r.renderHeading(block, width)
		case epub.BlockParagraph:
			rendered = r.renderParagraph(block, width)
		case epub.BlockListItem:
			rendered = r.renderListItem(block, width)
		case epub.BlockCode:
			rendered = singleBlock(renderCodeBlock(block.Code, block.Lang, width))
		case epub.BlockImage:
			imageIndex++
			rendered = singleBlock(r.renderImage(block, width, opts, imageIndex, imageCount, len(rc.Lines), &rc.Images))
		case epub.BlockRule:
			rendered = singleBlock(renderRule(width))
		case epub.BlockTable:
			rendered = r.renderTable(block, width)
		}
		lineBase := len(rc.Lines)
		for k, line := range rendered.Lines {
			if visibleWidth(line) > width {
				line = ansi.Truncate(line, width, "")
			}
			rc.Lines = append(rc.Lines, line)
			if k >= len(rendered.Links) {
				continue
			}
			lineW := visibleWidth(line)
			for _, link := range rendered.Links[k] {
				start, end := link.start, link.end
				if start < 0 {
					start = 0
				}
				if end > lineW {
					end = lineW
				}
				if end > start {
					rc.Links = append(rc.Links, LinkSpan{
						Line:     lineBase + k,
						StartCol: start,
						EndCol:   end,
						Href:     link.href,
					})
				}
			}
		}
	}
	for len(rc.Lines) > 0 && strings.TrimSpace(ansi.Strip(rc.Lines[len(rc.Lines)-1])) == "" {
		rc.Lines = rc.Lines[:len(rc.Lines)-1]
	}
	if len(rc.Lines) == 0 {
		rc.Lines = append(rc.Lines, margin+Style{FG: theme.Muted}.Render("（本章没有可显示的正文）"))
	}

	for name, blockIndex := range ch.Anchors {
		if blockIndex >= 0 && blockIndex < len(rc.BlockStart) {
			rc.Anchors[name] = rc.BlockStart[blockIndex]
		}
	}
	return rc
}

func addBlankLine(lines *[]string) {
	if len(*lines) == 0 {
		return
	}
	last := ansi.Strip((*lines)[len(*lines)-1])
	if strings.TrimSpace(last) == "" {
		return
	}
	*lines = append(*lines, "")
}

func (r *Renderer) renderHeading(b *epub.Block, width int) renderedBlock {
	base := Style{FG: theme.Accent, Bold: true}
	if b.Level > 3 {
		base = Style{Bold: true}
	}
	lines, links := wrapStyledLinesWithLinks(b.Spans, width, base, "  ", "  ")
	return renderedBlock{Lines: lines, Links: links}
}

func (r *Renderer) renderParagraph(b *epub.Block, width int) renderedBlock {
	if b.Quote {
		base := Style{Italic: true}
		prefix := "  " + Style{FG: theme.Muted}.Render("│ ")
		lines, links := wrapStyledLinesWithLinks(b.Spans, width, base, prefix, prefix)
		return renderedBlock{Lines: lines, Links: links}
	}
	lines, links := wrapStyledLinesWithLinks(b.Spans, width, Style{}, "  ", "  ")
	return renderedBlock{Lines: lines, Links: links}
}

func (r *Renderer) renderListItem(b *epub.Block, width int) renderedBlock {
	depth := b.Depth
	if depth < 1 {
		depth = 1
	}
	marker := "• "
	if b.Ordered {
		marker = fmt.Sprintf("%d. ", b.Number)
	}
	quotePrefix := ""
	if b.Quote {
		quotePrefix = Style{FG: theme.Muted}.Render("│ ")
	}
	indent := strings.Repeat("  ", depth-1)
	prefixBase := "  " + quotePrefix + indent
	first := prefixBase + Style{FG: theme.Accent}.Render(marker)
	cont := prefixBase + strings.Repeat(" ", visibleWidth(marker))
	base := Style{}
	if b.Quote {
		base.Italic = true
	}
	lines, links := wrapStyledLinesWithLinks(b.Spans, width, base, first, cont)
	return renderedBlock{Lines: lines, Links: links}
}

func (r *Renderer) renderTable(b *epub.Block, width int) renderedBlock {
	var out []string
	for _, row := range b.Table {
		text := strings.Join(row, "  │  ")
		spans := []epub.Span{{Text: text}}
		lines := wrapStyledLines(spans, width, Style{}, "  ", "  ")
		out = append(out, lines...)
	}
	return singleBlock(out)
}

func renderRule(width int) []string {
	n := width - 4
	if n > 72 {
		n = 72
	}
	if n < 3 {
		return nil
	}
	return []string{"  " + Style{FG: theme.Dim}.Render(strings.Repeat("─", n))}
}

func (r *Renderer) renderImage(b *epub.Block, width int, opts Options, index, total, lineStart int, spans *[]ImageSpan) []string {
	ref := epub.ImageRef{}
	if b.Image != nil {
		ref = *b.Image
	}
	maxCols := opts.MaxImageCols
	limit := width - 6
	if limit < 8 {
		limit = 8
	}
	if maxCols > limit {
		maxCols = limit
	}
	if maxCols < 8 {
		maxCols = 0
	}
	label := fmt.Sprintf("[图片 %d/%d]", index, total)
	start := lineStart
	var out []string
	var img image.Image
	var imgW, imgH int
	var loadErr error
	if ref.Path != "" {
		img, loadErr = r.loadImage(ref.Path)
		if loadErr == nil && img != nil {
			bounds := img.Bounds()
			imgW, imgH = bounds.Dx(), bounds.Dy()
		}
	}
	if ref.Path == "" {
		reason := "图片不可用"
		if ref.External {
			reason = "外部图片已忽略"
		}
		out = append(out, "  "+Style{FG: theme.Muted}.Render(label+" · "+reason))
		if ref.Alt != "" {
			out = append(out, wrapStyledLines([]epub.Span{{Text: ref.Alt, Italic: true}}, width, Style{FG: theme.Muted}, "  ", "  ")...)
		}
		return out
	}
	if loadErr != nil {
		out = append(out, "  "+Style{FG: theme.Muted}.Render(label+" · 无法预览"))
	} else {
		meta := fmt.Sprintf(" %d×%d · Enter 查看原图", imgW, imgH)
		line := "  " + Style{FG: theme.Accent}.Render(label) + Style{FG: theme.Muted}.Render(meta)
		out = append(out, truncateVisible(line, width))
		if maxCols > 0 {
			key := fmt.Sprintf("%s|%d|%d", ref.Path, maxCols, opts.MaxImageRows)
			thumb, err := r.thumbnail(ref.Path, img, maxCols, opts.MaxImageRows, key)
			if err == nil {
				for _, line := range thumb.lines {
					out = append(out, "  "+line)
				}
			}
		}
	}
	if ref.Alt != "" {
		out = append(out, wrapStyledLines([]epub.Span{{Text: ref.Alt, Italic: true}}, width, Style{FG: theme.Muted}, "  ", "  ")...)
	}
	*spans = append(*spans, ImageSpan{
		Index:     index,
		StartLine: start,
		EndLine:   start + len(out),
		Ref:       ref,
		PixelW:    imgW,
		PixelH:    imgH,
	})
	return out
}

func (r *Renderer) loadImage(path string) (image.Image, error) {
	if r.images == nil {
		return nil, fmt.Errorf("没有图片数据源")
	}
	r.mu.Lock()
	if img, ok := r.cache[path]; ok {
		r.mu.Unlock()
		return img, nil
	}
	r.mu.Unlock()
	data, _, err := r.images.ImageBytes(path)
	if err != nil {
		return nil, err
	}
	img, err := decodeImage(data)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	pixels := int64(b.Dx()) * int64(b.Dy())
	r.mu.Lock()
	if len(r.cache) >= 16 || r.cachePixels+pixels > 80_000_000 {
		r.cache = make(map[string]image.Image)
		r.cachePixels = 0
	}
	r.cache[path] = img
	r.cachePixels += pixels
	r.mu.Unlock()
	return img, nil
}

func (r *Renderer) thumbnail(path string, img image.Image, cols, rows int, key string) (thumbnail, error) {
	r.mu.Lock()
	if th, ok := r.thumbs[key]; ok {
		r.mu.Unlock()
		return th, nil
	}
	r.mu.Unlock()
	th, err := makeThumbnail(img, cols, rows)
	if err != nil {
		return thumbnail{}, err
	}
	r.mu.Lock()
	if len(r.thumbs) > 128 {
		r.thumbs = make(map[string]thumbnail)
	}
	r.thumbs[key] = th
	r.mu.Unlock()
	return th, nil
}

func (rc *RenderedChapter) VisibleImage(top, height int) *ImageSpan {
	if height <= 0 || len(rc.Images) == 0 {
		return nil
	}
	bottom := top + height
	var best *ImageSpan
	bestOverlap := 0
	for i := range rc.Images {
		img := &rc.Images[i]
		start := img.StartLine
		end := img.EndLine
		if start < top {
			start = top
		}
		if end > bottom {
			end = bottom
		}
		overlap := end - start
		if overlap > bestOverlap {
			bestOverlap = overlap
			best = img
		}
	}
	return best
}

// LinkAt returns the link target covering the given line and display column.
func (rc *RenderedChapter) LinkAt(line, col int) (string, bool) {
	for i := range rc.Links {
		l := &rc.Links[i]
		if l.Line == line && col >= l.StartCol && col < l.EndCol {
			return l.Href, true
		}
	}
	return "", false
}

func (rc *RenderedChapter) LineForAnchor(fragment string) (int, bool) {
	line, ok := rc.Anchors[fragment]
	return line, ok
}

// ScrollPercent returns a platform-stable chapter progress value. A chapter
// shorter than the viewport is considered 0, matching OffsetForPercent.
func ScrollPercent(totalLines, viewportHeight, offset int) float64 {
	max := maxOffset(totalLines, viewportHeight)
	if max <= 0 {
		return 0
	}
	if offset < 0 {
		offset = 0
	}
	if offset > max {
		offset = max
	}
	return float64(offset) / float64(max)
}

func OffsetForPercent(totalLines, viewportHeight int, percent float64) int {
	max := maxOffset(totalLines, viewportHeight)
	if max <= 0 {
		return 0
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 1 {
		percent = 1
	}
	return int(math.Round(percent * float64(max)))
}

func maxOffset(totalLines, viewportHeight int) int {
	if viewportHeight <= 0 {
		return 0
	}
	n := totalLines - viewportHeight
	if n < 0 {
		return 0
	}
	return n
}
