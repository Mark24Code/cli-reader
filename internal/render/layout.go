package render

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"cli-reader/internal/book"
	"cli-reader/internal/theme"
)

type cell struct {
	r       rune
	w       int
	style   Style
	link    string
	newline bool
}

// lineLink is a clickable link run within one laid-out line. Columns are
// zero-based display cells.
type lineLink struct {
	start int
	end   int
	href  string
}

type chunk struct {
	cells       []cell
	width       int
	spaceBefore bool
	hardBreak   bool
}

func layoutSpans(spans []book.Span, width int, base Style, styleFor func(book.Span, Style) Style) [][]cell {
	if width < 1 {
		width = 1
	}
	var cells []cell
	for _, sp := range spans {
		st := styleFor(sp, base)
		for _, r := range sp.Text {
			if r == '\n' {
				cells = append(cells, cell{newline: true})
				continue
			}
			w := runewidth.RuneWidth(r)
			if w < 0 {
				w = 0
			}
			cells = append(cells, cell{r: r, w: w, style: st, link: sp.Link})
		}
	}
	return layoutCells(cells, width)
}

func layoutCells(cells []cell, width int) [][]cell {
	if width < 1 {
		width = 1
	}
	chunks := makeChunks(cells)
	if len(chunks) == 0 {
		return nil
	}
	var lines [][]cell
	var line []cell
	lineWidth := 0
	flush := func() {
		lines = append(lines, line)
		line = nil
		lineWidth = 0
	}
	for _, ch := range chunks {
		if ch.hardBreak {
			flush()
			continue
		}
		if len(line) > 0 && ch.spaceBefore {
			if lineWidth+1+ch.width > width {
				flush()
			} else {
				sp := Style{}
				link := ""
				if len(ch.cells) > 0 {
					sp = ch.cells[0].style
					link = ch.cells[0].link
				}
				line = append(line, cell{r: ' ', w: 1, style: sp, link: link})
				lineWidth++
			}
		}
		for _, c := range ch.cells {
			if c.w > 0 && len(line) > 0 && lineWidth+c.w > width {
				flush()
			}
			line = append(line, c)
			lineWidth += c.w
		}
	}
	if len(line) > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}

func makeChunks(cells []cell) []chunk {
	var chunks []chunk
	var word []cell
	wordWidth := 0
	wordSpace := false
	pendingSpace := false

	flushWord := func() {
		if len(word) == 0 {
			return
		}
		chunks = append(chunks, chunk{cells: word, width: wordWidth, spaceBefore: wordSpace})
		word = nil
		wordWidth = 0
		wordSpace = false
	}
	for _, c := range cells {
		if c.newline {
			flushWord()
			chunks = append(chunks, chunk{hardBreak: true})
			pendingSpace = false
			continue
		}
		if unicode.IsSpace(c.r) {
			flushWord()
			pendingSpace = true
			continue
		}
		wide := c.w >= 2
		if wide {
			flushWord()
			chunks = append(chunks, chunk{
				cells:       []cell{c},
				width:       c.w,
				spaceBefore: pendingSpace,
			})
			pendingSpace = false
			continue
		}
		if len(word) == 0 {
			wordSpace = pendingSpace
			pendingSpace = false
		}
		word = append(word, c)
		wordWidth += c.w
	}
	flushWord()
	return chunks
}

func renderCells(cells []cell) string {
	var b strings.Builder
	var run []rune
	var runStyle Style
	haveRun := false
	flush := func() {
		if !haveRun || len(run) == 0 {
			return
		}
		b.WriteString(runStyle.Render(string(run)))
		run = run[:0]
	}
	for _, c := range cells {
		if c.newline {
			continue
		}
		if !haveRun || c.style != runStyle {
			flush()
			runStyle = c.style
			haveRun = true
		}
		run = append(run, c.r)
	}
	flush()
	return b.String()
}

func lineWidth(cells []cell) int {
	n := 0
	for _, c := range cells {
		n += c.w
	}
	return n
}

func spanStyle(sp book.Span, base Style) Style {
	st := base
	if sp.Bold {
		st.Bold = true
	}
	if sp.Italic {
		st.Italic = true
	}
	if sp.Underline {
		st.Underline = true
	}
	if sp.Code {
		st.FG = theme.InlineCodeFG
		st.BG = theme.InlineCodeBG
	}
	if sp.Link != "" {
		st.Underline = true
		if !sp.Code {
			st.FG = theme.Accent
		}
	}
	return st
}

func visibleWidth(s string) int {
	return ansi.StringWidth(s)
}

func padRight(s string, width int) string {
	w := visibleWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func truncateVisible(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if visibleWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// collectLineLinks turns linked cells into contiguous column runs. Interior
// spaces inherit their neighbouring link so a multi-word link stays one run.
func collectLineLinks(cells []cell) []lineLink {
	var out []lineLink
	col := 0
	start := -1
	href := ""
	flush := func(end int) {
		if start >= 0 && end > start {
			out = append(out, lineLink{start: start, end: end, href: href})
		}
		start = -1
		href = ""
	}
	for _, c := range cells {
		if c.link == "" {
			flush(col)
		} else if start < 0 || c.link != href {
			flush(col)
			start = col
			href = c.link
		}
		col += c.w
	}
	flush(col)
	return out
}

func wrapStyledLines(spans []book.Span, width int, base Style, firstPrefix, contPrefix string) []string {
	lines, _ := wrapStyledLinesWithLinks(spans, width, base, firstPrefix, contPrefix)
	return lines
}

func wrapStyledLinesWithLinks(spans []book.Span, width int, base Style, firstPrefix, contPrefix string) ([]string, [][]lineLink) {
	if width <= 0 {
		return nil, nil
	}
	firstStyled := firstPrefix
	contStyled := contPrefix
	firstW := visibleWidth(firstStyled)
	contW := visibleWidth(contStyled)
	prefixW := firstW
	if contW > prefixW {
		prefixW = contW
	}
	avail := width - prefixW
	if avail < 1 {
		avail = 1
	}
	lines := layoutSpans(spans, avail, base, spanStyle)
	if len(lines) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(lines))
	links := make([][]lineLink, len(lines))
	for i, line := range lines {
		prefix := contStyled
		if i == 0 {
			prefix = firstStyled
		}
		prefix = padRight(prefix, prefixW)
		content := renderCells(line)
		if strings.TrimSpace(ansi.Strip(content)) == "" {
			content = ""
		}
		out = append(out, strings.TrimRight(prefix+content, " "))
		if ls := collectLineLinks(line); len(ls) > 0 {
			for j := range ls {
				ls[j].start += prefixW
				ls[j].end += prefixW
			}
			links[i] = ls
		}
	}
	return out, links
}

func runeWidth(r rune) int {
	w := runewidth.RuneWidth(r)
	if w < 0 {
		return 0
	}
	return w
}
