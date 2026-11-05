package render

import (
	"fmt"
	"strconv"
	"strings"
)

type Style struct {
	FG, BG    string
	Bold      bool
	Italic    bool
	Underline bool
	Faint     bool
}

func (s Style) zero() bool {
	return s.FG == "" && s.BG == "" && !s.Bold && !s.Italic && !s.Underline && !s.Faint
}

func (s Style) Render(text string) string {
	if text == "" {
		return ""
	}
	if s.zero() {
		return text
	}
	var codes []string
	if s.Bold {
		codes = append(codes, "1")
	}
	if s.Faint {
		codes = append(codes, "2")
	}
	if s.Italic {
		codes = append(codes, "3")
	}
	if s.Underline {
		codes = append(codes, "4")
	}
	if r, g, b, ok := parseHex(s.FG); ok {
		codes = append(codes, fmt.Sprintf("38;2;%d;%d;%d", r, g, b))
	}
	if r, g, b, ok := parseHex(s.BG); ok {
		codes = append(codes, fmt.Sprintf("48;2;%d;%d;%d", r, g, b))
	}
	if len(codes) == 0 {
		return text
	}
	return "\x1b[" + strings.Join(codes, ";") + "m" + text + "\x1b[0m"
}

func parseHex(s string) (int, int, int, bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(v>>16) & 0xff, int(v>>8) & 0xff, int(v) & 0xff, true
}

func (s Style) withBold(v bool) Style {
	s.Bold = v
	return s
}

func (s Style) withItalic(v bool) Style {
	s.Italic = v
	return s
}

func (s Style) withUnderline(v bool) Style {
	s.Underline = v
	return s
}

func (s Style) withFG(c string) Style {
	s.FG = c
	return s
}

func (s Style) withBG(c string) Style {
	s.BG = c
	return s
}
