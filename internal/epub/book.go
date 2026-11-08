// Package epub parses EPUB 2/3 containers into the shared book model.
package epub

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"

	"golang.org/x/net/html/charset"

	"cli-reader/internal/book"
	"cli-reader/internal/htmlimport"
)

type containerXML struct {
	Rootfiles []struct {
		FullPath  string `xml:"full-path,attr"`
		MediaType string `xml:"media-type,attr"`
	} `xml:"rootfiles>rootfile"`
}

type opfXML struct {
	Version  string `xml:"version,attr"`
	Metadata struct {
		Titles      []string `xml:"title"`
		Creators    []string `xml:"creator"`
		Languages   []string `xml:"language"`
		Identifiers []string `xml:"identifier"`
	} `xml:"metadata"`
	Manifest struct {
		Items []opfItem `xml:"item"`
	} `xml:"manifest"`
	Spine struct {
		Toc   string       `xml:"toc,attr"`
		Items []opfItemRef `xml:"itemref"`
	} `xml:"spine"`
}

type opfItem struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

type opfItemRef struct {
	IDRef  string `xml:"idref,attr"`
	Linear string `xml:"linear,attr"`
}

// loader owns all ZIP-specific state. The shared book model is deliberately
// independent of it.
type loader struct {
	zr      *zip.Reader
	closer  func() error
	byName  map[string]*zip.File
	byLower map[string]*zip.File

	imageMu    sync.Mutex
	imageCache map[string][]byte
	imageBytes int64
}

// Open opens and parses an EPUB file.
func Open(filename string) (*book.Book, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	b, err := openReaderAt(f, st.Size(), filename)
	if err != nil {
		f.Close()
		return nil, err
	}
	b.SetCloser(f.Close)
	return b, nil
}

// OpenReaderAt parses an EPUB from an arbitrary reader. It is useful for tests
// and for embedding. The caller remains responsible for the underlying reader.
func OpenReaderAt(r io.ReaderAt, size int64, displayName string) (*book.Book, error) {
	return openReaderAt(r, size, displayName)
}

func openReaderAt(r io.ReaderAt, size int64, displayName string) (*book.Book, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("不是有效的 EPUB/ZIP 文件: %w", err)
	}
	l := &loader{
		zr:      zr,
		byName:  make(map[string]*zip.File, len(zr.File)),
		byLower: make(map[string]*zip.File, len(zr.File)),
	}
	for _, f := range zr.File {
		name := normaliseZipPath(f.Name)
		if name == "" {
			continue
		}
		if _, exists := l.byName[name]; !exists {
			l.byName[name] = f
		}
		lower := strings.ToLower(name)
		if _, exists := l.byLower[lower]; !exists {
			l.byLower[lower] = f
		}
	}

	warning, err := l.checkDRM()
	if err != nil {
		return nil, err
	}

	b := &book.Book{
		Path:   displayName,
		Format: book.FormatEPUB,
		Unit:   "章",
	}
	if warning != "" {
		b.Warnings = append(b.Warnings, warning)
	}
	if err := l.loadPackage(b); err != nil {
		return nil, err
	}
	b.SetImageProvider(l)
	b.SetFileProvider(l)
	b.SetLinkResolver(func(baseHref, raw string) book.LinkTarget {
		return l.resolveLink(b, baseHref, raw)
	})
	return b, nil
}

func (l *loader) loadPackage(b *book.Book) error {
	containerData, err := l.readZip("META-INF/container.xml", maxChapterBytes)
	if err != nil {
		return fmt.Errorf("缺少 META-INF/container.xml: %w", err)
	}
	var container containerXML
	if err := decodeXML(containerData, &container); err != nil {
		return fmt.Errorf("解析 container.xml 失败: %w", err)
	}
	opfPath := ""
	for _, rf := range container.Rootfiles {
		if rf.MediaType == "" || rf.MediaType == "application/oebps-package+xml" {
			opfPath, _, _, err = l.resolveHref("", rf.FullPath)
			if err == nil {
				break
			}
		}
	}
	if opfPath == "" {
		for name := range l.byName {
			if strings.EqualFold(path.Ext(name), ".opf") {
				opfPath = name
				break
			}
		}
	}
	if opfPath == "" {
		return fmt.Errorf("找不到 OPF 包文件")
	}

	opfData, err := l.readZip(opfPath, maxChapterBytes)
	if err != nil {
		return fmt.Errorf("读取 OPF 失败: %w", err)
	}
	var pkg opfXML
	if err := decodeXML(opfData, &pkg); err != nil {
		return fmt.Errorf("解析 OPF 失败: %w", err)
	}
	if len(pkg.Metadata.Titles) > 0 {
		b.Title = book.CollapseSpace(pkg.Metadata.Titles[0])
	}
	if len(pkg.Metadata.Creators) > 0 {
		creators := make([]string, 0, len(pkg.Metadata.Creators))
		for _, c := range pkg.Metadata.Creators {
			if c = book.CollapseSpace(c); c != "" {
				creators = append(creators, c)
			}
		}
		b.Author = strings.Join(creators, " / ")
	}
	if len(pkg.Metadata.Languages) > 0 {
		b.Language = book.CollapseSpace(pkg.Metadata.Languages[0])
	}
	if len(pkg.Metadata.Identifiers) > 0 {
		b.Identifier = book.CollapseSpace(pkg.Metadata.Identifiers[0])
	}
	if b.Title == "" {
		b.Title = strings.TrimSuffix(path.Base(b.Path), path.Ext(b.Path))
	}

	manifest := make(map[string]opfItem, len(pkg.Manifest.Items))
	for _, item := range pkg.Manifest.Items {
		manifest[item.ID] = item
	}

	navPath, ncxPath := "", ""
	for _, item := range pkg.Manifest.Items {
		for _, prop := range strings.Fields(item.Properties) {
			if prop == "nav" {
				if zp, _, _, e := l.resolveHref(opfPath, item.Href); e == nil {
					navPath = zp
				}
			}
		}
		if item.MediaType == "application/x-dtbncx+xml" {
			if zp, _, _, e := l.resolveHref(opfPath, item.Href); e == nil {
				ncxPath = zp
			}
		}
	}
	if pkg.Spine.Toc != "" {
		if item, ok := manifest[pkg.Spine.Toc]; ok {
			if zp, _, _, e := l.resolveHref(opfPath, item.Href); e == nil {
				ncxPath = zp
			}
		}
	}

	type spineSource struct {
		href string
		id   string
	}
	var sources []spineSource
	var spineBytes uint64
	for _, ref := range pkg.Spine.Items {
		item, ok := manifest[ref.IDRef]
		if !ok {
			b.Warnings = append(b.Warnings, fmt.Sprintf("spine 引用了不存在的 manifest 项 %q", ref.IDRef))
			continue
		}
		zp, _, _, err := l.resolveHref(opfPath, item.Href)
		if err != nil {
			b.Warnings = append(b.Warnings, fmt.Sprintf("跳过无法定位的章节 %q: %v", item.Href, err))
			continue
		}
		if item.MediaType != "" && item.MediaType != "application/xhtml+xml" && item.MediaType != "text/html" {
			b.Warnings = append(b.Warnings, fmt.Sprintf("跳过非 XHTML spine 项 %q (%s)", zp, item.MediaType))
			continue
		}
		if f := l.findZipFile(zp); f != nil {
			spineBytes += f.UncompressedSize64
		}
		sources = append(sources, spineSource{href: zp, id: ref.IDRef})
		b.Chapters = append(b.Chapters, book.Chapter{
			Index: len(b.Chapters),
			ID:    ref.IDRef,
			Href:  zp,
		})
	}
	if len(b.Chapters) == 0 {
		return fmt.Errorf("EPUB 中没有可阅读的 spine 章节")
	}
	// Parse small books eagerly (keeps simple documents and tests predictable);
	// larger books load one chapter at a time.
	lazy := len(b.Chapters) > 3 || spineBytes > 2<<20
	if !lazy {
		for i := range sources {
			data, err := l.readZip(sources[i].href, maxChapterBytes)
			if err != nil {
				b.Warnings = append(b.Warnings, fmt.Sprintf("读取章节失败 %q: %v", sources[i].href, err))
				continue
			}
			ch, err := l.parseChapter(data, i, sources[i].href)
			if err != nil {
				b.Warnings = append(b.Warnings, fmt.Sprintf("解析章节失败 %q: %v", sources[i].href, err))
				continue
			}
			ch.ID = sources[i].id
			ch.Index = i
			b.Chapters[i] = *ch
		}
	}

	var toc []book.TOCEntry
	if navPath != "" {
		if data, e := l.readZip(navPath, maxChapterBytes); e == nil {
			toc, e = l.parseNavTOC(data, navPath)
			if e != nil {
				b.Warnings = append(b.Warnings, fmt.Sprintf("解析 EPUB3 目录失败: %v", e))
			}
		}
	}
	if len(toc) == 0 && ncxPath != "" {
		if data, e := l.readZip(ncxPath, maxChapterBytes); e == nil {
			toc, e = l.parseNCXTOC(data, ncxPath)
			if e != nil {
				b.Warnings = append(b.Warnings, fmt.Sprintf("解析 NCX 目录失败: %v", e))
			}
		}
	}
	b.TOC = toc
	b.EnsureTOC()
	if lazy {
		b.SetChapterLoader(func(i int) error {
			if i < 0 || i >= len(sources) {
				return fmt.Errorf("章节索引无效")
			}
			data, err := l.readZip(sources[i].href, maxChapterBytes)
			if err != nil {
				return err
			}
			parsed, err := l.parseChapter(data, i, sources[i].href)
			if err != nil {
				return err
			}
			target := &b.Chapters[i]
			target.Blocks = parsed.Blocks
			target.Anchors = parsed.Anchors
			target.Heading = parsed.Heading
			target.DocTitle = parsed.DocTitle
			base := strings.TrimSuffix(path.Base(target.Href), path.Ext(target.Href))
			if target.Title == "" || target.Title == base {
				switch {
				case parsed.Heading != "":
					target.Title = parsed.Heading
				case parsed.DocTitle != "":
					target.Title = parsed.DocTitle
				default:
					target.Title = base
				}
				for j := range b.TOC {
					if b.TOC[j].Chapter == i && (b.TOC[j].Title == "" || b.TOC[j].Title == base) {
						b.TOC[j].Title = target.Title
					}
				}
			}
			return nil
		})
	}
	return nil
}

func (l *loader) parseChapter(data []byte, index int, href string) (*book.Chapter, error) {
	res, err := htmlimport.Parse(data, htmlimport.Options{
		Index: index,
		Href:  href,
		ResolveImage: func(ref *book.ImageRef) {
			l.resolveImage(href, ref)
		},
	})
	if err != nil {
		return nil, err
	}
	return res.Chapter, nil
}

func (l *loader) resolveImage(baseHref string, ref *book.ImageRef) {
	zp, _, external, err := l.resolveHref(baseHref, ref.Source)
	if err != nil || external {
		ref.External = external
		return
	}
	ref.Path = zp
	if ref.Alt == "" {
		ref.Alt = strings.TrimSuffix(path.Base(zp), path.Ext(zp))
	}
}

func (l *loader) resolveLink(b *book.Book, baseHref, raw string) book.LinkTarget {
	zipPath, fragment, external, err := l.resolveHref(baseHref, raw)
	target := book.LinkTarget{Chapter: -1, Fragment: fragment, External: external}
	if external {
		return target
	}
	if zipPath == "" {
		// A fragment-only link stays in the current document.
		if err == nil {
			target.Chapter = b.ChapterByHref(baseHref)
		}
		return target
	}
	if err != nil {
		target.Missing = true
		return target
	}
	target.Chapter = b.ChapterByHref(zipPath)
	return target
}

// checkDRM returns a non-fatal warning and an error when the container is
// protected. Only font obfuscation is tolerated.
func (l *loader) checkDRM() (string, error) {
	for _, f := range l.zr.File {
		if f.Flags&0x1 != 0 {
			return "", fmt.Errorf("%w: ZIP 条目已加密", book.ErrDRM)
		}
	}
	if f := l.findZipFile("META-INF/rights.xml"); f != nil && f.UncompressedSize64 > 0 {
		return "", fmt.Errorf("%w: 检测到 META-INF/rights.xml", book.ErrDRM)
	}
	data, err := l.readZip("META-INF/encryption.xml", maxChapterBytes)
	if err != nil {
		return "", nil
	}
	obfuscated := false
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.CharsetReader = charset.NewReaderLabel
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "无法完整解析 META-INF/encryption.xml", nil
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "EncryptedData":
			obfuscated = true
		case "EncryptionMethod":
			alg := ""
			for _, a := range start.Attr {
				if strings.EqualFold(a.Name.Local, "Algorithm") {
					alg = strings.TrimSpace(a.Value)
					break
				}
			}
			if !isFontObfuscation(alg) {
				return "", fmt.Errorf("%w: 正文内容已加密", book.ErrDRM)
			}
		}
	}
	if obfuscated {
		return "检测到字体混淆（不影响正文阅读）", nil
	}
	return "", nil
}

func isFontObfuscation(alg string) bool {
	switch alg {
	case "http://www.idpf.org/2008/embedding",
		"http://ns.adobe.com/pdf/enc#RC":
		return true
	}
	return false
}

func decodeXML(data []byte, v any) error {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.CharsetReader = charset.NewReaderLabel
	return dec.Decode(v)
}
