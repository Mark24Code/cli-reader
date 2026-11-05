package render

import (
	"sort"
	"strings"

	"cli-epub-reader/internal/epub"
	"cli-epub-reader/internal/theme"
)

const tableMargin = "  "

// tablePlacement is one cell positioned on the logical grid, honouring
// colspan and rowspan attributes.
type tablePlacement struct {
	row, col         int
	rowSpan, colSpan int
	cell             epub.TableCell
}

// renderedCell is the laid-out content of a single table cell.
type renderedCell struct {
	lines []string
	links [][]lineLink
	width int
	align string
}

// renderTable lays a table out as a real grid: column widths are derived from
// the content, cells are wrapped inside their column, and box-drawing borders
// keep rows aligned. Colspan/rowspan are merged across the grid. When the
// terminal is too narrow for even the minimum grid, the table falls back to a
// stacked key/value layout instead of overflowing.
func (r *Renderer) renderTable(b *epub.Block, width int) renderedBlock {
	if b == nil || len(b.Table) == 0 {
		return renderedBlock{}
	}
	placements, ncols := buildTablePlacements(b.Table)
	if ncols == 0 || len(placements) == 0 {
		return renderedBlock{}
	}

	// Each column costs 2 padding cells; every border costs one column.
	available := width - len(tableMargin) - (3*ncols + 1)
	widths := distributeColumnWidths(naturalColumnWidths(placements, ncols), available)
	if widths == nil {
		return r.renderTableStacked(b, width)
	}

	borderPos := make([]int, ncols+1)
	for j := 0; j < ncols; j++ {
		borderPos[j+1] = borderPos[j] + widths[j] + 3
	}

	cells := make([]renderedCell, len(placements))
	for i := range placements {
		p := &placements[i]
		contentWidth := borderPos[p.col+p.colSpan] - borderPos[p.col] - 3
		if contentWidth < 1 {
			contentWidth = 1
		}
		base := Style{}
		if p.cell.Header {
			base = Style{FG: theme.Accent, Bold: true}
		}
		raw := layoutSpans(p.cell.Spans, contentWidth, base, spanStyle)
		raw = hardWrapCellLines(raw, contentWidth)
		rc := renderedCell{
			lines: make([]string, len(raw)),
			links: make([][]lineLink, len(raw)),
			width: contentWidth,
			align: p.cell.Align,
		}
		for k, line := range raw {
			rc.lines[k] = renderCells(line)
			rc.links[k] = collectLineLinks(line)
		}
		if rc.align == "" && p.cell.Header {
			rc.align = "center"
		}
		cells[i] = rc
	}

	rowHeights := make([]int, len(b.Table))
	for i := range rowHeights {
		rowHeights[i] = 1
	}
	rowCells := make([][]int, len(b.Table))
	for i := range placements {
		p := &placements[i]
		if h := len(cells[i].lines); h > rowHeights[p.row] {
			rowHeights[p.row] = h
		}
		for rr := p.row; rr < p.row+p.rowSpan && rr < len(rowCells); rr++ {
			rowCells[rr] = append(rowCells[rr], i)
		}
	}
	for _, list := range rowCells {
		sort.Slice(list, func(a, b int) bool {
			return placements[list[a]].col < placements[list[b]].col
		})
	}

	var lines []string
	var links [][]lineLink
	add := func(line string, ls []lineLink) {
		lines = append(lines, line)
		if len(ls) == 0 {
			links = append(links, nil)
			return
		}
		links = append(links, ls)
	}

	if len(b.TableCaption) > 0 {
		caption, captionLinks := wrapStyledLinesWithLinks(b.TableCaption, width,
			Style{Italic: true, FG: theme.Muted}, tableMargin, tableMargin)
		for i, line := range caption {
			var ls []lineLink
			if i < len(captionLinks) {
				ls = captionLinks[i]
			}
			add(line, ls)
		}
	}

	border := Style{FG: theme.Dim}
	add(tableMargin+border.Render(tableRule(widths, "┌", "┬", "┐")), nil)

	for row := range b.Table {
		height := rowHeights[row]
		for lineIdx := 0; lineIdx < height; lineIdx++ {
			var sb strings.Builder
			sb.WriteString(tableMargin)
			sb.WriteString(border.Render("│"))
			x := len(tableMargin) + 1
			var ls []lineLink
			for _, idx := range rowCells[row] {
				p := &placements[idx]
				cell := &cells[idx]
				text := ""
				if p.row == row && lineIdx < len(cell.lines) {
					text = cell.lines[lineIdx]
				}
				contentCol := x + 1
				sb.WriteByte(' ')
				sb.WriteString(padAligned(text, cell.width, cell.align))
				sb.WriteByte(' ')
				if p.row == row && lineIdx < len(cell.links) {
					for _, link := range cell.links[lineIdx] {
						ls = append(ls, lineLink{
							start: contentCol + link.start,
							end:   contentCol + link.end,
							href:  link.href,
						})
					}
				}
				sb.WriteString(border.Render("│"))
				x += cell.width + 3
			}
			add(sb.String(), ls)
		}
		if row < len(b.Table)-1 {
			add(tableMargin+border.Render(tableSeparator(widths, placements, row)), nil)
		}
	}
	add(tableMargin+border.Render(tableRule(widths, "└", "┴", "┘")), nil)

	return renderedBlock{Lines: lines, Links: links}
}

func tableRule(widths []int, left, mid, right string) string {
	var sb strings.Builder
	sb.WriteString(left)
	for i, w := range widths {
		sb.WriteString(strings.Repeat("─", w+2))
		if i < len(widths)-1 {
			sb.WriteString(mid)
		}
	}
	sb.WriteString(right)
	return sb.String()
}

// tableSeparator renders the horizontal rule between two rows. Columns covered
// by a cell spanning both rows stay open, so the merged cell is not cut.
func tableSeparator(widths []int, placements []tablePlacement, row int) string {
	n := len(widths)
	open := make([]bool, n)
	for i := range open {
		open[i] = true
	}
	internal := make([]bool, n+1)
	for _, p := range placements {
		if p.row > row || p.row+p.rowSpan <= row+1 {
			continue
		}
		for j := p.col; j < p.col+p.colSpan && j < n; j++ {
			open[j] = false
		}
		for k := p.col + 1; k < p.col+p.colSpan && k <= n; k++ {
			internal[k] = true
		}
	}

	var sb strings.Builder
	sb.WriteString(tableJunction(false, n > 0 && open[0]))
	for j := 0; j < n; j++ {
		if open[j] {
			sb.WriteString(strings.Repeat("─", widths[j]+2))
		} else {
			sb.WriteString(strings.Repeat(" ", widths[j]+2))
		}
		k := j + 1
		switch {
		case k == n:
			sb.WriteString(tableJunction(open[n-1], false))
		case internal[k]:
			sb.WriteByte(' ')
		default:
			sb.WriteString(tableJunction(open[j], open[k]))
		}
	}
	return sb.String()
}

func tableJunction(left, right bool) string {
	switch {
	case left && right:
		return "┼"
	case left:
		return "┤"
	case right:
		return "├"
	default:
		return "│"
	}
}

func buildTablePlacements(rows [][]epub.TableCell) ([]tablePlacement, int) {
	occupied := make(map[[2]int]bool)
	var out []tablePlacement
	ncols := 0
	for r := range rows {
		col := 0
		for ci := range rows[r] {
			cell := rows[r][ci]
			for occupied[[2]int{r, col}] {
				col++
			}
			colSpan := cell.ColSpan
			if colSpan < 1 {
				colSpan = 1
			}
			if colSpan > 100 {
				colSpan = 100
			}
			rowSpan := cell.RowSpan
			if rowSpan < 1 {
				rowSpan = 1
			}
			if rowSpan > len(rows)-r {
				rowSpan = len(rows) - r
			}
			out = append(out, tablePlacement{
				row: r, col: col,
				rowSpan: rowSpan, colSpan: colSpan,
				cell: cell,
			})
			for dr := 0; dr < rowSpan; dr++ {
				for dc := 0; dc < colSpan; dc++ {
					occupied[[2]int{r + dr, col + dc}] = true
				}
			}
			col += colSpan
			if col > ncols {
				ncols = col
			}
		}
	}
	// Ragged rows (fewer cells than the widest row) still need a cell in every
	// column, otherwise their borders would not line up with the grid.
	for r := range rows {
		used := make([]bool, ncols)
		for _, p := range out {
			if r < p.row || r >= p.row+p.rowSpan {
				continue
			}
			for j := p.col; j < p.col+p.colSpan && j < ncols; j++ {
				used[j] = true
			}
		}
		for j := 0; j < ncols; j++ {
			if !used[j] {
				out = append(out, tablePlacement{row: r, col: j, rowSpan: 1, colSpan: 1})
			}
		}
	}
	return out, ncols
}

// naturalColumnWidths finds the width each column would like to have. Cells
// spanning several columns contribute their required width across them; the
// internal separator cells are not available inside a spanning cell.
func naturalColumnWidths(placements []tablePlacement, ncols int) []int {
	widths := make([]int, ncols)
	for i := range widths {
		widths[i] = 1
	}
	for _, p := range placements {
		need := spansDisplayWidth(p.cell.Spans) - (p.colSpan - 1)
		if need < 1 {
			continue
		}
		current := 0
		for j := p.col; j < p.col+p.colSpan && j < ncols; j++ {
			current += widths[j]
		}
		extra := need - current
		if extra <= 0 {
			continue
		}
		per := extra / p.colSpan
		rem := extra % p.colSpan
		for j := p.col; j < p.col+p.colSpan && j < ncols; j++ {
			widths[j] += per
		}
		last := p.col + p.colSpan - 1
		if last >= ncols {
			last = ncols - 1
		}
		widths[last] += rem
	}
	return widths
}

// distributeColumnWidths fits the natural widths into the available space. It
// returns nil when even one cell per column cannot fit.
func distributeColumnWidths(natural []int, available int) []int {
	n := len(natural)
	if n == 0 || available < 2*n {
		return nil
	}
	widths := make([]int, n)
	sum := 0
	for i, w := range natural {
		if w < 2 {
			w = 2
		}
		widths[i] = w
		sum += w
	}
	if sum <= available {
		return widths
	}

	const minWidth = 2
	for sum > available {
		pool := 0
		for _, w := range widths {
			pool += w - minWidth
		}
		if pool <= 0 {
			return nil
		}
		over := sum - available
		shrunk := 0
		fallback := -1
		for i := range widths {
			room := widths[i] - minWidth
			if room <= 0 {
				continue
			}
			cut := over * room / pool
			if cut > room {
				cut = room
			}
			if cut == 0 {
				fallback = i
				continue
			}
			widths[i] -= cut
			shrunk += cut
		}
		if shrunk == 0 {
			if fallback < 0 {
				return nil
			}
			widths[fallback]--
			shrunk = 1
		}
		sum -= shrunk
	}
	return widths
}

// spansDisplayWidth is the widest hard-break-delimited line of a span run.
func spansDisplayWidth(spans []epub.Span) int {
	best, current := 0, 0
	for _, sp := range spans {
		if sp.Break {
			if current > best {
				best = current
			}
			current = 0
			continue
		}
		for _, r := range sp.Text {
			if r == '\n' {
				if current > best {
					best = current
				}
				current = 0
				continue
			}
			current += runeWidth(r)
		}
	}
	if current > best {
		best = current
	}
	return best
}

// hardWrapCellLines splits lines that contain an unbreakable run (for example
// a long URL) so every cell line fits its column.
func hardWrapCellLines(lines [][]cell, width int) [][]cell {
	if width < 1 {
		return lines
	}
	var out [][]cell
	for _, line := range lines {
		var current []cell
		currentWidth := 0
		for _, c := range line {
			if c.newline {
				continue
			}
			if c.w > 0 && currentWidth+c.w > width && len(current) > 0 {
				out = append(out, current)
				current = nil
				currentWidth = 0
			}
			current = append(current, c)
			currentWidth += c.w
		}
		out = append(out, current)
	}
	if len(out) == 0 {
		out = [][]cell{nil}
	}
	return out
}

func padAligned(s string, width int, align string) string {
	w := visibleWidth(s)
	if w >= width {
		return s
	}
	gap := width - w
	switch align {
	case "center":
		left := gap / 2
		return strings.Repeat(" ", left) + s + strings.Repeat(" ", gap-left)
	case "right":
		return strings.Repeat(" ", gap) + s
	default:
		return s + strings.Repeat(" ", gap)
	}
}

// renderTableStacked is the narrow-terminal fallback. With a header row it
// prints readable "header: value" pairs; otherwise each row becomes a wrapped
// line with its cells separated by bars.
func (r *Renderer) renderTableStacked(b *epub.Block, width int) renderedBlock {
	var lines []string
	var links [][]lineLink
	add := func(spans []epub.Span, first, cont string, base Style) {
		ls, ll := wrapStyledLinesWithLinks(spans, width, base, first, cont)
		for i, line := range ls {
			lines = append(lines, line)
			if i < len(ll) {
				links = append(links, ll[i])
			} else {
				links = append(links, nil)
			}
		}
	}
	addSpans := func(spans []epub.Span, first, cont string) {
		add(spans, first, cont, Style{})
	}

	headers := tableHeaders(b.Table)
	start := 0
	if len(headers) > 0 {
		start = 1
	}
	for row := start; row < len(b.Table); row++ {
		cells := b.Table[row]
		if len(headers) == len(cells) {
			for i, cell := range cells {
				label := headers[i]
				if label == "" {
					label = "•"
				}
				first := tableMargin + Style{FG: theme.Muted}.Render(label+": ")
				cont := tableMargin + strings.Repeat(" ", visibleWidth(label)+2)
				addSpans(cell.Spans, first, cont)
			}
			continue
		}
		var spans []epub.Span
		for i, cell := range cells {
			if i > 0 {
				spans = append(spans, epub.Span{Text: " │ "})
			}
			spans = append(spans, cell.Spans...)
		}
		addSpans(spans, tableMargin, tableMargin)
	}
	if start == 1 {
		// The header row is rendered as labels; if no data row matched its
		// width (colspan, for example), keep the header content visible.
		rendered := false
		for row := start; row < len(b.Table); row++ {
			if len(b.Table[row]) == len(headers) {
				rendered = true
				break
			}
		}
		if !rendered {
			var spans []epub.Span
			for i, cell := range b.Table[0] {
				if i > 0 {
					spans = append(spans, epub.Span{Text: " │ "})
				}
				spans = append(spans, cell.Spans...)
			}
			addSpans(spans, tableMargin, tableMargin)
		}
	}
	if len(lines) == 0 && len(b.Table) > 0 {
		for _, cell := range b.Table[0] {
			addSpans(cell.Spans, tableMargin, tableMargin)
		}
	}
	return renderedBlock{Lines: lines, Links: links}
}

func tableHeaders(rows [][]epub.TableCell) []string {
	if len(rows) == 0 {
		return nil
	}
	row := rows[0]
	headers := make([]string, len(row))
	anyHeader := false
	for i, cell := range row {
		if cell.Header {
			anyHeader = true
		}
		headers[i] = spansPlainText(cell.Spans)
	}
	if !anyHeader {
		return nil
	}
	return headers
}

func spansPlainText(spans []epub.Span) string {
	var sb strings.Builder
	for _, sp := range spans {
		if sp.Break {
			sb.WriteByte(' ')
			continue
		}
		sb.WriteString(sp.Text)
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}
