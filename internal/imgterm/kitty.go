package imgterm

import (
	"encoding/base64"
	"fmt"
	"strings"
)

const kittyChunkSize = 4096

// KittySequence builds a transmit-and-display sequence for a PNG. The payload
// is split into base64 chunks as required by the kitty graphics protocol.
func KittySequence(pngData []byte, cols, rows int) []byte {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	payload := base64.StdEncoding.EncodeToString(pngData)
	var b strings.Builder
	first := true
	for len(payload) > 0 {
		n := kittyChunkSize
		if n > len(payload) {
			n = len(payload)
		}
		chunk := payload[:n]
		payload = payload[n:]
		more := 0
		if len(payload) > 0 {
			more = 1
		}
		if first {
			fmt.Fprintf(&b, "\x1b_Ga=T,f=100,q=2,c=%d,r=%d,m=%d;%s\x1b\\", cols, rows, more, chunk)
			first = false
		} else {
			fmt.Fprintf(&b, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}
	return []byte(b.String())
}
