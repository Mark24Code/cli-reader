package imgterm

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"image"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// RunViewer is the hidden helper subcommand. It draws one prepared image using
// the native terminal protocol, waits for a key, then restores the screen.
func RunViewer(args []string, stdin *os.File, stdout io.Writer) int {
	fs := flag.NewFlagSet("__view-image", flag.ContinueOnError)
	file := fs.String("file", "", "prepared image file")
	titleFlag := fs.String("title", "", "image title")
	proto := fs.String("protocol", "", "image protocol")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(os.Stderr, "missing --file")
		return 2
	}
	data, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取图片失败: %v\n", err)
		return 1
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		fmt.Fprintf(os.Stderr, "图片格式不受支持: %v\n", err)
		return 1
	}
	protocol := Protocol(*proto)
	if protocol == "" || protocol == "auto" {
		protocol = DetectProtocol()
	}
	if protocol == ProtocolNone {
		fmt.Fprintln(os.Stderr, "当前终端不支持原生图片协议")
		return 1
	}

	width, height := terminalSize(stdout)
	if width < 20 || height < 8 {
		fmt.Fprintln(os.Stderr, "终端窗口过小，无法查看图片")
		return 1
	}
	cols, rows := FitImage(cfg.Width, cfg.Height, width-6, height-8)
	if cols < 1 || rows < 1 {
		fmt.Fprintln(os.Stderr, "无法计算图片尺寸")
		return 1
	}

	oldState, rawErr := term.MakeRaw(int(stdin.Fd()))
	if rawErr != nil {
		fmt.Fprintf(os.Stderr, "无法切换到原始终端模式: %v\n", rawErr)
		return 1
	}
	defer term.Restore(int(stdin.Fd()), oldState)

	fmt.Fprint(stdout, "\x1b[?1049h\x1b[H\x1b[2J\x1b[?25l")
	defer fmt.Fprint(stdout, "\x1b[0m\x1b[?25h\x1b[2J\x1b[H\x1b[?1049l")

	title := safeText(*titleFlag)
	if title == "" {
		title = "图片"
	}
	accent := "\x1b[38;2;217;119;87m"
	muted := "\x1b[38;2;140;131;120m"
	reset := "\x1b[0m"
	fmt.Fprintf(stdout, "\x1b[1;2H%s✻ Claude%s%s  ·  图片查看%s",
		accent, reset, muted, reset)
	fmt.Fprintf(stdout, "\x1b[2;2H%s%s%s", muted, ansi.Truncate(title, width-4, "…"), reset)

	var seq []byte
	switch protocol {
	case ProtocolITerm2:
		seq = ITerm2Sequence(data, title, cols, rows)
	case ProtocolKitty:
		seq = KittySequence(data, cols, rows)
	}
	fmt.Fprintf(stdout, "\x1b[4;2H")
	_, _ = stdout.Write(seq)

	footer := fmt.Sprintf("  任意键返回  ·  %d×%d  ·  %s", cfg.Width, cfg.Height, protocol.Label())
	fmt.Fprintf(stdout, "\x1b[%d;1H\x1b[2K%s%s%s", height-1, muted, ansi.Truncate(footer, width, "…"), reset)

	reader := bufio.NewReader(stdin)
	_, _ = reader.ReadByte()
	return 0
}

func terminalSize(w io.Writer) (int, int) {
	type fder interface{ Fd() uintptr }
	if f, ok := w.(fder); ok {
		if width, height, err := term.GetSize(int(f.Fd())); err == nil {
			return width, height
		}
	}
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 100, 30
	}
	return width, height
}

func safeText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}
