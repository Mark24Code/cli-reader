// Package cbzbook loads image-page ZIP archives (CBZ comics).
package cbzbook

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"cli-reader/internal/book"
)

const (
	maxPages      = 5000
	maxTotalBytes = 1 << 30
)

type comicInfo struct {
	Title       string `xml:"Title"`
	Series      string `xml:"Series"`
	Number      string `xml:"Number"`
	Writer      string `xml:"Writer"`
	LanguageISO string `xml:"LanguageISO"`
	Summary     string `xml:"Summary"`
	PageCount   int    `xml:"PageCount"`
}

// Open loads a CBZ/ZIP comic into the shared book model.
func Open(name string) (*book.Book, error) {
	zr, err := zip.OpenReader(name)
	if err != nil {
		return nil, fmt.Errorf("不是有效的 CBZ/ZIP 文件: %w", err)
	}
	entries, warning, err := collectPages(&zr.Reader)
	if err != nil {
		zr.Close()
		return nil, err
	}
	if len(entries) == 0 {
		zr.Close()
		return nil, fmt.Errorf("CBZ 中没有图片页")
	}

	info := readComicInfo(&zr.Reader)
	b := &book.Book{
		Path:     name,
		Title:    comicTitle(info, name),
		Author:   strings.TrimSpace(info.Writer),
		Language: strings.TrimSpace(info.LanguageISO),
		Format:   book.FormatCBZ,
		Unit:     "页",
		PageMode: true,
	}
	if warning != "" {
		b.Warnings = append(b.Warnings, warning)
	}
	total := int64(0)
	for i, e := range entries {
		if i >= maxPages {
			break
		}
		if total+int64(e.UncompressedSize64) > maxTotalBytes {
			b.Warnings = append(b.Warnings, "漫画解压后体积过大，已截断后续页面")
			break
		}
		total += int64(e.UncompressedSize64)
		pageName := book.NormalizeArchivePath(e.Name)
		b.Chapters = append(b.Chapters, book.Chapter{
			Index:    i,
			Href:     pageName,
			Title:    fmt.Sprintf("第 %d 页", i+1),
			PageMode: true,
			Blocks: []book.Block{{
				Kind: book.BlockImage,
				Image: &book.ImageRef{
					Path:   pageName,
					Source: e.Name,
					Alt:    strings.TrimSuffix(path.Base(pageName), path.Ext(pageName)),
				},
			}},
		})
	}
	if len(b.Chapters) == 0 {
		zr.Close()
		return nil, fmt.Errorf("CBZ 中没有可阅读的图片页")
	}
	if info.PageCount > 0 && info.PageCount != len(b.Chapters) {
		b.Warnings = append(b.Warnings,
			fmt.Sprintf("ComicInfo 标注 %d 页，实际找到 %d 页", info.PageCount, len(b.Chapters)))
	}
	b.SetImageProvider(book.NewZipImages(&zr.Reader))
	b.SetCloser(zr.Close)
	b.EnsureTOC()
	return b, nil
}

func collectPages(zr *zip.Reader) ([]*zip.File, string, error) {
	var pages []*zip.File
	warning := ""
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, "/") || strings.HasPrefix(lower, "__macosx/") {
			continue
		}
		base := path.Base(lower)
		if strings.HasPrefix(base, ".") || !isImagePath(lower) {
			continue
		}
		if f.Flags&0x1 != 0 {
			return nil, "", fmt.Errorf("%w: ZIP 条目已加密", book.ErrDRM)
		}
		pages = append(pages, f)
	}
	sort.SliceStable(pages, func(i, j int) bool {
		return naturalLess(pages[i].Name, pages[j].Name)
	})
	if len(pages) > maxPages {
		warning = fmt.Sprintf("图片页超过 %d，仅加载前 %d 页", maxPages, maxPages)
	}
	return pages, warning, nil
}

func isImagePath(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".jpe", ".gif", ".webp", ".bmp", ".tif", ".tiff":
		return true
	}
	return false
}

// naturalLess compares names so page10 sorts after page2.
func naturalLess(a, b string) bool {
	ar, br := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ar) && j < len(br) {
		if unicode.IsDigit(ar[i]) && unicode.IsDigit(br[j]) {
			si, sj := i, j
			for i < len(ar) && unicode.IsDigit(ar[i]) {
				i++
			}
			for j < len(br) && unicode.IsDigit(br[j]) {
				j++
			}
			na, _ := strconv.ParseUint(string(ar[si:i]), 10, 64)
			nb, _ := strconv.ParseUint(string(br[sj:j]), 10, 64)
			if na != nb {
				return na < nb
			}
			continue
		}
		la, lb := unicode.ToLower(ar[i]), unicode.ToLower(br[j])
		if la != lb {
			return la < lb
		}
		i++
		j++
	}
	return len(ar) < len(br)
}

func readComicInfo(zr *zip.Reader) comicInfo {
	for _, f := range zr.File {
		if !strings.EqualFold(path.Base(strings.ReplaceAll(f.Name, "\\", "/")), "ComicInfo.xml") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return comicInfo{}
		}
		data, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if err != nil {
			return comicInfo{}
		}
		var info comicInfo
		if err := xml.Unmarshal(data, &info); err != nil {
			return comicInfo{}
		}
		return info
	}
	return comicInfo{}
}

func comicTitle(info comicInfo, name string) string {
	if t := strings.TrimSpace(info.Title); t != "" {
		return t
	}
	if s := strings.TrimSpace(info.Series); s != "" {
		if n := strings.TrimSpace(info.Number); n != "" {
			return s + " #" + n
		}
		return s
	}
	return strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
}
