package book

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
)

// ImageProvider supplies raw image bytes for a provider-specific key.
type ImageProvider interface {
	ImageBytes(path string) ([]byte, string, error)
}

// FileProvider supplies non-image resources by logical path.
type FileProvider interface {
	ReadFile(name string) ([]byte, error)
}

// LinkTarget is the resolved destination of a link found in chapter content.
type LinkTarget struct {
	Chapter  int    // reading-unit index; -1 when the target cannot be displayed
	Fragment string // anchor name inside the target document
	External bool   // points outside the book
	Missing  bool   // internal target file is absent
}

// LinkResolver lets a loader resolve links using its own container rules.
type LinkResolver func(baseHref, raw string) LinkTarget

// Book is a fully loaded, format-neutral ebook.
type Book struct {
	Path       string
	Title      string
	Author     string
	Language   string
	Identifier string
	Format     Format
	Unit       string // "章" default; "节" or "页" for other formats
	Mode       string // ModeText / ModeImage where applicable
	PageMode   bool   // chapters render as full-page images (comics)

	Chapters []Chapter
	TOC      []TOCEntry
	Warnings []string

	images ImageProvider
	files  FileProvider
	links  LinkResolver
	closer func() error
}

// SetImageProvider installs the loader's image source.
func (b *Book) SetImageProvider(p ImageProvider) { b.images = p }

// SetFileProvider installs the loader's auxiliary file source.
func (b *Book) SetFileProvider(p FileProvider) { b.files = p }

// SetCloser installs the resource cleanup callback used by Close.
func (b *Book) SetCloser(f func() error) { b.closer = f }

// SetLinkResolver installs a format-specific link resolver.
func (b *Book) SetLinkResolver(f LinkResolver) { b.links = f }

// Close releases resources owned by the loader.
func (b *Book) Close() error {
	if b.closer == nil {
		return nil
	}
	return b.closer()
}

// ImageBytes returns raw image data and its detected media type.
func (b *Book) ImageBytes(name string) ([]byte, string, error) {
	if b.images == nil {
		return nil, "", errors.New("没有图片数据源")
	}
	return b.images.ImageBytes(name)
}

// ReadFile returns a non-image resource from the book container.
func (b *Book) ReadFile(name string) ([]byte, error) {
	if b.files == nil {
		return nil, errors.New("没有文件数据源")
	}
	return b.files.ReadFile(name)
}

// ChapterByHref returns the chapter index for a logical path or pseudo-href.
func (b *Book) ChapterByHref(href string) int {
	for i := range b.Chapters {
		if b.Chapters[i].Href == href {
			return i
		}
	}
	return -1
}

// ResolveLink resolves a raw href found in content whose document is baseHref.
func (b *Book) ResolveLink(baseHref, raw string) LinkTarget {
	if b.links != nil {
		return b.links(baseHref, raw)
	}
	return b.genericResolveLink(baseHref, raw)
}

func (b *Book) genericResolveLink(baseHref, raw string) LinkTarget {
	target := LinkTarget{Chapter: -1}
	raw = strings.TrimSpace(SanitizeText(raw))
	if raw == "" {
		target.Missing = true
		return target
	}
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		target.Fragment = raw[i+1:]
		if decoded, err := url.PathUnescape(target.Fragment); err == nil {
			target.Fragment = decoded
		}
		raw = raw[:i]
	}
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		raw = raw[:i]
	}
	if raw == "" {
		target.Chapter = b.ChapterByHref(baseHref)
		if target.Chapter < 0 {
			target.Missing = true
		}
		return target
	}
	if strings.HasPrefix(raw, "//") {
		target.External = true
		return target
	}
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
		target.External = true
		return target
	}
	if decoded, err := url.PathUnescape(raw); err == nil {
		raw = decoded
	}
	baseDir := path.Dir(baseHref)
	if baseHref == "" || !strings.Contains(baseHref, "/") {
		baseDir = "."
	}
	joined := path.Clean(path.Join(baseDir, raw))
	if joined == "." || joined == ".." || strings.HasPrefix(joined, "../") || strings.HasPrefix(joined, "/") {
		target.Missing = true
		return target
	}
	target.Chapter = b.ChapterByHref(joined)
	if target.Chapter < 0 {
		target.Missing = true
	}
	return target
}

// UnitLabel returns the configured reading-unit label, or def when unset.
func (b *Book) UnitLabel(def string) string {
	if strings.TrimSpace(b.Unit) != "" {
		return b.Unit
	}
	if def == "" {
		return "章"
	}
	return def
}

// AddWarning appends a non-fatal loader warning.
func (b *Book) AddWarning(format string, args ...any) {
	b.Warnings = append(b.Warnings, fmt.Sprintf(format, args...))
}
