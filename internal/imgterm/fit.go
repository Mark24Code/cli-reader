package imgterm

import "math"

// FitImage returns a terminal cell box that preserves the image aspect ratio.
// A character cell is assumed to be roughly twice as tall as it is wide.
func FitImage(imgW, imgH, maxCols, maxRows int) (cols, rows int) {
	if imgW <= 0 || imgH <= 0 || maxCols <= 0 || maxRows <= 0 {
		return 0, 0
	}
	cols = maxCols
	rows = int(math.Ceil(float64(cols) * float64(imgH) / (2.0 * float64(imgW))))
	if rows < 1 {
		rows = 1
	}
	if rows > maxRows {
		rows = maxRows
		cols = int(math.Round(float64(rows) * 2.0 * float64(imgW) / float64(imgH)))
		if cols < 1 {
			cols = 1
		}
		if cols > maxCols {
			cols = maxCols
		}
	}
	return cols, rows
}
