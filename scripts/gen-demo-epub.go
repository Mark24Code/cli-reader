//go:build ignore

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"claude-epub-reader/internal/testepub"
)

func main() {
	out := "testdata/demo.epub"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := testepub.WriteSample(out, testepub.DefaultOptions()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote", out)
}
