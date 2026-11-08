package pdfbook

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	bookmodel "cli-reader/internal/book"
)

func pdfString(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`)
	return replacer.Replace(s)
}

// pdfUTF16Hex encodes a PDF text string as UTF-16BE with a BOM.
func pdfUTF16Hex(s string) string {
	out := "FEFF"
	for _, r := range s {
		if r > 0xFFFF {
			r -= 0x10000
			hi := 0xD800 + (r >> 10)
			lo := 0xDC00 + (r & 0x3FF)
			out += fmt.Sprintf("%04X%04X", hi, lo)
			continue
		}
		out += fmt.Sprintf("%04X", r)
	}
	return "<" + out + ">"
}

func buildPDF(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 30), G: uint8(y * 30), B: 120, A: 255})
		}
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, img, nil); err != nil {
		t.Fatal(err)
	}

	content1 := fmt.Sprintf(`BT /F1 12 Tf 72 700 Td (%s) Tj ET
BT /F2 10 Tf 72 670 Td (%s) Tj ET
BT /F2 10 Tf 72 655 Td (%s) Tj ET
BT /F2 10 Tf 72 640 Td (%s) Tj ET
BT /F1 12 Tf 72 610 Td (%s) Tj ET
`,
		pdfString("Hello PDF text"),
		pdfString("func main() {"),
		pdfString(`    fmt.Println("hi")`),
		pdfString("}"),
		pdfString("Second paragraph."),
	)
	content2 := "q 200 0 0 200 100 400 cm /Im1 Do Q\n"

	objects := []string{
		`<< /Type /Catalog /Pages 2 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R /F2 6 0 R >> >> /Contents 7 0 R >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Im1 9 0 R >> >> /Contents 8 0 R >>`,
		`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		`<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>`,
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content1), content1),
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content2), content2),
		"", // image object filled below
		fmt.Sprintf("<< /Title %s /Author %s >>", pdfUTF16Hex("PDF 测试"), pdfUTF16Hex("作者丁")),
	}
	objects[8] = fmt.Sprintf(
		"<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream",
		jpg.Len(), jpg.String(),
	)

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, obj := range objects {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objects)+1)
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 10 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objects)+1, xref)
	return buf.Bytes()
}

func writePDF(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.pdf")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOpenTextAndCode(t *testing.T) {
	p := writePDF(t, buildPDF(t))
	b, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "PDF 测试" || b.Author != "作者丁" {
		t.Fatalf("metadata = %q / %q", b.Title, b.Author)
	}
	if b.Unit != "页" || len(b.Chapters) != 2 {
		t.Fatalf("unit=%q chapters=%d", b.Unit, len(b.Chapters))
	}
	if b.Chapters[0].Title != "第 1 页" || b.Chapters[1].Title != "第 2 页" {
		t.Fatalf("titles = %q / %q", b.Chapters[0].Title, b.Chapters[1].Title)
	}
	var code string
	var paras []string
	for _, blk := range b.Chapters[0].Blocks {
		switch blk.Kind {
		case bookmodel.BlockCode:
			code = blk.Code
		case bookmodel.BlockParagraph:
			if len(blk.Spans) > 0 {
				paras = append(paras, blk.Spans[0].Text)
			}
		}
	}
	if !strings.Contains(code, "func main()") || !strings.Contains(code, "fmt.Println") {
		t.Fatalf("code = %q", code)
	}
	joined := strings.Join(paras, " | ")
	if !strings.Contains(joined, "Hello PDF text") || !strings.Contains(joined, "Second paragraph.") {
		t.Fatalf("paragraphs = %q", joined)
	}
}

func TestOpenImagesAndPageMode(t *testing.T) {
	p := writePDF(t, buildPDF(t))
	b, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	page := b.Chapters[1]
	if !page.PageMode {
		t.Fatalf("image-only page should be PageMode: %#v", page.Blocks)
	}
	if len(page.Blocks) != 1 || page.Blocks[0].Image == nil {
		t.Fatalf("blocks = %#v", page.Blocks)
	}
	data, mt, err := b.ImageBytes(page.Blocks[0].Image.Path)
	if err != nil || len(data) == 0 || mt != "image/jpeg" {
		t.Fatalf("image = %d %q %v", len(data), mt, err)
	}
}

func TestEncryptedPDF(t *testing.T) {
	src := buildPDF(t)
	var enc bytes.Buffer
	conf := model.NewAESConfiguration("user", "owner", 256)
	if err := api.Encrypt(bytes.NewReader(src), &enc, conf); err != nil {
		t.Fatal(err)
	}
	p := writePDF(t, enc.Bytes())
	if _, err := Open(p); !bookmodel.IsDRM(err) {
		t.Fatalf("expected DRM, got %v", err)
	}
}

func TestTruncatedPDFDoesNotPanic(t *testing.T) {
	src := buildPDF(t)
	p := writePDF(t, src[:len(src)/2])
	if _, err := Open(p); err == nil {
		t.Fatal("truncated PDF should fail")
	}
}
