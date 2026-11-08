package mobibook

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildIdentityHuff builds a HUFF/CDIC pair where every byte is an 8-bit code
// that maps directly to itself. It is a valid, intentionally trivial table.
func buildIdentityHuff() ([]byte, []byte) {
	const headerLen = 24
	const dict1Off = headerLen
	const dict2Off = dict1Off + 256*4
	huff := make([]byte, dict2Off+64*4)
	copy(huff, "HUFF\x00\x00\x00\x18")
	binary.BigEndian.PutUint32(huff[8:], dict1Off)
	binary.BigEndian.PutUint32(huff[12:], dict2Off)
	for c := 0; c < 256; c++ {
		// codelen=8, term bit set, maxcode_high=2c so dictionary index == c.
		v := uint32(2*c)<<8 | 0x80 | 8
		binary.BigEndian.PutUint32(huff[dict1Off+c*4:], v)
	}

	const cdicHeader = 16
	const tableLen = 256 * 2
	var body bytes.Buffer
	offsets := make([]uint16, 256)
	for i := 0; i < 256; i++ {
		offsets[i] = uint16(tableLen + body.Len())
		var b [3]byte
		binary.BigEndian.PutUint16(b[:2], 0x8001) // literal, length 1
		b[2] = byte(i)
		body.Write(b[:])
	}
	cdic := make([]byte, cdicHeader+tableLen+body.Len())
	copy(cdic, "CDIC\x00\x00\x00\x10")
	binary.BigEndian.PutUint32(cdic[8:], 256)
	binary.BigEndian.PutUint32(cdic[12:], 8)
	for i, off := range offsets {
		binary.BigEndian.PutUint16(cdic[cdicHeader+i*2:], off)
	}
	copy(cdic[cdicHeader+tableLen:], body.Bytes())
	return huff, cdic
}

func TestHuffCDICRoundTrip(t *testing.T) {
	huff, cdic := buildIdentityHuff()
	r, err := newHuffReader(huff, [][]byte{cdic})
	if err != nil {
		t.Fatal(err)
	}
	text := []byte("Hello, 世界! 123")
	got, err := r.unpack(text)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, text) {
		t.Fatalf("decoded = %q, want %q", got, text)
	}
}
