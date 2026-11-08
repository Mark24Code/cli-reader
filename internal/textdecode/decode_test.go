package textdecode

import (
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestDecodeUTF8(t *testing.T) {
	got := Decode([]byte("hello 中文\r\nnext"))
	if got.Text != "hello 中文\nnext" {
		t.Fatalf("text = %q", got.Text)
	}
	if got.Encoding != "UTF-8" {
		t.Fatalf("encoding = %q", got.Encoding)
	}
}

func TestDecodeUTF16LEBOM(t *testing.T) {
	src := []byte("hello 中文")
	le := make([]byte, 0, len(src)*2+2)
	le = append(le, 0xFF, 0xFE)
	for _, r := range string(src) {
		le = append(le, byte(r), byte(r>>8))
	}
	got := Decode(le)
	if got.Text != "hello 中文" {
		t.Fatalf("text = %q", got.Text)
	}
	if !strings.Contains(got.Encoding, "UTF-16LE") {
		t.Fatalf("encoding = %q", got.Encoding)
	}
}

func TestDecodeGB18030(t *testing.T) {
	src := "这是一段中文测试文本，用来验证编码检测。"
	enc, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	got := Decode(enc)
	if got.Text != src {
		t.Fatalf("text = %q, want %q (encoding=%q, warning=%q)", got.Text, src, got.Encoding, got.Warning)
	}
}
