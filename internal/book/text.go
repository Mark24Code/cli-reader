package book

import (
	"strings"
	"unicode"
)

// SanitizeText removes terminal control characters and normalises common
// whitespace. Book content is untrusted input and must never be able to inject
// ANSI escape sequences into the TUI.
func SanitizeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteByte('\n')
		case r == '\t':
			b.WriteByte('\t')
		case r == ' ' || r == ' ':
			b.WriteByte(' ')
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
			// Drop C0/C1 controls, including ESC and BEL.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CollapseSpace trims and collapses runs of whitespace into one space. It keeps
// explicit newlines so hard breaks survive.
func CollapseSpace(s string) string {
	s = SanitizeText(s)
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if r == '\n' {
			b.WriteByte('\n')
			space = false
			continue
		}
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// IsCJK reports whether r is a Chinese, Japanese or Korean character or a
// full-width punctuation mark. It decides whether wrapped lines join with a
// space or directly.
func IsCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r) ||
		(r >= 0x3000 && r <= 0x303f) ||
		(r >= 0xff00 && r <= 0xffef)
}

// JoinLines joins two wrapped text lines. A trailing hyphen is removed when the
// next line continues a word; CJK boundaries join without a space.
func JoinLines(a, b string) string {
	a = strings.TrimRight(a, " \t")
	b = strings.TrimLeft(b, " \t")
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	ar := []rune(a)
	br := []rune(b)
	if ar[len(ar)-1] == '-' && len(ar) > 1 && !unicode.IsSpace(ar[len(ar)-2]) &&
		unicode.IsLower(br[0]) {
		return strings.TrimSuffix(a, "-") + b
	}
	if IsCJK(ar[len(ar)-1]) || IsCJK(br[0]) {
		return a + b
	}
	return a + " " + b
}
