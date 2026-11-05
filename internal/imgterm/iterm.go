package imgterm

import (
	"encoding/base64"
	"fmt"
)

// ITerm2Sequence builds an OSC 1337 inline image sequence. data must already be
// prepared to a format iTerm2 supports and small enough for its transfer limit.
func ITerm2Sequence(data []byte, name string, cols, rows int) []byte {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	encName := base64.StdEncoding.EncodeToString([]byte(name))
	encData := base64.StdEncoding.EncodeToString(data)
	header := fmt.Sprintf("\x1b]1337;File=name=%s;size=%d;inline=1;width=%d;height=%d;preserveAspectRatio=1:",
		encName, len(data), cols, rows)
	out := make([]byte, 0, len(header)+len(encData)+1)
	out = append(out, header...)
	out = append(out, encData...)
	out = append(out, '\a')
	return out
}
