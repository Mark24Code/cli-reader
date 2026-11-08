// Package textdecode turns raw text bytes into UTF-8 using BOM markers,
// UTF-8 validation and statistical charset detection.
package textdecode

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/saintfish/chardet"
	"golang.org/x/text/encoding/htmlindex"
)

// Result is decoded text plus the detected encoding label.
type Result struct {
	Text     string
	Encoding string
	Warning  string
}

// Decode converts raw text bytes to UTF-8.
func Decode(data []byte) Result {
	data = NormalizeNewlines(data)
	if len(data) == 0 {
		return Result{Text: "", Encoding: "UTF-8"}
	}
	if text, enc, ok := decodeBOM(data); ok {
		return Result{Text: text, Encoding: enc}
	}
	if utf8.Valid(data) {
		return Result{Text: string(data), Encoding: "UTF-8"}
	}

	detected, err := chardet.NewTextDetector().DetectBest(data)
	if err == nil && detected != nil && isCJKLabel(detected.Charset) && detected.Confidence >= 30 {
		label := normalizeLabel(detected.Charset)
		if enc, getErr := htmlindex.Get(label); getErr == nil {
			decoded, decErr := enc.NewDecoder().Bytes(data)
			if decErr == nil {
				warning := ""
				if detected.Confidence < 50 {
					warning = fmt.Sprintf("编码置信度较低（%d%%），已按 %s 读取", detected.Confidence, detected.Charset)
				}
				return Result{Text: string(decoded), Encoding: detected.Charset, Warning: warning}
			}
		}
	}
	// Short CJK samples are often mis-detected as Latin-1/Windows-1252. Try
	// the common East Asian encodings and keep the one that decodes with the
	// fewest replacement/control characters and the most CJK.
	if text, label, ok := decodeCJKBest(data); ok {
		return Result{Text: text, Encoding: label}
	}
	if err == nil && detected != nil {
		label := normalizeLabel(detected.Charset)
		if enc, getErr := htmlindex.Get(label); getErr == nil {
			decoded, decErr := enc.NewDecoder().Bytes(data)
			if decErr == nil {
				return Result{
					Text:     string(decoded),
					Encoding: detected.Charset,
					Warning:  fmt.Sprintf("编码置信度较低（%d%%），已按 %s 读取", detected.Confidence, detected.Charset),
				}
			}
		}
	}
	// Last resort: decode as Windows-1252 so the reader still shows something.
	if enc, e := htmlindex.Get("windows-1252"); e == nil {
		if decoded, e2 := enc.NewDecoder().Bytes(data); e2 == nil {
			return Result{
				Text:     string(decoded),
				Encoding: "Windows-1252",
				Warning:  "无法可靠识别文本编码，已回退到 Windows-1252",
			}
		}
	}
	return Result{
		Text:     string(bytes.ToValidUTF8(data, []byte("�"))),
		Encoding: "未知",
		Warning:  "文本编码无法识别，部分字符可能显示异常",
	}
}

func decodeBOM(data []byte) (string, string, bool) {
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		return string(data[3:]), "UTF-8 (BOM)", true
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		if len(data) < 4 {
			return "", "", false
		}
		return decodeUTF16(data[2:], false), "UTF-16LE (BOM)", true
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		if len(data) < 4 {
			return "", "", false
		}
		return decodeUTF16(data[2:], true), "UTF-16BE (BOM)", true
	}
	return "", "", false
}

func decodeUTF16(data []byte, bigEndian bool) string {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	runes := make([]rune, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		var r rune
		if bigEndian {
			r = rune(data[i])<<8 | rune(data[i+1])
		} else {
			r = rune(data[i+1])<<8 | rune(data[i])
		}
		if r >= 0xD800 && r <= 0xDBFF && i+3 < len(data) {
			var lo rune
			if bigEndian {
				lo = rune(data[i+2])<<8 | rune(data[i+3])
			} else {
				lo = rune(data[i+3])<<8 | rune(data[i+2])
			}
			if lo >= 0xDC00 && lo <= 0xDFFF {
				r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
				i += 2
			}
		}
		runes = append(runes, r)
	}
	return string(runes)
}

func isCJKLabel(label string) bool {
	switch normalizeLabel(label) {
	case "gb18030", "big5", "shift_jis", "euc-jp", "euc-kr":
		return true
	}
	return false
}

func decodeCJKBest(data []byte) (string, string, bool) {
	candidates := []string{"gb18030", "big5", "shift_jis", "euc-jp", "euc-kr"}
	bestScore := 0
	bestText, bestLabel := "", ""
	for _, label := range candidates {
		enc, err := htmlindex.Get(label)
		if err != nil {
			continue
		}
		decoded, err := enc.NewDecoder().Bytes(data)
		if err != nil {
			continue
		}
		score := scoreText(string(decoded))
		if bestText == "" || score < bestScore {
			bestScore = score
			bestText = string(decoded)
			bestLabel = label
		}
	}
	if bestText == "" || bestScore >= 0 {
		return "", "", false
	}
	return bestText, bestLabel, true
}

func scoreText(s string) int {
	invalid, cjk, control := 0, 0, 0
	for _, r := range s {
		switch {
		case r == '�':
			invalid++
		case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r),
			unicode.Is(unicode.Katakana, r), unicode.Is(unicode.Hangul, r):
			cjk++
		case unicode.IsControl(r) && r != '\n' && r != '\t':
			control++
		}
	}
	return invalid*20 + control*5 - cjk
}

func normalizeLabel(label string) string {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "gb18030", "gb2312", "gbk", "hz-gb-2312":
		return "gb18030"
	case "big5", "big-5", "big5-hkscs":
		return "big5"
	case "shift_jis", "sjis", "windows-31j":
		return "shift_jis"
	case "euc-jp":
		return "euc-jp"
	case "euc-kr":
		return "euc-kr"
	case "iso-8859-1", "latin1":
		return "windows-1252"
	case "utf-16le", "utf-16be", "utf-8":
		return strings.ToLower(strings.TrimSpace(label))
	}
	return strings.ToLower(strings.TrimSpace(label))
}

// NormalizeNewlines converts CRLF and CR to LF.
func NormalizeNewlines(data []byte) []byte {
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(data, []byte("\r"), []byte("\n"))
}
