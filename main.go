package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"cli-epub-reader/internal/buildinfo"
	"cli-epub-reader/internal/epub"
	"cli-epub-reader/internal/imgterm"
	"cli-epub-reader/internal/state"
	"cli-epub-reader/internal/tui"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__view-image" {
		os.Exit(imgterm.RunViewer(os.Args[2:], os.Stdin, os.Stdout))
	}
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("cli-epub-reader", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	stateFile := fs.String("state-file", "", "收藏与阅读进度文件路径")
	protocolFlag := fs.String("image-protocol", "auto", "图片协议: auto, iterm2, kitty, none")
	showVersion := fs.Bool("version", false, "显示版本")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: cli-epub-reader [选项] <book.epub>")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "在 Claude 风格的终端界面中阅读 EPUB。")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Println("cli-epub-reader", buildinfo.Version)
		return 0
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	bookPath := fs.Arg(0)

	protocol, err := parseProtocol(*protocolFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		return 2
	}

	book, err := epub.Open(bookPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无法打开 EPUB: %v\n", err)
		return 1
	}
	defer book.Close()

	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, "错误: 需要在交互式终端中运行")
		return 1
	}

	path := strings.TrimSpace(*stateFile)
	if path == "" {
		path = state.DefaultPath()
	}
	store := state.Load(path)
	key := state.BookKey(bookPath)
	model := tui.New(book, store, key, protocol)
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := program.Run()
	if m, ok := final.(tui.Model); ok {
		if saveErr := m.Finalize(); saveErr != nil {
			fmt.Fprintf(os.Stderr, "保存阅读进度失败: %v\n", saveErr)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "运行失败: %v\n", err)
		return 1
	}
	return 0
}

func parseProtocol(s string) (imgterm.Protocol, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return imgterm.DetectProtocol(), nil
	case "iterm2", "iterm":
		return imgterm.ProtocolITerm2, nil
	case "kitty":
		return imgterm.ProtocolKitty, nil
	case "none", "off":
		return imgterm.ProtocolNone, nil
	default:
		return imgterm.ProtocolNone, fmt.Errorf("未知图片协议 %q", s)
	}
}
