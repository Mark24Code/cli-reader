package book

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

const (
	DefaultMaxImageBytes = 32 << 20
	DefaultMaxImageCache = 32 << 20
	DefaultMaxFileBytes  = 16 << 20
)

var errImageNotFound = errors.New("图片不存在")

// MediaTypeFor infers an image media type from its name and magic bytes.
func MediaTypeFor(name string, data []byte) string {
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
	case ".tif", ".tiff":
		return "image/tiff"
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
		case len(data) > 4 && (string(data[:4]) == "II*\x00" || string(data[:4]) == "MM\x00*"):
			return "image/tiff"
		}
	}
	return "application/octet-stream"
}

// NormalizeArchivePath cleans a slash-separated path inside a ZIP or a local
// subtree. It returns "" for paths that escape the root.
func NormalizeArchivePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "/")
	p = path.Clean(p)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

// ResolveLocalImage fills in Path for an image referenced by a relative path
// in a local HTML/Markdown file. Absolute paths, URLs that escape the subtree
// and external schemes are left without a Path.
func ResolveLocalImage(ref *ImageRef) {
	if ref == nil {
		return
	}
	src := strings.TrimSpace(ref.Source)
	if i := strings.IndexAny(src, "?#"); i >= 0 {
		src = src[:i]
	}
	if decoded, err := url.PathUnescape(src); err == nil {
		src = decoded
	}
	if src == "" || strings.HasPrefix(src, "/") {
		return
	}
	p := NormalizeArchivePath(src)
	if p == "" {
		return
	}
	ref.Path = p
	if ref.Alt == "" {
		base := path.Base(p)
		ref.Alt = strings.TrimSuffix(base, path.Ext(base))
	}
}

// ZipImages is an ImageProvider and FileProvider backed by a ZIP archive.
type ZipImages struct {
	zr      *zip.Reader
	byName  map[string]*zip.File
	byLower map[string]*zip.File

	maxImageBytes int64
	maxCacheBytes int64

	mu    sync.Mutex
	cache map[string][]byte
	bytes int64
}

// NewZipImages indexes a ZIP archive for image and file reads.
func NewZipImages(zr *zip.Reader) *ZipImages {
	z := &ZipImages{
		zr:            zr,
		byName:        make(map[string]*zip.File, len(zr.File)),
		byLower:       make(map[string]*zip.File, len(zr.File)),
		maxImageBytes: DefaultMaxImageBytes,
		maxCacheBytes: DefaultMaxImageCache,
		cache:         make(map[string][]byte),
	}
	for _, f := range zr.File {
		name := NormalizeArchivePath(f.Name)
		if name == "" {
			continue
		}
		if _, ok := z.byName[name]; !ok {
			z.byName[name] = f
		}
		lower := strings.ToLower(name)
		if _, ok := z.byLower[lower]; !ok {
			z.byLower[lower] = f
		}
	}
	return z
}

func (z *ZipImages) find(name string) *zip.File {
	name = NormalizeArchivePath(name)
	if name == "" {
		return nil
	}
	if f := z.byName[name]; f != nil {
		return f
	}
	if decoded, err := url.PathUnescape(name); err == nil {
		decoded = NormalizeArchivePath(decoded)
		if f := z.byName[decoded]; f != nil {
			return f
		}
		name = decoded
	}
	return z.byLower[strings.ToLower(name)]
}

func (z *ZipImages) read(name string, limit int64) ([]byte, error) {
	f := z.find(name)
	if f == nil {
		return nil, errImageNotFound
	}
	if f.Flags&0x1 != 0 {
		return nil, fmt.Errorf("%w: ZIP 条目已加密", ErrDRM)
	}
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("条目 %s 过大 (%d bytes)", name, f.UncompressedSize64)
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
		return nil, fmt.Errorf("条目 %s 超过大小限制", name)
	}
	return data, nil
}

// ImageBytes implements ImageProvider.
func (z *ZipImages) ImageBytes(name string) ([]byte, string, error) {
	key := NormalizeArchivePath(name)
	if key == "" {
		return nil, "", errImageNotFound
	}
	z.mu.Lock()
	if data, ok := z.cache[key]; ok {
		z.mu.Unlock()
		return data, MediaTypeFor(key, data), nil
	}
	z.mu.Unlock()

	data, err := z.read(key, z.maxImageBytes)
	if err != nil {
		return nil, "", err
	}
	mt := MediaTypeFor(key, data)

	z.mu.Lock()
	if z.bytes+int64(len(data)) <= z.maxCacheBytes {
		z.cache[key] = data
		z.bytes += int64(len(data))
	} else {
		z.cache = map[string][]byte{key: data}
		z.bytes = int64(len(data))
	}
	z.mu.Unlock()
	return data, mt, nil
}

// ReadFile implements FileProvider.
func (z *ZipImages) ReadFile(name string) ([]byte, error) {
	key := NormalizeArchivePath(name)
	if key == "" {
		return nil, errImageNotFound
	}
	return z.read(key, DefaultMaxFileBytes)
}

// DirImages is an ImageProvider for local files under a directory subtree.
type DirImages struct {
	root string

	maxImageBytes int64
	maxCacheBytes int64

	mu    sync.Mutex
	cache map[string][]byte
	bytes int64
}

// NewDirImages creates a provider rooted at dir. The root is resolved through
// symlinks so later containment checks are reliable.
func NewDirImages(dir string) *DirImages {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return &DirImages{
		root:          filepath.Clean(dir),
		maxImageBytes: DefaultMaxImageBytes,
		maxCacheBytes: DefaultMaxImageCache,
		cache:         make(map[string][]byte),
	}
}

func (d *DirImages) resolve(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") || filepath.IsAbs(name) {
		return "", errImageNotFound
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errImageNotFound
	}
	target := filepath.Join(d.root, filepath.FromSlash(clean))
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	rel, err := filepath.Rel(d.root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errImageNotFound
	}
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() {
		return "", errImageNotFound
	}
	return target, nil
}

// ImageBytes implements ImageProvider.
func (d *DirImages) ImageBytes(name string) ([]byte, string, error) {
	key := strings.ReplaceAll(name, "\\", "/")
	d.mu.Lock()
	if data, ok := d.cache[key]; ok {
		d.mu.Unlock()
		return data, MediaTypeFor(key, data), nil
	}
	d.mu.Unlock()

	target, err := d.resolve(key)
	if err != nil {
		return nil, "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, "", err
	}
	if info.Size() > d.maxImageBytes {
		return nil, "", fmt.Errorf("图片 %s 过大 (%d bytes)", key, info.Size())
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, "", err
	}
	mt := MediaTypeFor(key, data)

	d.mu.Lock()
	if d.bytes+int64(len(data)) <= d.maxCacheBytes {
		d.cache[key] = data
		d.bytes += int64(len(data))
	} else {
		d.cache = map[string][]byte{key: data}
		d.bytes = int64(len(data))
	}
	d.mu.Unlock()
	return data, mt, nil
}

// MapImages is a simple in-memory ImageProvider used by MOBI/AZW3.
type MapImages struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMapImages creates an empty in-memory image map.
func NewMapImages() *MapImages {
	return &MapImages{data: make(map[string][]byte)}
}

// Put stores image bytes under key.
func (m *MapImages) Put(key string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		m.data = make(map[string][]byte)
	}
	m.data[key] = data
}

// ImageBytes implements ImageProvider.
func (m *MapImages) ImageBytes(key string) ([]byte, string, error) {
	m.mu.RLock()
	data, ok := m.data[key]
	m.mu.RUnlock()
	if !ok {
		return nil, "", errImageNotFound
	}
	return data, MediaTypeFor(key, data), nil
}
