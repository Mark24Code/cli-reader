package epub

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	"cli-reader/internal/book"
)

const (
	maxChapterBytes = 16 << 20
	maxImageBytes   = 32 << 20
	maxImageCache   = 32 << 20
)

var errNotFound = errors.New("file not found in epub")

func normaliseZipPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "/")
	p = path.Clean(p)
	if p == "." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

func (l *loader) findZipFile(name string) *zip.File {
	name = normaliseZipPath(name)
	if name == "" {
		return nil
	}
	if f := l.byName[name]; f != nil {
		return f
	}
	if decoded, err := url.PathUnescape(name); err == nil {
		decoded = normaliseZipPath(decoded)
		if f := l.byName[decoded]; f != nil {
			return f
		}
		name = decoded
	}
	if f := l.byLower[strings.ToLower(name)]; f != nil {
		return f
	}
	return nil
}

// resolveHref resolves an href found in baseFile (a zip path) to a normalised
// archive path plus fragment. External URLs and unsafe paths are rejected.
func (l *loader) resolveHref(baseFile, href string) (zipPath, fragment string, external bool, err error) {
	href = strings.TrimSpace(book.SanitizeText(href))
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
	if l.findZipFile(joined) == nil {
		return joined, fragment, false, errNotFound
	}
	return joined, fragment, false, nil
}

func (l *loader) readZip(name string, limit int64) ([]byte, error) {
	f := l.findZipFile(name)
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

// ReadFile implements book.FileProvider.
func (l *loader) ReadFile(name string) ([]byte, error) {
	return l.readZip(name, maxChapterBytes)
}

// ImageBytes implements book.ImageProvider.
func (l *loader) ImageBytes(name string) ([]byte, string, error) {
	name = normaliseZipPath(name)
	if name == "" {
		return nil, "", errNotFound
	}
	l.imageMu.Lock()
	if data, ok := l.imageCache[name]; ok {
		l.imageMu.Unlock()
		return data, book.MediaTypeFor(name, data), nil
	}
	l.imageMu.Unlock()

	data, err := l.readZip(name, maxImageBytes)
	if err != nil {
		return nil, "", err
	}
	mt := book.MediaTypeFor(name, data)

	l.imageMu.Lock()
	if l.imageCache == nil {
		l.imageCache = make(map[string][]byte)
	}
	if l.imageBytes+int64(len(data)) <= maxImageCache {
		l.imageCache[name] = data
		l.imageBytes += int64(len(data))
	} else {
		// Simple pressure valve: drop the cache rather than grow without bound.
		l.imageCache = map[string][]byte{name: data}
		l.imageBytes = int64(len(data))
	}
	l.imageMu.Unlock()
	return data, mt, nil
}
