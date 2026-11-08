// Package formats detects an ebook container and dispatches to its loader.
package formats

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"cli-reader/internal/book"
	"cli-reader/internal/cbzbook"
	"cli-reader/internal/epub"
	"cli-reader/internal/htmlbook"
	"cli-reader/internal/mdbook"
	"cli-reader/internal/mobibook"
	"cli-reader/internal/pdfbook"
	"cli-reader/internal/txtbook"
)

// ParseFormat maps a --format value to a Format. "auto" (and "") returns "".
func ParseFormat(s string) (book.Format, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return "", true
	case "epub":
		return book.FormatEPUB, true
	case "pdf":
		return book.FormatPDF, true
	case "txt", "text":
		return book.FormatTXT, true
	case "md", "markdown", "mdown", "mkd":
		return book.FormatMarkdown, true
	case "html", "htm", "xhtml", "xht":
		return book.FormatHTML, true
	case "cbz", "comic":
		return book.FormatCBZ, true
	case "mobi", "azw", "azw3", "prc", "kindle":
		return book.FormatMOBI, true
	}
	return "", false
}

// Options controls format-specific loading behaviour.
type Options struct {
	// PDFMode is "image" (default), "text" or "auto".
	PDFMode string
}

// Open loads path using override when set, or auto-detects the container.
func Open(name string, override book.Format) (*book.Book, error) {
	return OpenWithOptions(name, override, Options{PDFMode: "image"})
}

// OpenWithOptions loads a book with format-specific options.
func OpenWithOptions(name string, override book.Format, opts Options) (*book.Book, error) {
	format := override
	if format == "" {
		detected, err := Detect(name)
		if err != nil {
			return nil, err
		}
		format = detected
	}

	var (
		b   *book.Book
		err error
	)
	switch format {
	case book.FormatEPUB:
		b, err = epub.Open(name)
	case book.FormatTXT:
		b, err = txtbook.Open(name)
	case book.FormatHTML:
		b, err = htmlbook.Open(name)
	case book.FormatMarkdown:
		b, err = mdbook.Open(name)
	case book.FormatCBZ:
		b, err = cbzbook.Open(name)
	case book.FormatPDF:
		mode := strings.ToLower(strings.TrimSpace(opts.PDFMode))
		if mode == "text" {
			b, err = pdfbook.OpenText(name)
			break
		}
		b, err = pdfbook.OpenImage(name)
		if err != nil && !book.IsDRM(err) {
			textBook, textErr := pdfbook.OpenText(name)
			if textErr == nil {
				textBook.Warnings = append(textBook.Warnings,
					fmt.Sprintf("%v；已切换到文本模式；可安装 poppler / Ghostscript / MuPDF 后重试", err))
				b, err = textBook, nil
			}
		}
	case book.FormatMOBI:
		b, err = mobibook.Open(name)
	default:
		err = fmt.Errorf("%w: %s", book.ErrUnsupportedFormat, format)
	}
	if err != nil {
		return nil, err
	}
	b.Format = format
	return b, nil
}

// Detect determines the format from magic bytes first and extensions second.
func Detect(name string) (book.Format, error) {
	head, err := readHead(name, 16<<10)
	if err != nil {
		return "", err
	}
	if len(head) >= 4 && bytes.Equal(head[:4], []byte{'P', 'K', 0x03, 0x04}) {
		return detectZip(name)
	}
	if bytes.HasPrefix(head, []byte("%PDF-")) {
		return book.FormatPDF, nil
	}
	if len(head) >= 68 {
		typ := string(head[60:64])
		creator := string(head[64:68])
		if (typ == "BOOK" && creator == "MOBI") || (typ == "TEXt" && creator == "REAd") {
			return book.FormatMOBI, nil
		}
	}
	if format, ok := formatFromExtension(name); ok {
		return format, nil
	}
	if looksLikeText(head) {
		return book.FormatTXT, nil
	}
	return "", fmt.Errorf("%w: 无法识别 %s 的格式（支持 EPUB/PDF/TXT/Markdown/HTML/CBZ/MOBI/AZW3）",
		book.ErrUnsupportedFormat, filepath.Base(name))
}

func detectZip(name string) (book.Format, error) {
	zr, err := zip.OpenReader(name)
	if err != nil {
		return "", fmt.Errorf("无法读取 ZIP: %w", err)
	}
	defer zr.Close()
	hasEpubMarker := false
	images := 0
	for _, f := range zr.File {
		n := strings.TrimPrefix(strings.ReplaceAll(f.Name, "\\", "/"), "/")
		lower := strings.ToLower(n)
		if lower == "meta-inf/container.xml" {
			hasEpubMarker = true
		}
		if lower == "mimetype" && f.UncompressedSize64 > 0 && f.UncompressedSize64 < 128 {
			rc, err := f.Open()
			if err == nil {
				buf := make([]byte, 128)
				nread, _ := rc.Read(buf)
				rc.Close()
				if strings.Contains(strings.ToLower(string(buf[:nread])), "application/epub+zip") {
					hasEpubMarker = true
				}
			}
		}
		if strings.HasPrefix(lower, "__macosx/") || strings.HasSuffix(lower, "/") {
			continue
		}
		if isImagePath(lower) {
			images++
		}
	}
	if hasEpubMarker {
		return book.FormatEPUB, nil
	}
	if images > 0 {
		return book.FormatCBZ, nil
	}
	return "", fmt.Errorf("%w: ZIP 中既没有 EPUB 包，也没有图片页", book.ErrUnsupportedFormat)
}

func formatFromExtension(name string) (book.Format, bool) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".epub":
		return book.FormatEPUB, true
	case ".pdf":
		return book.FormatPDF, true
	case ".txt", ".text":
		return book.FormatTXT, true
	case ".md", ".markdown", ".mdown", ".mkd":
		return book.FormatMarkdown, true
	case ".html", ".htm", ".xhtml", ".xht":
		return book.FormatHTML, true
	case ".cbz", ".zip":
		return book.FormatCBZ, true
	case ".mobi", ".azw", ".azw3", ".prc":
		return book.FormatMOBI, true
	}
	return "", false
}

func isImagePath(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".jpe", ".gif", ".webp", ".bmp", ".tif", ".tiff":
		return true
	}
	return false
}

func readHead(name string, n int64) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	nread, err := f.Read(buf)
	if err != nil && nread == 0 {
		return nil, err
	}
	return buf[:nread], nil
}

func looksLikeText(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	if utf8.Valid(data) {
		return true
	}
	// Non-UTF-8 text (GBK/Big5/Shift_JIS etc.) usually has few control bytes.
	control := 0
	for _, b := range data {
		if b < 0x09 || (b > 0x0d && b < 0x20) {
			control++
		}
	}
	return control*100/len(data) < 5
}
