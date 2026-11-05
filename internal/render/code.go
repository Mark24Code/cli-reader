package render

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"claude-epub-reader/internal/theme"
)

type codeCell struct {
	r     rune
	w     int
	style Style
}

func renderCodeBlock(source, lang string, width int) []string {
	if width < 12 {
		return nil
	}
	source = expandTabs(strings.ReplaceAll(source, "\r\n", "\n"), 4)
	lexerName, lines := highlightCode(source, lang)

	avail := width - 4
	if avail < 8 {
		avail = 8
	}
	headerStyle := Style{FG: theme.Dim, BG: theme.CodeBG}
	label := lexerName
	if label == "" {
		label = "code"
	}
	header := "  " + Style{FG: theme.AccentSoft, BG: theme.CodeBG}.Render("╭─ "+label+" ") +
		padBg(width-2-visibleWidth("╭─ "+label+" "), headerStyle)
	out := []string{truncateVisible(header, width)}

	gutter := Style{FG: theme.AccentSoft, BG: theme.CodeBG}
	padStyle := Style{BG: theme.CodeBG}
	for _, logical := range lines {
		chunks := splitCodeLine(logical, avail)
		if len(chunks) == 0 {
			chunks = [][]codeCell{nil}
		}
		for _, chunk := range chunks {
			content := renderCodeCells(chunk)
			used := codeCellsWidth(chunk)
			padding := ""
			if used < avail {
				padding = padStyle.Render(strings.Repeat(" ", avail-used))
			}
			out = append(out, "  "+gutter.Render("│ ")+content+padding)
		}
	}
	footer := "  " + Style{FG: theme.Dim, BG: theme.CodeBG}.Render("╰─") +
		padBg(width-2-visibleWidth("╰─"), headerStyle)
	out = append(out, truncateVisible(footer, width))
	return out
}

func padBg(n int, st Style) string {
	if n <= 0 {
		return ""
	}
	return st.Render(strings.Repeat(" ", n))
}

func highlightCode(source, lang string) (string, [][]codeCell) {
	lexer := lexers.Get(strings.ToLower(strings.TrimSpace(lang)))
	if lexer == nil {
		if strings.TrimSpace(lang) == "" {
			lexer = lexers.Analyse(source)
		}
	}
	name := ""
	if lexer != nil {
		name = strings.ToLower(lexer.Config().Name)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	tokens, ok := tokeniseSafely(chroma.Coalesce(lexer), source)
	if !ok {
		return "", plainCodeLines(source)
	}
	var lines [][]codeCell
	var line []codeCell
	appendNewline := func() {
		lines = append(lines, line)
		line = nil
	}
	for _, tok := range tokens {
		st := chromaTokenStyle(tok.Type)
		parts := strings.Split(tok.Value, "\n")
		for i, part := range parts {
			if i > 0 {
				appendNewline()
			}
			for _, r := range part {
				if r == '\r' {
					continue
				}
				line = append(line, codeCell{r: r, w: runeWidth(r), style: st})
			}
		}
	}
	if len(line) > 0 || len(lines) == 0 {
		lines = append(lines, line)
	}
	return name, lines
}

func tokeniseSafely(lexer chroma.Lexer, source string) (tokens []chroma.Token, ok bool) {
	defer func() {
		if recover() != nil {
			tokens = nil
			ok = false
		}
	}()
	tokens, err := chroma.Tokenise(lexer, nil, source)
	if err != nil {
		return nil, false
	}
	return tokens, true
}

func chromaTokenStyle(tt chroma.TokenType) Style {
	entry := theme.ChromaStyle().Get(tt)
	st := Style{FG: theme.CodeText, BG: theme.CodeBG}
	if entry.Colour.IsSet() {
		if hex := entry.Colour.String(); strings.HasPrefix(hex, "#") && len(hex) == 7 {
			st.FG = hex
		}
	}
	if entry.Bold == chroma.Yes {
		st.Bold = true
	}
	if entry.Italic == chroma.Yes {
		st.Italic = true
	}
	if entry.Underline == chroma.Yes {
		st.Underline = true
	}
	return st
}

func plainCodeLines(source string) [][]codeCell {
	var lines [][]codeCell
	for _, part := range strings.Split(source, "\n") {
		var line []codeCell
		for _, r := range part {
			line = append(line, codeCell{r: r, w: runeWidth(r), style: Style{FG: theme.CodeText, BG: theme.CodeBG}})
		}
		lines = append(lines, line)
	}
	return lines
}

func splitCodeLine(cells []codeCell, width int) [][]codeCell {
	if len(cells) == 0 {
		return nil
	}
	var out [][]codeCell
	var cur []codeCell
	curWidth := 0
	for _, c := range cells {
		if c.w > 0 && curWidth+c.w > width && len(cur) > 0 {
			out = append(out, cur)
			cur = nil
			curWidth = 0
		}
		cur = append(cur, c)
		curWidth += c.w
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

func renderCodeCells(cells []codeCell) string {
	var b strings.Builder
	var run []rune
	var st Style
	have := false
	flush := func() {
		if have && len(run) > 0 {
			b.WriteString(st.Render(string(run)))
			run = run[:0]
		}
	}
	for _, c := range cells {
		if !have || c.style != st {
			flush()
			st = c.style
			have = true
		}
		run = append(run, c.r)
	}
	flush()
	return b.String()
}

func codeCellsWidth(cells []codeCell) int {
	n := 0
	for _, c := range cells {
		n += c.w
	}
	return n
}

func expandTabs(s string, tab int) string {
	if tab <= 0 || !strings.ContainsRune(s, '\t') {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		switch r {
		case '\n':
			b.WriteRune(r)
			col = 0
		case '\t':
			n := tab - (col % tab)
			b.WriteString(strings.Repeat(" ", n))
			col += n
		default:
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}
