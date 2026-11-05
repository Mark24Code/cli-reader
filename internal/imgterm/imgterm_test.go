package imgterm

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestDetectProtocol(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want Protocol
	}{
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, ProtocolITerm2},
		{map[string]string{"LC_TERMINAL": "iTerm2"}, ProtocolITerm2},
		{map[string]string{"KITTY_WINDOW_ID": "1"}, ProtocolKitty},
		{map[string]string{"TERM": "xterm-kitty"}, ProtocolKitty},
		{map[string]string{"TERM_PROGRAM": "ghostty"}, ProtocolKitty},
		{map[string]string{"TERM_PROGRAM": "WezTerm"}, ProtocolITerm2},
		{map[string]string{"TERM": "xterm-256color"}, ProtocolNone},
	}
	for _, tc := range cases {
		got := DetectFrom(func(k string) string { return tc.env[k] })
		if got != tc.want {
			t.Errorf("DetectFrom(%v) = %v, want %v", tc.env, got, tc.want)
		}
	}
}

func TestFitImage(t *testing.T) {
	cols, rows := FitImage(640, 360, 80, 20)
	if cols < 1 || cols > 80 || rows < 1 || rows > 20 {
		t.Fatalf("fit out of bounds: %dx%d", cols, rows)
	}
	cols2, rows2 := FitImage(480, 720, 80, 20)
	if cols2 < 1 || cols2 > 80 || rows2 < 1 || rows2 > 20 {
		t.Fatalf("fit out of bounds: %dx%d", cols2, rows2)
	}
	if rows2 != 20 {
		t.Fatalf("tall image should hit max rows, got %d", rows2)
	}
}

func TestITerm2Sequence(t *testing.T) {
	data := []byte("fakepng")
	seq := string(ITerm2Sequence(data, "图片.png", 40, 12))
	if !strings.HasPrefix(seq, "\x1b]1337;File=") || !strings.HasSuffix(seq, "\a") {
		t.Fatalf("bad sequence: %q", seq)
	}
	if !strings.Contains(seq, "inline=1") || !strings.Contains(seq, "width=40") || !strings.Contains(seq, "height=12") {
		t.Fatalf("missing options: %q", seq)
	}
	parts := strings.SplitN(strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b]1337;File="), "\a"), ":", 2)
	if len(parts) != 2 {
		t.Fatal("missing data separator")
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil || string(decoded) != string(data) {
		t.Fatalf("payload decode failed: %v", err)
	}
}

func TestKittySequenceChunking(t *testing.T) {
	data := bytes.Repeat([]byte{0x42}, 10000)
	seq := string(KittySequence(data, 50, 20))
	if !strings.HasPrefix(seq, "\x1b_Ga=T,f=100") {
		t.Fatalf("bad start: %q", seq[:min(30, len(seq))])
	}
	if count := strings.Count(seq, "\x1b_G"); count < 2 {
		t.Fatalf("expected multiple chunks, got %d", count)
	}
	if !strings.HasSuffix(seq, "\x1b\\") {
		t.Fatal("missing terminator")
	}
}

func TestPrepareImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1200, 800))
	for y := 0; y < 800; y++ {
		for x := 0; x < 1200; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	p, err := PrepareImage(buf.Bytes(), ProtocolITerm2)
	if err != nil {
		t.Fatal(err)
	}
	if p.Width != 1200 || p.Height != 800 {
		t.Fatalf("dims = %dx%d", p.Width, p.Height)
	}
	if len(p.Data) > iterm2Limit {
		t.Fatalf("payload too large: %d", len(p.Data))
	}
	if _, err := PrepareImage(buf.Bytes(), ProtocolNone); err == nil {
		t.Fatal("expected protocol none error")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
