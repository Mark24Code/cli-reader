package pdfbook

import (
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"

	"cli-reader/internal/book"
)

func glyph(x float64, s, font string) pdf.Text {
	return pdf.Text{X: x, Y: 100, W: 8, FontSize: 10, Font: font, S: s}
}

func TestBadGlyphAndCleanup(t *testing.T) {
	cases := []struct {
		text, font string
		bad        bool
	}{
		{"�", "SimSun", true},
		{"é", "SimSun", true},
		{"é", "TimesNewRomanPSMT", false},
		{"A", "SimSun", false},
		{"", "SimSun", true},
		{"\x9b", "SimSun", true},
	}
	for _, tc := range cases {
		if got := isBadGlyph(tc.text, tc.font); got != tc.bad {
			t.Errorf("isBadGlyph(%q, %q) = %v, want %v", tc.text, tc.font, got, tc.bad)
		}
	}
	if got := cleanGlyphText("a�bé", "SimSun"); got != "ab" {
		t.Fatalf("cleanGlyphText = %q", got)
	}
	if got := cleanGlyphText("a�bé", "TimesNewRomanPSMT"); got != "abé" {
		t.Fatalf("cleanGlyphText non-CJK = %q", got)
	}
}

func TestBuildTextRowKeepsDecreasingXOrder(t *testing.T) {
	items := []pdf.Text{
		glyph(100.0, "图", "SimSun"),
		glyph(99.6, "书", "SimSun"),
		glyph(99.2, "在", "SimSun"),
		glyph(98.8, "版", "SimSun"),
		glyph(139.0, "编", "SimSun"),
		glyph(148.0, "目", "SimSun"),
	}
	row, ok := buildTextRow(items)
	if !ok {
		t.Fatal("row not built")
	}
	if row.text != "图书在版编目" {
		t.Fatalf("row = %q", row.text)
	}
}

func TestBuildTextRowKeepsBacktrackedPunctuation(t *testing.T) {
	items := []pdf.Text{
		glyph(82.0, "进", "SimSun"),
		glyph(83.0, "行", "SimSun"),
		glyph(84.0, "自", "SimSun"),
		glyph(85.0, "学", "SimSun"),
		glyph(86.0, "习", "SimSun"),
		glyph(85.8, "。", "SimSun"),
		glyph(86.3, "相", "SimSun"),
		glyph(87.0, "比", "SimSun"),
	}
	row, ok := buildTextRow(items)
	if !ok {
		t.Fatal("row not built")
	}
	if row.text != "进行自学习。相比" {
		t.Fatalf("row = %q", row.text)
	}
}

func TestDetectTable(t *testing.T) {
	rows := []textRow{
		{y: 700, fontSize: 10, cells: []textCell{{text: "Year", x: 72}, {text: "Event", x: 200}}},
		{y: 685, fontSize: 10, cells: []textCell{{text: "first event line", x: 200}}},
		{y: 680, fontSize: 10, cells: []textCell{{text: "2020", x: 72}}},
		{y: 672, fontSize: 10, cells: []textCell{{text: "continues", x: 200}}},
		{y: 640, fontSize: 10, cells: []textCell{{text: "2021", x: 72}, {text: "second event", x: 200}}},
		{y: 580, fontSize: 10, cells: []textCell{{text: "paragraph after the table", x: 72}}},
	}
	end, block, ok := detectTable(rows, 0)
	if !ok {
		t.Fatal("table not detected")
	}
	if end != 5 {
		t.Fatalf("end = %d, want 5", end)
	}
	if block.Kind != book.BlockTable || len(block.Table) != 3 {
		t.Fatalf("block = %#v", block)
	}
	if !block.Table[0][0].Header || !block.Table[0][1].Header {
		t.Fatal("header row not marked")
	}
	got := func(row, col int) string {
		var s string
		for _, sp := range block.Table[row][col].Spans {
			s += sp.Text
		}
		return s
	}
	if got(1, 0) != "2020" || got(1, 1) != "first event line continues" {
		t.Fatalf("row 1 = %q / %q", got(1, 0), got(1, 1))
	}
	if got(2, 0) != "2021" || got(2, 1) != "second event" {
		t.Fatalf("row 2 = %q / %q", got(2, 0), got(2, 1))
	}
}

func TestDetectTableRejectsProse(t *testing.T) {
	rows := []textRow{
		{y: 700, fontSize: 10, cells: []textCell{{text: "This is a long prose start", x: 72}, {text: "and a second run", x: 200}}},
		{y: 680, fontSize: 10, cells: []textCell{{text: "2020", x: 72}}},
		{y: 660, fontSize: 10, cells: []textCell{{text: "2021", x: 72}, {text: "more prose", x: 200}}},
	}
	if _, _, ok := detectTable(rows, 0); ok {
		t.Fatal("prose was incorrectly detected as a table")
	}
}

func mathGlyph(x, y, fs float64, font, s string) pdf.Text {
	return pdf.Text{X: x, Y: y, FontSize: fs, Font: font, S: s}
}

func TestBuildFormulaRowSubscripts(t *testing.T) {
	italic := "TimesNewRomanPS-ItalicMT"
	regular := "TimesNewRomanPSMT"
	symbol := "SymbolMT"
	items := []pdf.Text{
		mathGlyph(100, 100, 9, italic, "w"),
		mathGlyph(103, 97.6, 5, regular, "1"),
		mathGlyph(108, 100, 9, italic, "x"),
		mathGlyph(111, 97.6, 5, regular, "1"),
		mathGlyph(118, 100, 9, symbol, "+"),
		mathGlyph(125, 100, 9, italic, "w"),
		mathGlyph(128, 97.6, 5, regular, "2"),
		mathGlyph(133, 100, 9, italic, "x"),
		mathGlyph(136, 97.6, 5, regular, "2"),
	}
	got, ok := buildFormulaRow(items)
	if !ok {
		t.Fatal("formula not detected")
	}
	if got != "w₁x₁ + w₂x₂" {
		t.Fatalf("formula = %q", got)
	}
}

func TestBuildFormulaRowMisplacedPlus(t *testing.T) {
	italic := "TimesNewRomanPS-ItalicMT"
	regular := "TimesNewRomanPSMT"
	symbol := "SymbolMT"
	items := []pdf.Text{
		mathGlyph(100, 100, 9, italic, "w"),
		mathGlyph(103, 97.6, 5, regular, "3"),
		mathGlyph(106, 100, 9, symbol, "+"),
		mathGlyph(107, 100, 9, italic, "x"),
		mathGlyph(110, 97.6, 5, regular, "3"),
		mathGlyph(120, 100, 9, italic, "b"),
	}
	got, ok := buildFormulaRow(items)
	if !ok {
		t.Fatal("formula not detected")
	}
	if got != "w₃x₃ + b" {
		t.Fatalf("formula = %q", got)
	}
}

func TestBuildFormulaRowEllipsis(t *testing.T) {
	italic := "TimesNewRomanPS-ItalicMT"
	regular := "TimesNewRomanPSMT"
	symbol := "SymbolMT"
	items := []pdf.Text{
		mathGlyph(100, 100, 9, italic, "x"),
		mathGlyph(103, 97.6, 5, regular, "2"),
		mathGlyph(106, 100, 9, symbol, "+"),
		mathGlyph(110, 100, 9, regular, "…"),
		mathGlyph(120, 100, 9, symbol, "+"),
		mathGlyph(125, 100, 9, italic, "w"),
		mathGlyph(128, 97.6, 5, regular, "n"),
		mathGlyph(133, 100, 9, italic, "x"),
		mathGlyph(136, 97.6, 5, regular, "n"),
		mathGlyph(145, 100, 9, symbol, "+"),
		mathGlyph(150, 100, 9, italic, "b"),
	}
	got, ok := buildFormulaRow(items)
	if !ok {
		t.Fatal("formula not detected")
	}
	if got != "x₂ + … + wₙxₙ + b" {
		t.Fatalf("formula = %q", got)
	}
}

func TestBuildFormulaRowRejectsProse(t *testing.T) {
	items := []pdf.Text{
		mathGlyph(100, 100, 9, "TimesNewRomanPSMT", "This"),
		mathGlyph(120, 100, 9, "TimesNewRomanPSMT", "is"),
		mathGlyph(140, 100, 9, "TimesNewRomanPSMT", "prose"),
	}
	if got, ok := buildFormulaRow(items); ok {
		t.Fatalf("prose was treated as formula: %q", got)
	}
}

func TestBuildTextRowInsertsWordGaps(t *testing.T) {
	items := []pdf.Text{
		{X: 0, Y: 100, W: 20, FontSize: 10, Font: "TimesNewRomanPSMT", S: "Hello"},
		{X: 40, Y: 100, W: 25, FontSize: 10, Font: "TimesNewRomanPSMT", S: "World"},
	}
	row, ok := buildTextRow(items)
	if !ok {
		t.Fatal("row not built")
	}
	if !strings.Contains(row.text, "Hello") || !strings.Contains(row.text, "World") {
		t.Fatalf("row = %q", row.text)
	}
	if !strings.Contains(row.text, "Hello ") {
		t.Fatalf("missing gap: %q", row.text)
	}
}
