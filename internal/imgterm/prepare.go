package imgterm

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"math"

	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const (
	maxDecodePixels = 40_000_000
	maxDecodeSide   = 20_000
	iterm2Limit     = 900_000
	kittyLimit      = 6_000_000
)

type Prepared struct {
	Data          []byte
	Format        string
	Width, Height int
}

// PrepareImage converts an image into a payload appropriate for the selected
// terminal protocol and keeps it under the protocol's transfer limit.
func PrepareImage(data []byte, protocol Protocol) (Prepared, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Prepared{}, fmt.Errorf("无法识别图片格式: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return Prepared{}, fmt.Errorf("图片尺寸无效")
	}
	if cfg.Width > maxDecodeSide || cfg.Height > maxDecodeSide ||
		int64(cfg.Width)*int64(cfg.Height) > maxDecodePixels {
		return Prepared{}, fmt.Errorf("图片尺寸过大 (%dx%d)", cfg.Width, cfg.Height)
	}

	switch protocol {
	case ProtocolITerm2:
		if (format == "png" || format == "jpeg" || format == "gif") &&
			base64Len(len(data)) <= iterm2Limit {
			return Prepared{Data: data, Format: format, Width: cfg.Width, Height: cfg.Height}, nil
		}
	case ProtocolKitty:
		if format == "png" && base64Len(len(data)) <= kittyLimit {
			return Prepared{Data: data, Format: format, Width: cfg.Width, Height: cfg.Height}, nil
		}
	default:
		return Prepared{}, fmt.Errorf("当前终端不支持原生图片协议")
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Prepared{}, err
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	alpha := hasAlpha(img)
	limit := iterm2Limit
	if protocol == ProtocolKitty {
		limit = kittyLimit
	}
	edges := []int{2400, 2000, 1600, 1400, 1200, 1000, 800, 600, 400}
	for _, edge := range edges {
		scaled := resizeToLimit(img, edge)
		if protocol == ProtocolKitty {
			encoded, err := encodePNG(scaled)
			if err == nil && base64Len(len(encoded)) <= limit {
				return Prepared{Data: encoded, Format: "png", Width: scaled.Bounds().Dx(), Height: scaled.Bounds().Dy()}, nil
			}
			continue
		}
		encoded, err := encodePNG(scaled)
		if err == nil && base64Len(len(encoded)) <= limit {
			return Prepared{Data: encoded, Format: "png", Width: scaled.Bounds().Dx(), Height: scaled.Bounds().Dy()}, nil
		}
		if alpha {
			flatten := flattenAlpha(scaled)
			encoded, err = encodeJPEG(flatten, 88)
			if err == nil && base64Len(len(encoded)) <= limit {
				return Prepared{Data: encoded, Format: "jpeg", Width: flatten.Bounds().Dx(), Height: flatten.Bounds().Dy()}, nil
			}
		} else {
			encoded, err = encodeJPEG(scaled, 90)
			if err == nil && base64Len(len(encoded)) <= limit {
				return Prepared{Data: encoded, Format: "jpeg", Width: scaled.Bounds().Dx(), Height: scaled.Bounds().Dy()}, nil
			}
		}
	}
	return Prepared{}, fmt.Errorf("图片在传输限制内仍无法完成编码 (%dx%d)", w, h)
}

func base64Len(n int) int {
	return base64.StdEncoding.EncodedLen(n)
}

func hasAlpha(img image.Image) bool {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	stepX := int(math.Max(1, float64(w)/200))
	stepY := int(math.Max(1, float64(h)/200))
	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		for x := b.Min.X; x < b.Max.X; x += stepX {
			_, _, _, a := img.At(x, y).RGBA()
			if a != 0xffff {
				return true
			}
		}
	}
	return false
}

func resizeToLimit(img image.Image, maxEdge int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxEdge && h <= maxEdge {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
		return dst
	}
	scale := float64(maxEdge) / float64(w)
	if hs := float64(maxEdge) / float64(h); hs < scale {
		scale = hs
	}
	nw := int(math.Round(float64(w) * scale))
	nh := int(math.Round(float64(h) * scale))
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	return dst
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.DefaultCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func flattenAlpha(img image.Image) *image.RGBA {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.RGBA{R: 0x21, G: 0x1f, B: 0x1c, A: 0xff}}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Over)
	return dst
}
