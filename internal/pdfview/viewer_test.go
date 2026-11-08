package pdfview

import (
	"testing"
	"time"
)

func TestReadArrow(t *testing.T) {
	cases := []struct {
		seq  []byte
		want string
	}{
		{[]byte{'[', 'A'}, "up"},
		{[]byte{'[', 'B'}, "down"},
		{[]byte{'[', 'C'}, "right"},
		{[]byte{'[', 'D'}, "left"},
		{[]byte{'O', 'A'}, "up"},
	}
	for _, tc := range cases {
		ch := make(chan byte, len(tc.seq))
		for _, b := range tc.seq {
			ch <- b
		}
		if got := readArrow(ch); got != tc.want {
			t.Errorf("readArrow(%q) = %q, want %q", tc.seq, got, tc.want)
		}
	}
}

func TestReadArrowEscapeAlone(t *testing.T) {
	start := time.Now()
	if got := readArrow(make(chan byte)); got != "" {
		t.Fatalf("readArrow(empty) = %q", got)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("escape timeout too long: %v", elapsed)
	}
}
