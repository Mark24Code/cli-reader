package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"

	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const (
	maxImagePixels = 40_000_000
	maxImageSide   = 20_000
)

type thumbnail struct {
	lines []string
	cols  int
	rows  int
}

func decodeImage(data []byte) (image.Image, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("图片尺寸无效")
	}
	if cfg.Width > maxImageSide || cfg.Height > maxImageSide ||
		int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return nil, fmt.Errorf("图片尺寸过大 (%dx%d)", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

func makeThumbnail(img image.Image, maxCols, maxRows int) (thumbnail, error) {
	if img == nil {
		return thumbnail{}, fmt.Errorf("空图片")
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return thumbnail{}, fmt.Errorf("图片尺寸无效")
	}
	if maxCols < 1 {
		maxCols = 1
	}
	if maxRows < 1 {
		maxRows = 1
	}

	// A terminal cell is roughly twice as tall as it is wide, and each cell
	// contains two vertical pixels in half-block mode.
	cols := maxCols
	rows := int(math.Ceil(float64(cols) * float64(h) / (2.0 * float64(w))))
	if rows < 1 {
		rows = 1
	}
	if rows > maxRows {
		rows = maxRows
		cols = int(math.Round(float64(rows) * 2.0 * float64(w) / float64(h)))
		if cols < 1 {
			cols = 1
		}
		if cols > maxCols {
			cols = maxCols
		}
	}
	th := rows * 2
	dst := image.NewRGBA(image.Rect(0, 0, cols, th))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)

	const bgR, bgG, bgB = 0x21, 0x1f, 0x1c
	lines := make([]string, rows)
	for y := 0; y < rows; y++ {
		var line []byte
		for x := 0; x < cols; x++ {
			top := flatten(dst.RGBAAt(x, y*2), bgR, bgG, bgB)
			bottom := flatten(dst.RGBAAt(x, y*2+1), bgR, bgG, bgB)
			line = append(line, []byte(fmt.Sprintf("\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀",
				top.R, top.G, top.B, bottom.R, bottom.G, bottom.B))...)
		}
		line = append(line, []byte("\x1b[0m")...)
		lines[y] = string(line)
	}
	return thumbnail{lines: lines, cols: cols, rows: rows}, nil
}

func flatten(c color.RGBA, bgR, bgG, bgB uint8) color.RGBA {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	a := uint32(n.A)
	if a == 0xff {
		return color.RGBA{R: n.R, G: n.G, B: n.B, A: 0xff}
	}
	if a == 0 {
		return color.RGBA{R: bgR, G: bgG, B: bgB, A: 0xff}
	}
	mix := func(src, bg uint8) uint8 {
		return uint8((uint32(src)*a + uint32(bg)*(255-a)) / 255)
	}
	return color.RGBA{
		R: mix(n.R, bgR),
		G: mix(n.G, bgG),
		B: mix(n.B, bgB),
		A: 0xff,
	}
}
