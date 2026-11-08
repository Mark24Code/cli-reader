package pdfbook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ledongthuc/pdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"cli-reader/internal/book"
)

// ErrImageModeUnsupported reports that no PDF page rasterizer is available.
var ErrImageModeUnsupported = errors.New("PDF 图片模式不可用")

type pageTool string

const (
	toolSips     pageTool = "sips"
	toolPdfToPpm pageTool = "pdftoppm"
	toolMutool   pageTool = "mutool"
	toolGS       pageTool = "gs"
	toolMagick   pageTool = "imagemagick"
)

type pageRenderer struct {
	tool pageTool
	bin  string
}

const (
	pageRenderDPI        = 150
	pageRenderMaxSide    = 2000
	pageImageMaxBytes    = 32 << 20
	pageImageCacheBytes  = 128 << 20
	pageRenderTimeout    = 60 * time.Second
	pageImageKeyPrefix   = "pdfpage:"
	extractedPagePattern = "_page_"
)

// OpenImage opens a PDF in page-image mode. Each page is rasterized on demand
// by a local renderer (macOS sips, poppler, MuPDF, Ghostscript or ImageMagick).
func OpenImage(name string) (*book.Book, error) {
	renderer, err := detectPageRenderer()
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxFileBytes {
		return nil, fmt.Errorf("PDF 超过 %d MiB 限制", maxFileBytes>>20)
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	r, err := pdf.NewReader(f, st.Size())
	if err != nil {
		f.Close()
		lower := strings.ToLower(err.Error())
		if err == pdf.ErrInvalidPassword || strings.Contains(lower, "encrypt") ||
			strings.Contains(lower, "password") || strings.Contains(lower, "decrypt") {
			return nil, fmt.Errorf("%w: PDF 已加密或需要密码", book.ErrDRM)
		}
		return nil, fmt.Errorf("无法解析 PDF: %w", err)
	}
	numPages := r.NumPage()
	title, author := pdfMetadata(r)
	f.Close()
	if numPages <= 0 {
		return nil, fmt.Errorf("PDF 没有页面")
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	}

	b := &book.Book{
		Path:     name,
		Title:    title,
		Author:   author,
		Format:   book.FormatPDF,
		Unit:     "页",
		Mode:     book.ModeImage,
		PageMode: true,
	}
	if numPages > maxPages {
		b.Warnings = append(b.Warnings,
			fmt.Sprintf("PDF 有 %d 页，超过上限，仅处理前 %d 页", numPages, maxPages))
		numPages = maxPages
	}
	provider := &pageImageProvider{
		path:     name,
		renderer: renderer,
		cache:    make(map[int][]byte),
	}
	// Probe the first page so an unusable renderer fails at open time (and can
	// fall back to text mode) rather than deep in the reading session.
	if _, _, err := provider.ImageBytes(pageImageKeyPrefix + "1"); err != nil {
		return nil, fmt.Errorf("PDF 图片渲染不可用: %w", err)
	}
	for n := 1; n <= numPages; n++ {
		key := fmt.Sprintf("%s%d", pageImageKeyPrefix, n)
		b.Chapters = append(b.Chapters, book.Chapter{
			Index:    n - 1,
			Href:     fmt.Sprintf("pdf:page-%04d", n),
			Title:    fmt.Sprintf("第 %d 页", n),
			PageMode: true,
			Blocks: []book.Block{{
				Kind: book.BlockImage,
				Image: &book.ImageRef{
					Path: key,
					Alt:  fmt.Sprintf("第 %d 页", n),
				},
			}},
		})
	}
	b.SetImageProvider(provider)
	b.EnsureTOC()
	return b, nil
}

// OpenText opens a PDF in reflowed text mode.
func OpenText(name string) (*book.Book, error) {
	b, err := Open(name)
	if err != nil {
		return nil, err
	}
	b.Mode = book.ModeText
	return b, nil
}

func detectPageRenderer() (*pageRenderer, error) {
	if runtime.GOOS == "darwin" {
		if p, err := exec.LookPath("sips"); err == nil {
			return &pageRenderer{tool: toolSips, bin: p}, nil
		}
	}
	candidates := []struct {
		tool  pageTool
		names []string
	}{
		{toolPdfToPpm, []string{"pdftoppm"}},
		{toolMutool, []string{"mutool"}},
		{toolGS, []string{"gs", "gswin64c", "gswin32c"}},
		{toolMagick, []string{"magick", "convert"}},
	}
	for _, c := range candidates {
		for _, name := range c.names {
			if p, err := exec.LookPath(name); err == nil {
				return &pageRenderer{tool: c.tool, bin: p}, nil
			}
		}
	}
	return nil, fmt.Errorf("%w：未找到 sips / pdftoppm / mutool / gs / ImageMagick", ErrImageModeUnsupported)
}

type pageImageProvider struct {
	path     string
	renderer *pageRenderer

	renderMu sync.Mutex
	mu       sync.Mutex
	cache    map[int][]byte
	bytes    int64
}

func (p *pageImageProvider) ImageBytes(key string) ([]byte, string, error) {
	n, err := parsePageKey(key)
	if err != nil {
		return nil, "", err
	}
	p.mu.Lock()
	if data, ok := p.cache[n]; ok {
		p.mu.Unlock()
		return data, "image/png", nil
	}
	p.mu.Unlock()

	data, err := p.render(n)
	if err != nil {
		return nil, "", err
	}
	p.mu.Lock()
	if p.bytes+int64(len(data)) <= pageImageCacheBytes {
		p.cache[n] = data
		p.bytes += int64(len(data))
	} else {
		p.cache = map[int][]byte{n: data}
		p.bytes = int64(len(data))
	}
	p.mu.Unlock()
	return data, "image/png", nil
}

func parsePageKey(key string) (int, error) {
	if !strings.HasPrefix(key, pageImageKeyPrefix) {
		return 0, fmt.Errorf("未知 PDF 图片键 %q", key)
	}
	n, err := strconv.Atoi(strings.TrimPrefix(key, pageImageKeyPrefix))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("PDF 页码无效: %q", key)
	}
	return n, nil
}

func (p *pageImageProvider) render(page int) ([]byte, error) {
	p.renderMu.Lock()
	defer p.renderMu.Unlock()

	tmp, err := os.MkdirTemp("", "cli-reader-pdfpage-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	out := filepath.Join(tmp, "page.png")

	ctx, cancel := context.WithTimeout(context.Background(), pageRenderTimeout)
	defer cancel()
	var cmd *exec.Cmd
	switch p.renderer.tool {
	case toolSips:
		pdfPath := filepath.Join(tmp,
			strings.TrimSuffix(filepath.Base(p.path), ".pdf")+extractedPagePattern+strconv.Itoa(page)+".pdf")
		if err := api.ExtractPagesFile(p.path, tmp, []string{strconv.Itoa(page)}, model.NewDefaultConfiguration()); err != nil {
			return nil, fmt.Errorf("提取第 %d 页失败: %w", page, err)
		}
		cmd = exec.CommandContext(ctx, p.renderer.bin,
			"-s", "format", "png", "-Z", strconv.Itoa(pageRenderMaxSide), pdfPath, "--out", out)
	case toolPdfToPpm:
		prefix := filepath.Join(tmp, "page")
		cmd = exec.CommandContext(ctx, p.renderer.bin,
			"-f", strconv.Itoa(page), "-l", strconv.Itoa(page),
			"-png", "-r", strconv.Itoa(pageRenderDPI), "-singlefile", p.path, prefix)
		out = prefix + ".png"
	case toolMutool:
		cmd = exec.CommandContext(ctx, p.renderer.bin,
			"draw", "-q", "-F", "png", "-r", strconv.Itoa(pageRenderDPI),
			"-o", out, p.path, strconv.Itoa(page))
	case toolGS:
		cmd = exec.CommandContext(ctx, p.renderer.bin,
			"-q", "-dNOPAUSE", "-dBATCH", "-sDEVICE=png16m", "-r"+strconv.Itoa(pageRenderDPI),
			"-dFirstPage="+strconv.Itoa(page), "-dLastPage="+strconv.Itoa(page),
			"-sOutputFile="+out, p.path)
	case toolMagick:
		src := fmt.Sprintf("%s[%d]", p.path, page-1)
		cmd = exec.CommandContext(ctx, p.renderer.bin,
			"-density", strconv.Itoa(pageRenderDPI), src,
			"-background", "white", "-alpha", "remove", "-colorspace", "sRGB", out)
	default:
		return nil, fmt.Errorf("%w：未知渲染器", ErrImageModeUnsupported)
	}
	if cmd == nil {
		return nil, fmt.Errorf("%w：渲染器未配置", ErrImageModeUnsupported)
	}
	var stderr strings.Builder
	cmd.Stderr = &limitedWriter{w: &stderr, n: 8 << 10}
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("渲染第 %d 页超时", page)
		}
		return nil, fmt.Errorf("渲染第 %d 页失败: %v %s", page, err, strings.TrimSpace(stderr.String()))
	}
	f, err := os.Open(out)
	if err != nil {
		return nil, fmt.Errorf("渲染第 %d 页没有输出: %w", page, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, pageImageMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("渲染第 %d 页得到空图片", page)
	}
	if len(data) > pageImageMaxBytes {
		return nil, fmt.Errorf("第 %d 页图片超过大小限制", page)
	}
	data, err = flattenPageOnWhite(data)
	if err != nil {
		return nil, fmt.Errorf("处理第 %d 页背景失败: %w", page, err)
	}
	return data, nil
}

// flattenPageOnWhite composites transparent page output onto a white canvas.
// PDF backends such as macOS sips produce RGBA pages with a transparent paper
// background, which would otherwise show the terminal's dark background.
func flattenPageOnWhite(data []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if opaque, ok := img.(interface{ Opaque() bool }); ok && opaque.Opaque() {
		return data, nil
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Over)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	n, err := l.w.Write(p)
	l.n -= n
	return n, err
}
