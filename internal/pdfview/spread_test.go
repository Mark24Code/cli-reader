package pdfview

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"

	"cli-reader/internal/book"
)

func solid(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestComposeSpreadSideBySide(t *testing.T) {
	red := color.RGBA{R: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	got := composeSpread(solid(10, 20, red), solid(30, 10, blue))
	// 10 + gutter(8) + 30 wide, as tall as the taller left page.
	if b := got.Bounds(); b.Dx() != 48 || b.Dy() != 20 {
		t.Fatalf("bounds = %v, want 48x20", b)
	}

	check := func(x, y int, want color.RGBA) {
		t.Helper()
		r, g, b, a := got.At(x, y).RGBA()
		gotColor := color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
		if gotColor != want {
			t.Errorf("pixel (%d,%d) = %v, want %v", x, y, gotColor, want)
		}
	}

	check(0, 0, red)
	check(9, 19, red)
	// Gutter columns 10–17 separate the pages for the full height.
	check(10, 0, spreadGutterColor)
	check(17, 19, spreadGutterColor)
	// The shorter right page is centered: rows 5–14, white above and below.
	check(18, 4, white)
	check(18, 5, blue)
	check(47, 14, blue)
	check(47, 15, white)
}

func TestSpreadGutterScales(t *testing.T) {
	if got := spreadGutter(10, 30); got != minSpreadGutter {
		t.Fatalf("small gutter = %d, want %d", got, minSpreadGutter)
	}
	if got := spreadGutter(2000, 2000); got != 80 {
		t.Fatalf("large gutter = %d, want 80", got)
	}
}

func TestComposeSpreadNil(t *testing.T) {
	left := solid(2, 2, color.White)
	if got := composeSpread(left, nil); got != left {
		t.Fatalf("composeSpread(left, nil) = %v, want left", got)
	}
	if got := composeSpread(nil, left); got != left {
		t.Fatalf("composeSpread(nil, left) = %v, want left", got)
	}
}

func TestPageStep(t *testing.T) {
	v := &viewer{}
	if got := v.pageStep(); got != 1 {
		t.Fatalf("single page step = %d, want 1", got)
	}
	v.spread = true
	if got := v.pageStep(); got != 2 {
		t.Fatalf("spread page step = %d, want 2", got)
	}
}

type mapImages map[string][]byte

func (m mapImages) ImageBytes(path string) ([]byte, string, error) {
	data, ok := m[path]
	if !ok {
		return nil, "", errors.New("missing image")
	}
	return data, "image/png", nil
}

func bookWithPages(t *testing.T, pages ...image.Image) *book.Book {
	t.Helper()
	b := &book.Book{}
	images := mapImages{}
	for i, img := range pages {
		key := fmt.Sprintf("page:%d", i+1)
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		images[key] = buf.Bytes()
		b.Chapters = append(b.Chapters, book.Chapter{
			Index: i,
			Blocks: []book.Block{{
				Kind:  book.BlockImage,
				Image: &book.ImageRef{Path: key},
			}},
		})
	}
	b.SetImageProvider(images)
	return b
}

func TestLoadPageSpread(t *testing.T) {
	b := bookWithPages(t,
		solid(10, 10, color.White),
		solid(12, 20, color.White),
		solid(10, 10, color.White),
	)
	v := &viewer{b: b, total: 3, page: 1, spread: true}

	v.loadPage()
	if v.loadErr != "" {
		t.Fatal(v.loadErr)
	}
	if v.shown != 2 || v.imgW != 30 || v.imgH != 20 {
		t.Fatalf("spread 1-2: shown=%d size=%dx%d", v.shown, v.imgW, v.imgH)
	}

	// The last page has no right-hand partner.
	v.page = 3
	v.loadPage()
	if v.shown != 1 || v.imgW != 10 || v.imgH != 10 {
		t.Fatalf("spread last: shown=%d size=%dx%d", v.shown, v.imgW, v.imgH)
	}

	v.spread = false
	v.page = 1
	v.loadPage()
	if v.shown != 1 || v.imgW != 10 || v.imgH != 10 {
		t.Fatalf("single: shown=%d size=%dx%d", v.shown, v.imgW, v.imgH)
	}
}
