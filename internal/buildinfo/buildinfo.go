// Package buildinfo carries release metadata compiled into the binary.
package buildinfo

// Version is overridden at build time with:
//
//	-ldflags "-X cli-epub-reader/internal/buildinfo.Version=v1.2.3"
var Version = "0.3.3"

const (
	Author   = "Mark24Code"
	Email    = "mark.zhangyoung@gmail.com"
	Homepage = "https://github.com/Mark24Code/cli-epub-reader"
)
