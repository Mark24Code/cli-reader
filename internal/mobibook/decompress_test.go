package mobibook

import "testing"

func TestPalmDocDecompress(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"literal", []byte{0x09, 0x41, 0x42}, "\tAB"},
		{"copy", []byte{0x05, 'A', 'B', 'C', 'D', 'E', 0x80, 0x08}, "ABCDEEEE"},
		{"space", []byte{0xC1}, " A"},
		{"literal-run", []byte{0x02, 'h', 'i'}, "hi"},
	}
	for _, tc := range cases {
		got, err := palmDocDecompress(tc.in, 1<<20)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if string(got) != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
	if _, err := palmDocDecompress([]byte{0x80, 0x08}, 1<<20); err == nil {
		t.Fatal("back-reference before output should fail")
	}
}

func TestTrimTrailingData(t *testing.T) {
	// Writer-style trailing data: multibyte byte 0x00, strand bytes 0x82
	// 0x88, then the backwards size 0x83. flags 0b11.
	data := append([]byte("Hello world</p>"), 0x00, 0x82, 0x88, 0x83)
	got := trimTrailingData(data, 3)
	if string(got) != "Hello world</p>" {
		t.Fatalf("trimmed = %q", got)
	}
	if got := trimTrailingData([]byte("plain"), 0); string(got) != "plain" {
		t.Fatalf("flags=0 changed data: %q", got)
	}
}
