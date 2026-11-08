package pdfview

import "testing"

func TestFitPageZoomFitsViewport(t *testing.T) {
	cases := []struct {
		imgW, imgH, width, bodyRows int
	}{
		{1000, 2000, 100, 50},
		{1000, 1414, 80, 20},
		{2000, 500, 100, 50},
	}
	for _, tc := range cases {
		z := fitPageZoom(tc.imgW, tc.imgH, tc.width, tc.bodyRows)
		if z <= 0 || z > 1 {
			t.Fatalf("zoom for %#v = %v, want (0,1]", tc, z)
		}
		v := &viewer{imgW: tc.imgW, imgH: tc.imgH, zoom: z}
		_, fullRows, _, _, maxScroll := v.geometry(tc.width, tc.bodyRows+6)
		if fullRows > tc.bodyRows || maxScroll != 0 {
			t.Fatalf("case %#v: fullRows=%d maxScroll=%d, want <=%d and 0",
				tc, fullRows, maxScroll, tc.bodyRows)
		}
	}
}

func TestFitPageZoomClamps(t *testing.T) {
	// Short pages do not grow past a full-width view.
	if got := fitPageZoom(2000, 500, 100, 50); got != 1 {
		t.Fatalf("short page zoom = %v, want 1", got)
	}
	// Extremely tall pages fall back to the minimum zoom.
	if got := fitPageZoom(10, 4000, 100, 50); got != 0.05 {
		t.Fatalf("tall page zoom = %v, want 0.05", got)
	}
	if got := fitPageZoom(0, 0, 100, 50); got != 1 {
		t.Fatalf("empty page zoom = %v, want 1", got)
	}
}
