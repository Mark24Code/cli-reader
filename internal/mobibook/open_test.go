package mobibook

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leotaku/mobi"
	"golang.org/x/text/language"

	bookmodel "cli-reader/internal/book"
)

func putU16(b []byte, v uint16) { binary.BigEndian.PutUint16(b, v) }
func putU32(b []byte, v uint32) { binary.BigEndian.PutUint32(b, v) }

func buildEXTH(title, author, lang string) []byte {
	var entries [][]byte
	add := func(typ uint32, value string) {
		data := []byte(value)
		e := make([]byte, 8+len(data))
		putU32(e, typ)
		putU32(e[4:], uint32(len(e)))
		copy(e[8:], data)
		entries = append(entries, e)
	}
	add(503, title)
	add(100, author)
	add(524, lang)
	var body []byte
	for _, e := range entries {
		body = append(body, e...)
	}
	total := 12 + len(body)
	pad := (4 - total%4) % 4
	out := make([]byte, total+pad)
	copy(out, "EXTH")
	putU32(out[4:], uint32(len(out)))
	putU32(out[8:], uint32(len(entries)))
	copy(out[12:], body)
	return out
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makePalmDB(records [][]byte) []byte {
	n := len(records)
	tableEnd := 78 + 8*n
	offset := tableEnd + 2
	total := offset
	for _, r := range records {
		total += len(r)
	}
	data := make([]byte, total)
	copy(data[:32], "MOBI 测试")
	copy(data[60:64], "BOOK")
	copy(data[64:68], "MOBI")
	putU16(data[76:78], uint16(n))
	pos := offset
	for i, rec := range records {
		hdr := 78 + i*8
		putU32(data[hdr:], uint32(pos))
		data[hdr+4] = 0
		data[hdr+5] = byte(i + 1)
		copy(data[pos:], rec)
		pos += len(rec)
	}
	return data
}

func buildMOBI(t *testing.T, compression, encryption uint16) []byte {
	t.Helper()
	html := `<html><body>
		<h1>第一章</h1><p>正文一。</p>
		<h1>第二章</h1><p>正文二。</p>
		<img recindex="00001" alt="插图">
	</body></html>`
	title := "MOBI 测试"
	author := "作者丁"
	lang := "zh"
	exth := buildEXTH(title, author, lang)
	fullName := []byte(title)
	headerLen := 232
	fullNameOffset := 16 + headerLen + len(exth)

	rec0 := make([]byte, 16+headerLen)
	putU16(rec0[0:], compression)
	putU32(rec0[4:], uint32(len(html)))
	putU16(rec0[8:], 1)
	putU16(rec0[10:], 4096)
	putU16(rec0[12:], encryption)
	copy(rec0[16:20], "MOBI")
	putU32(rec0[20:], uint32(headerLen))
	putU32(rec0[24:], 2)
	putU32(rec0[28:], 65001)
	putU32(rec0[36:], 6)
	putU32(rec0[80:], 2) // first non-book
	putU32(rec0[84:], uint32(fullNameOffset))
	putU32(rec0[88:], uint32(len(fullName)))
	putU32(rec0[108:], 2) // first image record
	putU32(rec0[128:], 0x40)
	putU32(rec0[164:], 0xFFFFFFFF)
	putU32(rec0[168:], 0xFFFFFFFF)
	putU32(rec0[172:], 0)
	putU32(rec0[240:], 0)
	rec0 = append(rec0, exth...)
	rec0 = append(rec0, fullName...)

	text := []byte(html)
	records := [][]byte{rec0, text, testPNG(t)}
	switch compression {
	case 2:
		records[1] = palmDocLiteral(text)
	case 17480:
		huff, cdic := buildIdentityHuff()
		putU32(rec0[112:], 3) // HuffmanRecordOffset
		putU32(rec0[116:], 2) // HuffmanRecordCount (HUFF + one CDIC)
		records = [][]byte{rec0, text, testPNG(t), huff, cdic}
	}
	return makePalmDB(records)
}

// palmDocLiteral encodes bytes so PalmDOC's LZ77 decoder reproduces them
// exactly, without attempting real compression.
func palmDocLiteral(data []byte) []byte {
	out := make([]byte, 0, len(data)+len(data)/4)
	for _, b := range data {
		if b == 0 || (b >= 0x09 && b < 0x80) {
			out = append(out, b)
		} else {
			out = append(out, 0x01, b)
		}
	}
	return out
}

func writeMOBI(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.mobi")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOpenMOBI(t *testing.T) {
	for _, compression := range []uint16{1, 2, 17480} {
		p := writeMOBI(t, buildMOBI(t, compression, 0))
		b, err := Open(p)
		if err != nil {
			t.Fatalf("compression %d: %v", compression, err)
		}
		if b.Title != "MOBI 测试" || b.Author != "作者丁" || b.Language != "zh" {
			t.Fatalf("metadata = %q / %q / %q", b.Title, b.Author, b.Language)
		}
		if len(b.Chapters) != 2 {
			t.Fatalf("compression %d chapters = %d", compression, len(b.Chapters))
		}
		if b.Chapters[0].Title != "第一章" || b.Chapters[1].Title != "第二章" {
			t.Fatalf("titles = %q / %q", b.Chapters[0].Title, b.Chapters[1].Title)
		}
		var imagePath string
		var text strings.Builder
		for _, ch := range b.Chapters {
			for _, blk := range ch.Blocks {
				if blk.Kind == bookmodel.BlockImage && blk.Image != nil {
					imagePath = blk.Image.Path
				}
				for _, sp := range blk.Spans {
					text.WriteString(sp.Text)
				}
			}
		}
		if !strings.Contains(text.String(), "正文一") || !strings.Contains(text.String(), "正文二") {
			t.Fatalf("text = %q", text.String())
		}
		if imagePath != "mobi:img/2" {
			t.Fatalf("image path = %q", imagePath)
		}
		data, mt, err := b.ImageBytes(imagePath)
		if err != nil || len(data) == 0 || mt != "image/png" {
			t.Fatalf("image = %d %q %v", len(data), mt, err)
		}
		if len(b.TOC) < 2 {
			t.Fatalf("toc = %#v", b.TOC)
		}
	}
}

func TestMOBIDRM(t *testing.T) {
	p := writeMOBI(t, buildMOBI(t, 1, 2))
	if _, err := Open(p); !bookmodel.IsDRM(err) {
		t.Fatalf("expected DRM, got %v", err)
	}
}

func TestMOBITruncated(t *testing.T) {
	data := buildMOBI(t, 1, 0)
	p := writeMOBI(t, data[:len(data)/2])
	if _, err := Open(p); err == nil {
		t.Fatal("truncated MOBI should fail")
	}
}

func TestOpenAZW3(t *testing.T) {
	src := mobi.Book{
		Title:    "AZW3 测试",
		Authors:  []string{"作者戊"},
		Language: language.Chinese,
		Chapters: []mobi.Chapter{
			{Title: "第一章", Chunks: mobi.Chunks("<p>正文一</p>")},
			{Title: "第二章", Chunks: mobi.Chunks("<p>正文二</p>")},
		},
	}
	db := src.Realize()
	var buf bytes.Buffer
	if err := db.Write(&buf); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "test.azw3")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "AZW3 测试" || b.Author != "作者戊" {
		t.Fatalf("metadata = %q / %q", b.Title, b.Author)
	}
	var text strings.Builder
	for _, ch := range b.Chapters {
		for _, blk := range ch.Blocks {
			for _, sp := range blk.Spans {
				text.WriteString(sp.Text)
			}
		}
	}
	if !strings.Contains(text.String(), "正文一") || !strings.Contains(text.String(), "正文二") {
		t.Fatalf("text = %q", text.String())
	}
}
