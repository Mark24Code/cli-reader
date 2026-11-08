package pdfbook

import (
	"bytes"
	"errors"
	"image"
	_ "image/png"
	"testing"

	bookmodel "cli-reader/internal/book"
)

func TestOpenImageMode(t *testing.T) {
	p := writePDF(t, buildPDF(t))
	b, err := OpenImage(p)
	if errors.Is(err, ErrImageModeUnsupported) {
		t.Skip("no PDF page renderer installed")
	}
	if err != nil {
		t.Fatal(err)
	}
	if b.Mode != bookmodel.ModeImage || !b.PageMode || b.Unit != "页" {
		t.Fatalf("mode=%q pageMode=%v unit=%q", b.Mode, b.PageMode, b.Unit)
	}
	if len(b.Chapters) != 2 {
		t.Fatalf("chapters = %d", len(b.Chapters))
	}
	if !b.Chapters[0].PageMode || len(b.Chapters[0].Blocks) != 1 || b.Chapters[0].Blocks[0].Image == nil {
		t.Fatalf("page chapter = %#v", b.Chapters[0])
	}
	data, mt, err := b.ImageBytes(b.Chapters[0].Blocks[0].Image.Path)
	if err != nil {
		t.Fatal(err)
	}
	if mt != "image/png" || len(data) == 0 {
		t.Fatalf("image = %d bytes, %q", len(data), mt)
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(data)); err != nil || format != "png" {
		t.Fatalf("decoded format = %q err=%v", format, err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if opaque, ok := img.(interface{ Opaque() bool }); !ok || !opaque.Opaque() {
		t.Fatal("page image still has a transparent background")
	}
	if r, g, b, a := img.At(0, 0).RGBA(); a != 0xffff || r != 0xffff || g != 0xffff || b != 0xffff {
		t.Fatalf("corner pixel = %d,%d,%d,%d, want opaque white", r, g, b, a)
	}
}

func TestOpenTextMode(t *testing.T) {
	p := writePDF(t, buildPDF(t))
	b, err := OpenText(p)
	if err != nil {
		t.Fatal(err)
	}
	if b.Mode != bookmodel.ModeText {
		t.Fatalf("mode = %q", b.Mode)
	}
}
