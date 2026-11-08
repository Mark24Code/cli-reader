package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	"cli-reader/internal/formats"
)

func stats(label string) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Printf("%s: heap=%dMB heapSys=%dMB sys=%dMB totalAlloc=%dMB\n",
		label, ms.HeapAlloc>>20, ms.HeapSys>>20, ms.Sys>>20, ms.TotalAlloc>>20)
}

func main() {
	path := os.Args[1]
	stats("before")

	start := time.Now()
	b, err := formats.OpenWithOptions(path, "", formats.Options{PDFMode: "image"})
	fmt.Printf("elapsed=%v err=%v\n", time.Since(start), err)
	if err != nil {
		return
	}

	runtime.GC()
	stats("open+gc (book live)")

	if f, e := os.Create("/tmp/azw3.heap.pprof"); e == nil {
		_ = pprof.Lookup("heap").WriteTo(f, 0)
		f.Close()
	}
	if f, e := os.Create("/tmp/azw3.alloc.pprof"); e == nil {
		_ = pprof.Lookup("allocs").WriteTo(f, 0)
		f.Close()
	}

	// Touch every chapter so lazy books actually load their content.
	nblocks, nspans := 0, 0
	touch := time.Now()
	for i := range b.Chapters {
		ch := b.EnsureChapter(i)
		if ch == nil {
			continue
		}
		nblocks += len(ch.Blocks)
		for _, blk := range ch.Blocks {
			nspans += len(blk.Spans)
		}
	}
	fmt.Printf("touched %d chapters in %v blocks=%d spans=%d\n", len(b.Chapters), time.Since(touch), nblocks, nspans)
	runtime.GC()
	stats("all chapters loaded")

	b.Close()
	runtime.GC()
	stats("after close+gc")
}
