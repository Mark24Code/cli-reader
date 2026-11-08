package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"cli-reader/internal/book"
	"cli-reader/internal/buildinfo"
	"cli-reader/internal/formats"
	"cli-reader/internal/imgterm"
	"cli-reader/internal/pdfview"
	"cli-reader/internal/state"
	"cli-reader/internal/tui"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__view-image" {
		os.Exit(imgterm.RunViewer(os.Args[2:], os.Stdin, os.Stdout))
	}
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("cli-reader", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	stateFile := fs.String("state-file", "", "收藏与阅读进度文件路径")
	protocolFlag := fs.String("image-protocol", "auto", "图片协议: auto, iterm2, kitty, none")
	formatFlag := fs.String("format", "auto", "书籍格式: auto, epub, pdf, txt, md, html, cbz, mobi, azw3")
	pdfModeFlag := fs.String("pdf-mode", "image", "PDF 模式: image（默认，精确图片渲染）, text, auto")
	showVersion := fs.Bool("version", false, "显示版本")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: cli-reader [选项] <book>")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "在 Claude 风格的终端界面中阅读电子书（EPUB/PDF/TXT/Markdown/HTML/CBZ/MOBI/AZW3）。")
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
		fmt.Println("cli-reader", buildinfo.Version)
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
	format, ok := formats.ParseFormat(*formatFlag)
	if !ok {
		fmt.Fprintf(os.Stderr, "错误: 未知格式 %q\n", *formatFlag)
		return 2
	}
	pdfMode := strings.ToLower(strings.TrimSpace(*pdfModeFlag))
	switch pdfMode {
	case "image", "text", "auto":
	default:
		fmt.Fprintf(os.Stderr, "错误: 未知 PDF 模式 %q（可选 image/text/auto）\n", *pdfModeFlag)
		return 2
	}

	openBook := func(mode string) (*book.Book, error) {
		b, openErr := formats.OpenWithOptions(bookPath, format, formats.Options{PDFMode: mode})
		if openErr == nil {
			// Parsing PDFs (pdfcpu) and large images leaves unused heap behind;
			// return it to the OS before entering the reading loop.
			debug.FreeOSMemory()
		}
		return b, openErr
	}

	bk, err := openBook(pdfMode)
	if err != nil {
		switch {
		case book.IsDRM(err):
			fmt.Fprintln(os.Stderr, "无法打开书籍: 这本书受 DRM 保护，仅支持无 DRM 的书籍")
		case errors.Is(err, book.ErrUnsupportedFormat):
			fmt.Fprintln(os.Stderr, "无法打开书籍: 不支持的格式。支持 EPUB/PDF/TXT/Markdown/HTML/CBZ/MOBI/AZW3")
		default:
			fmt.Fprintf(os.Stderr, "无法打开书籍: %v\n", err)
		}
		return 1
	}
	defer func() {
		if bk != nil {
			_ = bk.Close()
		}
	}()
	printWarnings := func(b *book.Book) {
		for _, w := range b.Warnings {
			fmt.Fprintln(os.Stderr, "提示:", w)
		}
	}
	printWarnings(bk)

	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, "错误: 需要在交互式终端中运行")
		return 1
	}

	path := strings.TrimSpace(*stateFile)
	if path == "" {
		path = state.DefaultPath()
		if strings.TrimSpace(os.Getenv("CLI_READER_STATE")) == "" &&
			strings.TrimSpace(os.Getenv("CLI_EPUB_READER_STATE")) == "" {
			// Older releases and the previous program name used other locations.
			state.MigrateLegacy(path)
		}
	}
	store := state.Load(path)
	key := state.BookKey(bookPath)

	for {
		if bk.Format == book.FormatPDF && bk.Mode == book.ModeImage {
			if protocol == imgterm.ProtocolNone {
				fmt.Fprintln(os.Stderr, "提示: 当前终端不支持原生图片协议，PDF 图片模式不可用，已切换到文本模式")
				_ = bk.Close()
				bk, err = openBook("text")
				if err != nil {
					fmt.Fprintf(os.Stderr, "无法打开书籍: %v\n", err)
					return 1
				}
				printWarnings(bk)
				continue
			}
			bs := store.Book(key, bk.Path, bk.Title, bk.Author)
			initial := 1
			if idx := bk.ChapterByHref(bs.Last.Href); idx >= 0 {
				initial = idx + 1
			}
			last, action, viewErr := pdfview.Run(bk, protocol, initial)
			if last >= 1 && last <= len(bk.Chapters) {
				bs.Last = book.Position{Href: bk.Chapters[last-1].Href}
				if saveErr := store.Save(); saveErr != nil {
					fmt.Fprintf(os.Stderr, "保存阅读进度失败: %v\n", saveErr)
				}
			}
			if viewErr != nil {
				fmt.Fprintf(os.Stderr, "图片阅读器失败: %v\n", viewErr)
				return 1
			}
			if action == pdfview.ActionText {
				_ = bk.Close()
				bk, err = openBook("text")
				if err != nil {
					fmt.Fprintf(os.Stderr, "无法打开书籍: %v\n", err)
					return 1
				}
				printWarnings(bk)
				continue
			}
			return 0
		}

		model := tui.New(bk, store, key, protocol)
		program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
		final, runErr := program.Run()
		wantsImage := false
		if m, ok := final.(tui.Model); ok {
			if saveErr := m.Finalize(); saveErr != nil {
				fmt.Fprintf(os.Stderr, "保存阅读进度失败: %v\n", saveErr)
			}
			wantsImage = m.WantsImageMode()
		}
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "运行失败: %v\n", runErr)
			return 1
		}
		if wantsImage && bk.Format == book.FormatPDF {
			_ = bk.Close()
			bk, err = openBook("image")
			if err != nil {
				fmt.Fprintf(os.Stderr, "无法打开书籍: %v\n", err)
				return 1
			}
			printWarnings(bk)
			continue
		}
		return 0
	}
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
