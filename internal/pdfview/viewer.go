// Package pdfview renders PDF pages full-screen with the terminal's native
// image protocol. It is used as the default PDF reading experience so opening a
// PDF does not require pressing Enter on a thumbnail first.
package pdfview

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"cli-reader/internal/book"
	"cli-reader/internal/imgterm"
)

// Action tells main what to do after the viewer exits.
type Action int

const (
	ActionQuit Action = iota
	ActionText
)

const (
	accent = "\x1b[38;2;217;119;87m"
	muted  = "\x1b[38;2;140;131;120m"
	dim    = "\x1b[38;2;100;95;90m"
	reset  = "\x1b[0m"
)

// Run displays PDF pages natively and returns the last page plus the requested
// next action.
func Run(b *book.Book, protocol imgterm.Protocol, initialPage int) (int, Action, error) {
	if b == nil || len(b.Chapters) == 0 {
		return 0, ActionQuit, errors.New("没有可显示的 PDF 页面")
	}
	if protocol == imgterm.ProtocolNone {
		return 0, ActionQuit, errors.New("当前终端不支持原生图片协议")
	}
	if initialPage < 1 {
		initialPage = 1
	}
	if initialPage > len(b.Chapters) {
		initialPage = len(b.Chapters)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return initialPage, ActionQuit, errors.New("需要在交互式终端中运行")
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return initialPage, ActionQuit, fmt.Errorf("无法切换到原始终端模式: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[?25l")
	defer fmt.Fprint(os.Stdout, "\x1b[0m\x1b[?25h\x1b[2J\x1b[H\x1b[?1049l")

	v := &viewer{
		b:        b,
		protocol: protocol,
		total:    len(b.Chapters),
		page:     initialPage,
		zoom:     1,
		cache:    make(map[string]imgterm.Prepared),
	}
	v.loadPage()
	v.draw()

	keys := make(chan byte, 32)
	readErr := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			c, err := reader.ReadByte()
			if err != nil {
				readErr <- err
				return
			}
			keys <- c
		}
	}()

	sizeCheck := time.NewTicker(500 * time.Millisecond)
	defer sizeCheck.Stop()
	lastW, lastH, _ := term.GetSize(int(os.Stdout.Fd()))

	draw := func() {
		switch v.uiMode {
		case uiNav:
			v.renderNav()
		case uiInput:
			v.renderInput()
		case uiHelp:
			v.renderHelp()
		default:
			v.render()
		}
	}

	pushHistory := func() {
		loc := viewLocation{page: v.page, scroll: v.scroll}
		if n := len(v.history); n > 0 && v.history[n-1] == loc {
			return
		}
		v.history = append(v.history, loc)
		if len(v.history) > 50 {
			v.history = v.history[len(v.history)-50:]
		}
	}
	goToPage := func(page int, push bool) {
		if page < 1 {
			page = 1
		}
		if page > v.total {
			page = v.total
		}
		if page == v.page {
			v.uiMode = uiNormal
			v.haveFrame = false
			draw()
			return
		}
		if push {
			pushHistory()
		}
		v.page = page
		v.scroll = 0
		v.loadPage()
		v.uiMode = uiNormal
		v.haveFrame = false
		draw()
	}
	nextPage := func(delta int) {
		// A spread whose right page is the last page already shows the end;
		// don't step onto that page alone.
		if delta > 0 && v.page+v.shown-1 >= v.total {
			return
		}
		next := v.page + delta*v.pageStep()
		if next < 1 {
			next = 1
		}
		if next > v.total {
			next = v.total
		}
		if next != v.page {
			goToPage(next, true)
		}
	}
	toggleSpread := func() {
		v.spread = !v.spread
		v.scroll = 0
		v.loadPage()
		v.uiMode = uiNormal
		v.haveFrame = false
		draw()
	}
	fitWidth := func() {
		v.zoom = 1
		v.scroll = 0
		v.haveFrame = false
		draw()
	}
	fitPage := func() {
		width, height := termSize()
		v.zoom = fitPageZoom(v.imgW, v.imgH, width, height-6)
		v.scroll = 0
		v.haveFrame = false
		draw()
	}
	scrollPage := func(dir int) {
		w, h := termSize()
		step := h / 5
		if step < 1 {
			step = 1
		}
		_, _, _, _, maxScroll := v.geometry(w, h)
		target := v.scroll + dir*step
		if target < 0 {
			target = 0
		}
		if target > maxScroll {
			target = maxScroll
		}
		if target != v.scroll {
			v.scroll = target
			draw()
		}
	}
	scrollViewport := func(dir int) {
		w, h := termSize()
		_, _, _, bodyRows, maxScroll := v.geometry(w, h)
		target := v.scroll + dir*bodyRows
		if target < 0 {
			target = 0
		}
		if target > maxScroll {
			target = maxScroll
		}
		if target != v.scroll {
			v.scroll = target
			draw()
		}
	}
	openNav := func() {
		v.uiMode = uiNav
		v.navCursor = v.page - 1
		v.navOffset = 0
		v.ensureNavCursor()
		draw()
	}
	openInput := func() {
		v.uiMode = uiInput
		v.input = ""
		v.inputErr = ""
		draw()
	}
	openHelp := func() {
		v.uiMode = uiHelp
		draw()
	}
	goBack := func() {
		if len(v.history) == 0 {
			return
		}
		loc := v.history[len(v.history)-1]
		v.history = v.history[:len(v.history)-1]
		v.page = loc.page
		v.scroll = loc.scroll
		v.loadPage()
		v.uiMode = uiNormal
		v.haveFrame = false
		v.scroll = loc.scroll
		draw()
	}

	for {
		select {
		case <-sizeCheck.C:
			if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil && (w != lastW || h != lastH) {
				lastW, lastH = w, h
				draw()
			}
		case <-readErr:
			return v.page, ActionQuit, nil
		case c := <-keys:
			switch v.uiMode {
			case uiHelp:
				v.uiMode = uiNormal
				v.haveFrame = false
				draw()
				continue
			case uiInput:
				switch c {
				case 0x1b:
					if readArrow(keys) == "" {
						v.uiMode = uiNormal
						v.haveFrame = false
						draw()
					}
				case 13, 10:
					page, err := strconv.Atoi(strings.TrimSpace(v.input))
					if err == nil && page >= 1 && page <= v.total {
						goToPage(page, true)
					} else {
						v.inputErr = fmt.Sprintf("请输入 1–%d 之间的页码", v.total)
						v.renderInput()
					}
				case 127, 8:
					if len(v.input) > 0 {
						v.input = v.input[:len(v.input)-1]
						v.inputErr = ""
						v.renderInput()
					}
				case 3:
					return v.page, ActionQuit, nil
				default:
					if c >= '0' && c <= '9' && len(v.input) < 6 {
						v.input += string(c)
						v.inputErr = ""
						v.renderInput()
					}
				}
				continue
			case uiNav:
				if c == 0x1b {
					switch readArrow(keys) {
					case "up":
						v.moveNav(-1)
					case "down":
						v.moveNav(1)
					case "pgup":
						_, h := termSize()
						v.moveNav(-(h - 6))
					case "pgdn":
						_, h := termSize()
						v.moveNav(h - 6)
					case "home":
						v.navCursor = 0
						v.ensureNavCursor()
						v.renderNav()
					case "end":
						v.navCursor = v.total - 1
						v.ensureNavCursor()
						v.renderNav()
					default:
						v.uiMode = uiNormal
						v.haveFrame = false
						draw()
					}
					continue
				}
				switch c {
				case 'k':
					v.moveNav(-1)
				case 'j':
					v.moveNav(1)
				case 'g':
					v.navCursor = 0
					v.ensureNavCursor()
					v.renderNav()
				case 'G':
					v.navCursor = v.total - 1
					v.ensureNavCursor()
					v.renderNav()
				case 13, 10:
					goToPage(v.navCursor+1, true)
				case 't', 'q', 'h':
					v.uiMode = uiNormal
					v.haveFrame = false
					draw()
				case 3:
					return v.page, ActionQuit, nil
				}
				continue
			}

			switch c {
			case 'q', 3:
				return v.page, ActionQuit, nil
			case 9:
				return v.page, ActionText, nil
			case 'n', 'l', ' ':
				if c == ' ' {
					scrollViewport(1)
				} else {
					nextPage(1)
				}
			case 'p', 'h':
				nextPage(-1)
			case 'j':
				scrollPage(1)
			case 'k':
				scrollPage(-1)
			case 's':
				toggleSpread()
			case 'w':
				fitWidth()
			case 'c':
				fitPage()
			case 'b':
				goBack()
			case 't':
				openNav()
			case '/', ':':
				openInput()
			case '?':
				openHelp()
			case 'g':
				v.scroll = 0
				draw()
			case 'G':
				v.scroll = 1 << 30
				draw()
			case '0':
				v.scroll = 0
				draw()
			case '=', '+':
				v.setZoom(0.25)
			case '-', '_':
				v.setZoom(-0.25)
			case 'z':
				v.zoom = 1
				v.scroll = 0
				draw()
			case 0x1b:
				switch readArrow(keys) {
				case "up":
					scrollPage(-1)
				case "down":
					scrollPage(1)
				case "left":
					nextPage(-1)
				case "right":
					nextPage(1)
				case "pgup":
					scrollViewport(-1)
				case "pgdn":
					scrollViewport(1)
				case "home":
					v.scroll = 0
					draw()
				case "end":
					v.scroll = 1 << 30
					draw()
				default:
					return v.page, ActionQuit, nil
				}
			}
		}
	}
}

func termSize() (int, int) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		w = 80
	}
	if err != nil || h <= 0 {
		h = 24
	}
	return w, h
}

func (v *viewer) draw() {
	switch v.uiMode {
	case uiNav:
		v.renderNav()
	case uiInput:
		v.renderInput()
	case uiHelp:
		v.renderHelp()
	default:
		v.render()
	}
}

func (v *viewer) ensureNavCursor() {
	if v.total <= 0 {
		return
	}
	if v.navCursor < 0 {
		v.navCursor = 0
	}
	if v.navCursor >= v.total {
		v.navCursor = v.total - 1
	}
	_, h := termSize()
	rows := h - 6
	if rows < 3 {
		rows = 3
	}
	if v.navCursor < v.navOffset {
		v.navOffset = v.navCursor
	}
	if v.navCursor >= v.navOffset+rows {
		v.navOffset = v.navCursor - rows + 1
	}
	if v.navOffset < 0 {
		v.navOffset = 0
	}
}

func (v *viewer) moveNav(delta int) {
	if v.total <= 0 {
		return
	}
	v.navCursor += delta
	v.ensureNavCursor()
	v.renderNav()
}

func (v *viewer) renderNav() {
	w, h := termSize()
	fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H\x1b[?25l")
	title := strings.ReplaceAll(book.SanitizeText(v.b.Title), "\n", " ")
	if title == "" {
		title = "PDF"
	}
	header := accent + "目录" + reset + " " + muted + "·" + reset + " " + "《" + title + "》"
	right := muted + fmt.Sprintf("%d/%d", v.page, v.total) + reset
	fmt.Fprintf(os.Stdout, "\x1b[1;2H%s%s%s", ansi.Truncate(header, max(1, w-ansi.StringWidth(right)-3), "…"), spaces(max(1, w-ansi.StringWidth(header)-ansi.StringWidth(right)-2)), right)
	fmt.Fprintf(os.Stdout, "\x1b[2;1H%s%s%s", dim, repeat("─", w), reset)

	v.ensureNavCursor()
	rows := h - 6
	if rows < 3 {
		rows = 3
	}
	const selectedBG = "\x1b[48;2;217;119;87m\x1b[30m"
	for i := 0; i < rows; i++ {
		idx := v.navOffset + i
		if idx >= v.total {
			break
		}
		marker := "  "
		if idx == v.page-1 {
			marker = "› "
		}
		line := fmt.Sprintf("  %s第 %d 页", marker, idx+1)
		row := 3 + i
		fmt.Fprintf(os.Stdout, "\x1b[%d;1H\x1b[2K", row)
		if idx == v.navCursor {
			fmt.Fprintf(os.Stdout, "%s%s%s", selectedBG, padRight(ansi.Truncate(line, w, "…"), w), reset)
		} else {
			fmt.Fprint(os.Stdout, ansi.Truncate(line, w, "…"))
		}
	}
	footer := "  ↑↓ 选择 · PgUp/PgDn 翻页 · Enter 跳转 · Esc 关闭"
	fmt.Fprintf(os.Stdout, "\x1b[%d;1H\x1b[2K%s%s%s", h-1, muted, ansi.Truncate(footer, w, "…"), reset)
}

func (v *viewer) renderInput() {
	w, h := termSize()
	fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H\x1b[?25l")
	header := accent + "跳转到页" + reset + " " + muted + "·" + reset + " " + fmt.Sprintf("1–%d", v.total)
	fmt.Fprintf(os.Stdout, "\x1b[1;2H%s", ansi.Truncate(header, w-2, "…"))
	fmt.Fprintf(os.Stdout, "\x1b[2;1H%s%s%s", dim, repeat("─", w), reset)
	fmt.Fprintf(os.Stdout, "\x1b[4;2H%s页码  %s❯%s %s", muted, accent, reset, v.input)
	if v.inputErr != "" {
		fmt.Fprintf(os.Stdout, "\x1b[6;2H\x1b[38;2;220;100;90m%s%s", ansi.Truncate(v.inputErr, w-2, "…"), reset)
	}
	footer := "  输入页码 · Enter 跳转 · Esc 取消"
	fmt.Fprintf(os.Stdout, "\x1b[%d;1H\x1b[2K%s%s%s", h-1, muted, ansi.Truncate(footer, w, "…"), reset)
}

func (v *viewer) renderHelp() {
	w, h := termSize()
	fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H\x1b[?25l")
	header := accent + "帮助" + reset + " " + muted + "·" + reset + " PDF 图片模式"
	fmt.Fprintf(os.Stdout, "\x1b[1;2H%s", ansi.Truncate(header, w-2, "…"))
	fmt.Fprintf(os.Stdout, "\x1b[2;1H%s%s%s", dim, repeat("─", w), reset)
	lines := []string{
		"↑ / ↓ 或 j / k    纵向滚动（约 20% 屏高）",
		"← / → 或 n / p    上一页 / 下一页（双页模式一次两页）",
		"s                 单页 / 双页切换",
		"w                 宽度撑满（100%，可纵向滚动）",
		"c                 整页居中（完整页面缩放到窗口内）",
		"PgUp / PgDn       整屏滚动",
		"- / =             缩小 / 放大",
		"g / G             页首 / 页尾",
		"t                 目录",
		"/ 或 :            跳转到页码",
		"b                 返回上一页",
		"Tab               切换到文本模式",
		"q / Esc           退出",
	}
	for i, line := range lines {
		row := 4 + i
		if row >= h-1 {
			break
		}
		fmt.Fprintf(os.Stdout, "\x1b[%d;2H%s", row, ansi.Truncate(line, w-2, "…"))
	}
	fmt.Fprintf(os.Stdout, "\x1b[%d;1H\x1b[2K%s%s%s", h-1, muted, "  任意键返回", reset)
}

func readArrow(keys <-chan byte) string {
	timer := time.NewTimer(30 * time.Millisecond)
	defer timer.Stop()
	var seq []byte
	for len(seq) < 2 {
		select {
		case b := <-keys:
			seq = append(seq, b)
		case <-timer.C:
			return ""
		}
	}
	if seq[0] != '[' && seq[0] != 'O' {
		return ""
	}
	switch seq[1] {
	case 'A':
		return "up"
	case 'B':
		return "down"
	case 'C':
		return "right"
	case 'D':
		return "left"
	case 'H':
		return "home"
	case 'F':
		return "end"
	case '5', '6':
		select {
		case b := <-keys:
			if b == '~' {
				if seq[1] == '5' {
					return "pgup"
				}
				return "pgdn"
			}
		case <-timer.C:
		}
	}
	return ""
}

type uiMode int

const (
	uiNormal uiMode = iota
	uiNav
	uiInput
	uiHelp
)

type viewLocation struct {
	page   int
	scroll int
}

type viewer struct {
	b        *book.Book
	protocol imgterm.Protocol
	total    int
	page     int
	scroll   int
	zoom     float64
	spread   bool // show two pages side by side
	shown    int  // number of pages currently composed (1 or 2)

	uiMode    uiMode
	navCursor int
	navOffset int
	input     string
	inputErr  string
	history   []viewLocation

	img     image.Image
	imgW    int
	imgH    int
	loadErr string
	cache   map[string]imgterm.Prepared

	haveFrame       bool
	lastPage        int
	lastDisplayCols int
	lastDisplayRows int
	lastTop         int
	lastLeft        int
}

func (v *viewer) loadPage() {
	v.img = nil
	v.imgW, v.imgH = 0, 0
	v.loadErr = ""
	v.shown = 1
	v.cache = make(map[string]imgterm.Prepared)
	if v.page < 1 || v.page > len(v.b.Chapters) {
		v.loadErr = "页码无效"
		return
	}
	left, err := v.decodePage(v.page)
	if err != nil {
		v.loadErr = err.Error()
		return
	}
	v.img = left
	if v.spread && v.page < len(v.b.Chapters) {
		// A page without an image on the right leaves the left page alone.
		if right, rerr := v.decodePage(v.page + 1); rerr == nil {
			v.img = composeSpread(left, right)
			v.shown = 2
		}
	}
	b := v.img.Bounds()
	v.imgW, v.imgH = b.Dx(), b.Dy()
}

// decodePage renders and decodes the page image for a 1-based page number.
func (v *viewer) decodePage(page int) (image.Image, error) {
	ch := &v.b.Chapters[page-1]
	var path string
	if len(ch.Blocks) > 0 && ch.Blocks[0].Image != nil {
		path = ch.Blocks[0].Image.Path
	}
	if path == "" {
		return nil, errors.New("这一页没有可显示的图片")
	}
	data, _, err := v.b.ImageBytes(path)
	if err != nil {
		return nil, fmt.Errorf("渲染页面失败: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("解码页面图片失败: %w", err)
	}
	return img, nil
}

// pageStep is how many pages one turn advances. Spread mode shows two pages,
// so it advances two at a time; a jump from the table of contents or the page
// prompt still goes to the exact requested page.
func (v *viewer) pageStep() int {
	if v.spread {
		return 2
	}
	return 1
}

// fitPageZoom returns the zoom that shows the whole page inside the viewport
// and leaves it centered. It never magnifies past 100% width, so fit-page is
// also a fit-width view for short pages.
func fitPageZoom(imgW, imgH, width, bodyRows int) float64 {
	if imgW <= 0 || imgH <= 0 || width <= 0 || bodyRows <= 0 {
		return 1
	}
	// Half a row of slack keeps geometry's rounding from producing a scrollbar.
	z := 2 * (float64(bodyRows) - 0.5) * float64(imgW) / (float64(width) * float64(imgH))
	if z > 1 {
		z = 1
	}
	if z < 0.05 {
		z = 0.05
	}
	return z
}

const minSpreadGutter = 8

var spreadGutterColor = color.RGBA{R: 96, G: 92, B: 88, A: 255}

// spreadGutter returns the pixel width of the gap between two pages. It scales
// with the combined page width so the gap stays a couple of cells wide once
// the spread is fitted to the terminal.
func spreadGutter(leftWidth, rightWidth int) int {
	return max(minSpreadGutter, (leftWidth+rightWidth)/50)
}

// composeSpread lays two page images side by side on a white canvas, centering
// the shorter page vertically and leaving a muted gutter between them.
func composeSpread(left, right image.Image) image.Image {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	lb, rb := left.Bounds(), right.Bounds()
	lw, lh := lb.Dx(), lb.Dy()
	rw, rh := rb.Dx(), rb.Dy()
	gutter := spreadGutter(lw, rw)
	width, height := lw+gutter+rw, max(lh, rh)
	if width <= 0 || height <= 0 {
		return left
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	// Page renders are usually light; a white canvas keeps the margins that
	// differently sized pages leave behind from showing as black bars.
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(dst, image.Rect(lw, 0, lw+gutter, height),
		image.NewUniform(spreadGutterColor), image.Point{}, draw.Src)
	ly := (height - lh) / 2
	draw.Draw(dst, image.Rect(0, ly, lw, ly+lh), left, lb.Min, draw.Src)
	ry := (height - rh) / 2
	draw.Draw(dst, image.Rect(lw+gutter, ry, lw+gutter+rw, ry+rh), right, rb.Min, draw.Src)
	return dst
}

// geometry returns the full-width page height in cells, the scrollable range,
// and the visible slice for the current terminal size.
func (v *viewer) geometry(width, height int) (cols, fullRows, displayRows, bodyRows, maxScroll int) {
	if v.zoom <= 0 {
		v.zoom = 1
	}
	cols = int(math.Round(float64(max(1, width)) * v.zoom))
	if cols < 1 {
		cols = 1
	}
	bodyRows = height - 6
	if bodyRows < 2 {
		bodyRows = 2
	}
	if v.imgW <= 0 || v.imgH <= 0 {
		return cols, 1, 1, bodyRows, 0
	}
	fullRows = int(math.Ceil(float64(cols) * float64(v.imgH) / (2.0 * float64(v.imgW))))
	if fullRows < 1 {
		fullRows = 1
	}
	displayRows = bodyRows
	if fullRows < displayRows {
		displayRows = fullRows
	}
	maxScroll = fullRows - bodyRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	return cols, fullRows, displayRows, bodyRows, maxScroll
}

func (v *viewer) setZoom(delta float64) {
	if v.img == nil {
		return
	}
	width, height, _ := term.GetSize(int(os.Stdout.Fd()))
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	_, oldFull, _, _, _ := v.geometry(width, height)
	frac := 0.0
	if oldFull > 0 {
		frac = float64(v.scroll) / float64(oldFull)
	}
	oldZoom := v.zoom
	v.zoom += delta
	if v.zoom < 0.25 {
		v.zoom = 0.25
	}
	if v.zoom > 4 {
		v.zoom = 4
	}
	if v.zoom == oldZoom {
		return
	}
	_, newFull, _, _, _ := v.geometry(width, height)
	v.scroll = int(math.Round(frac * float64(newFull)))
	v.render()
}

func (v *viewer) render() {
	width, height, _ := term.GetSize(int(os.Stdout.Fd()))
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}

	title := book.SanitizeText(v.b.Title)
	if title == "" {
		title = "PDF"
	}
	title = strings.ReplaceAll(title, "\n", " ")

	cols, fullRows, displayRows, bodyRows, maxScroll := v.geometry(width, height)
	if v.scroll > maxScroll {
		v.scroll = maxScroll
	}
	if v.scroll < 0 {
		v.scroll = 0
	}
	scrollPct := 0
	if maxScroll > 0 {
		scrollPct = int(math.Round(100 * float64(v.scroll) / float64(maxScroll)))
	}

	pageFrac := 0.0
	if maxScroll > 0 {
		pageFrac = float64(v.scroll) / float64(maxScroll)
	}
	overall := (float64(v.page-1) + pageFrac) / float64(v.total)
	if overall < 0 {
		overall = 0
	}
	if overall > 1 {
		overall = 1
	}
	filled := int(math.Round(overall * 8))
	if filled < 0 {
		filled = 0
	}
	if filled > 8 {
		filled = 8
	}
	bar := accent + repeat("▰", filled) + reset + dim + repeat("▱", 8-filled) + reset
	pageLabel := fmt.Sprintf("%d/%d", v.page, v.total)
	if v.shown > 1 {
		pageLabel = fmt.Sprintf("%d–%d/%d", v.page, v.page+v.shown-1, v.total)
	}
	right := bar + " " + muted + fmt.Sprintf("%.0f%% · %s", overall*100, pageLabel) + reset

	left := accent + "✻ cli-reader" + reset + " " + muted + "·" + reset + " " + "《" + title + "》"
	left = ansi.Truncate(left, max(1, width-ansi.StringWidth(right)-3), "…")
	mode := "单页"
	if v.spread {
		mode = "双页"
	}
	status := fmt.Sprintf("  %d%% 缩放 · 滚动 %d%% · %s · %s",
		int(math.Round(v.zoom*100)), scrollPct, mode, v.protocol.Label())
	footer := "  ↑↓ 滚动 · ←→ 翻页 · s 双页 · w 撑满 · c 居中 · -/= 缩放 · t 目录 · / 跳转 · Tab 文本 · q 退出"

	drawChrome := func(full bool) {
		if full {
			fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H")
		}
		fmt.Fprint(os.Stdout, "\x1b[?25l")
		pad := max(1, width-ansi.StringWidth(left)-ansi.StringWidth(right)-2)
		fmt.Fprintf(os.Stdout, "\x1b[1;1H\x1b[2K %s%s%s", left, spaces(pad), right)
		fmt.Fprintf(os.Stdout, "\x1b[2;1H\x1b[2K%s%s%s", dim, repeat("─", width), reset)
		if height >= 8 {
			fmt.Fprintf(os.Stdout, "\x1b[%d;1H\x1b[2K%s%s%s", height-2, muted, ansi.Truncate(status, width, "…"), reset)
		}
		fmt.Fprintf(os.Stdout, "\x1b[%d;1H\x1b[2K%s%s%s", height-1, muted, ansi.Truncate(footer, width, "…"), reset)
	}

	if v.loadErr != "" {
		drawChrome(true)
		drawError(v.loadErr)
		v.haveFrame = false
		return
	}
	if v.img == nil {
		drawChrome(true)
		drawError("正在准备页面…")
		v.haveFrame = false
		return
	}
	if width < 20 || height < 8 {
		drawChrome(true)
		drawError("终端窗口过小，请放大后继续")
		v.haveFrame = false
		return
	}

	displayCols := cols
	srcW := v.imgW
	x0 := 0
	leftCol := 1 + (width-cols)/2
	if cols > width {
		displayCols = width
		srcW = int(math.Round(float64(v.imgW) * float64(width) / float64(cols)))
		if srcW < 1 {
			srcW = 1
		}
		if srcW > v.imgW {
			srcW = v.imgW
		}
		x0 = (v.imgW - srcW) / 2
		leftCol = 1
	}
	if displayCols < 1 {
		displayCols = 1
	}
	if leftCol < 1 {
		leftCol = 1
	}

	y0 := 0
	srcH := v.imgH
	if fullRows > bodyRows {
		y0 = int(math.Round(float64(v.scroll) * float64(v.imgH) / float64(fullRows)))
		y1 := int(math.Round(float64(v.scroll+displayRows) * float64(v.imgH) / float64(fullRows)))
		if y0 < 0 {
			y0 = 0
		}
		if y1 > v.imgH {
			y1 = v.imgH
		}
		if y1 <= y0 {
			y1 = y0 + 1
		}
		srcH = y1 - y0
	} else {
		displayRows = fullRows
	}

	key := fmt.Sprintf("%d|%d|%.2f|%d|%d|%d|%d", v.page, v.scroll, v.zoom, displayCols, displayRows, srcW, srcH)
	prepared, ok := v.cache[key]
	if !ok {
		prepared, ok = v.prepareSlice(x0, y0, srcW, srcH)
		if !ok {
			drawChrome(true)
			drawError("准备页面图片失败")
			v.haveFrame = false
			return
		}
		if len(v.cache) > 8 {
			v.cache = make(map[string]imgterm.Prepared)
		}
		v.cache[key] = prepared
	}

	top := 3 + (bodyRows-displayRows)/2
	if top < 3 {
		top = 3
	}
	fullRedraw := !v.haveFrame || v.page != v.lastPage ||
		displayCols != v.lastDisplayCols || displayRows != v.lastDisplayRows ||
		top != v.lastTop || leftCol != v.lastLeft
	drawChrome(fullRedraw)

	fmt.Fprintf(os.Stdout, "\x1b[%d;%dH", top, leftCol)
	switch v.protocol {
	case imgterm.ProtocolITerm2:
		os.Stdout.Write(imgterm.ITerm2Sequence(prepared.Data, fmt.Sprintf("%s #%d", title, v.page), displayCols, displayRows))
	case imgterm.ProtocolKitty:
		if fullRedraw {
			fmt.Fprint(os.Stdout, "\x1b_Ga=d\x1b\\")
		}
		os.Stdout.Write(imgterm.KittySequenceWithID(prepared.Data, displayCols, displayRows, 1))
	}
	v.haveFrame = true
	v.lastPage = v.page
	v.lastDisplayCols = displayCols
	v.lastDisplayRows = displayRows
	v.lastTop = top
	v.lastLeft = leftCol
}

func (v *viewer) prepareSlice(x0, y0, srcW, srcH int) (imgterm.Prepared, bool) {
	if srcH < 1 || srcW < 1 || v.img == nil {
		return imgterm.Prepared{}, false
	}
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x0+srcW > v.imgW {
		srcW = v.imgW - x0
	}
	if y0+srcH > v.imgH {
		srcH = v.imgH - y0
	}
	if srcW < 1 || srcH < 1 {
		return imgterm.Prepared{}, false
	}
	dst := image.NewRGBA(image.Rect(0, 0, srcW, srcH))
	draw.Draw(dst, dst.Bounds(), v.img, image.Pt(x0, y0), draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return imgterm.Prepared{}, false
	}
	prepared, err := imgterm.PrepareImage(buf.Bytes(), v.protocol)
	if err != nil {
		return imgterm.Prepared{}, false
	}
	return prepared, true
}

func drawError(msg string) {
	msg = strings.ReplaceAll(book.SanitizeText(msg), "\n", " ")
	fmt.Fprintf(os.Stdout, "\x1b[4;2H%s%s%s", muted, ansi.Truncate(msg, 200, "…"), reset)
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return repeat(" ", n)
}

func padRight(s string, width int) string {
	w := ansi.StringWidth(s)
	if w >= width {
		return s
	}
	return s + spaces(width-w)
}

func repeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
