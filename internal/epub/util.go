package epub

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"unicode"
)

const (
	maxChapterBytes = 16 << 20
	maxImageBytes   = 32 << 20
	maxImageCache   = 64 << 20
)

var errNotFound = errors.New("file not found in epub")

// SanitizeText removes terminal control characters and normalises common
// whitespace. EPUBs are untrusted input and must never be able to inject ANSI
// escape sequences into the TUI.
func SanitizeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteByte('\n')
		case r == '\t':
			b.WriteByte('\t')
		case r == ' ' || r == ' ':
			b.WriteByte(' ')
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
			// Drop C0/C1 controls, including ESC and BEL.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CollapseSpace trims and collapses runs of whitespace into one space. It keeps
// explicit newlines so <br> can create intentional line breaks.
func CollapseSpace(s string) string {
	s = SanitizeText(s)
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if r == '\n' {
			b.WriteByte('\n')
			space = false
			continue
		}
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func normaliseZipPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "/")
	p = path.Clean(p)
	if p == "." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

func (b *Book) findZipFile(name string) *zip.File {
	name = normaliseZipPath(name)
	if name == "" {
		return nil
	}
	if f := b.byName[name]; f != nil {
		return f
	}
	if decoded, err := url.PathUnescape(name); err == nil {
		decoded = normaliseZipPath(decoded)
		if f := b.byName[decoded]; f != nil {
			return f
		}
		name = decoded
	}
	if f := b.byLower[strings.ToLower(name)]; f != nil {
		return f
	}
	return nil
}

// resolveHref resolves an href found in baseFile (a zip path) to a normalised
// archive path plus fragment. External URLs and unsafe paths are rejected.
func (b *Book) resolveHref(baseFile, href string) (zipPath, fragment string, external bool, err error) {
	href = strings.TrimSpace(SanitizeText(href))
	if href == "" {
		return "", "", false, errNotFound
	}
	if i := strings.IndexByte(href, '#'); i >= 0 {
		fragment = href[i+1:]
		if decoded, e := url.PathUnescape(fragment); e == nil {
			fragment = decoded
		}
		href = href[:i]
	}
	if i := strings.IndexByte(href, '?'); i >= 0 {
		href = href[:i]
	}
	if href == "" {
		return "", fragment, false, nil
	}
	if strings.HasPrefix(href, "//") {
		return "", fragment, true, nil
	}
	if u, e := url.Parse(href); e == nil && u.Scheme != "" {
		return "", fragment, true, nil
	}
	if decoded, e := url.PathUnescape(href); e == nil {
		href = decoded
	}
	baseDir := path.Dir(baseFile)
	if baseFile == "" {
		baseDir = "."
	}
	joined := normaliseZipPath(path.Join(baseDir, href))
	if joined == "" {
		return "", fragment, false, fmt.Errorf("unsafe href %q", href)
	}
	if b.findZipFile(joined) == nil {
		return joined, fragment, false, errNotFound
	}
	return joined, fragment, false, nil
}

func (b *Book) readZip(name string, limit int64) ([]byte, error) {
	f := b.findZipFile(name)
	if f == nil {
		return nil, errNotFound
	}
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("epub entry %s is too large (%d bytes)", name, f.UncompressedSize64)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("epub entry %s exceeds size limit", name)
	}
	return data, nil
}

func (b *Book) ReadFile(name string) ([]byte, error) {
	return b.readZip(name, maxChapterBytes)
}

// ImageBytes returns the raw image data and a detected media type.
func (b *Book) ImageBytes(name string) ([]byte, string, error) {
	name = normaliseZipPath(name)
	if name == "" {
		return nil, "", errNotFound
	}
	b.imageMu.Lock()
	if data, ok := b.imageCache[name]; ok {
		b.imageMu.Unlock()
		return data, mediaTypeFor(name, data), nil
	}
	b.imageMu.Unlock()

	data, err := b.readZip(name, maxImageBytes)
	if err != nil {
		return nil, "", err
	}
	mt := mediaTypeFor(name, data)

	b.imageMu.Lock()
	if b.imageCache == nil {
		b.imageCache = make(map[string][]byte)
	}
	if b.imageBytes+int64(len(data)) <= maxImageCache {
		b.imageCache[name] = data
		b.imageBytes += int64(len(data))
	} else {
		// Simple pressure valve: drop the cache rather than grow without bound.
		b.imageCache = map[string][]byte{name: data}
		b.imageBytes = int64(len(data))
	}
	b.imageMu.Unlock()
	return data, mt, nil
}

func mediaTypeFor(name string, data []byte) string {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg", ".jpe":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg", ".svgz":
		return "image/svg+xml"
	case ".bmp":
		return "image/bmp"
	}
	if len(data) > 0 {
		switch {
		case len(data) > 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
			return "image/png"
		case len(data) > 3 && data[0] == 0xff && data[1] == 0xd8:
			return "image/jpeg"
		case len(data) > 6 && string(data[:6]) == "GIF87a", len(data) > 6 && string(data[:6]) == "GIF89a":
			return "image/gif"
		case len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
			return "image/webp"
		}
	}
	return "application/octet-stream"
}
