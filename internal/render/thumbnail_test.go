package render

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestMakeThumbnail(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 640, 360))
	for y := 0; y < 360; y++ {
		for x := 0; x < 640; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	th, err := makeThumbnail(img, 40, 12)
	if err != nil {
		t.Fatal(err)
	}
	if th.cols > 40 || th.rows > 12 || th.cols < 1 || th.rows < 1 {
		t.Fatalf("bad size %dx%d", th.cols, th.rows)
	}
	if len(th.lines) != th.rows {
		t.Fatalf("line count = %d rows = %d", len(th.lines), th.rows)
	}
	for _, line := range th.lines {
		if ansi.StringWidth(line) != th.cols {
			t.Fatalf("width = %d want %d", ansi.StringWidth(line), th.cols)
		}
		if !strings.Contains(line, "▀") {
			t.Fatal("half block missing")
		}
	}
}
