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

	"golang.org/x/net/html/charset"
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

// Open opens and parses an EPUB file.
func Open(filename string) (*Book, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	b, err := OpenReaderAt(f, st.Size(), filename)
	if err != nil {
		f.Close()
		return nil, err
	}
	b.closer = f.Close
	return b, nil
}

// OpenReaderAt parses an EPUB from an arbitrary reader. It is useful for tests
// and for embedding. The caller remains responsible for the underlying reader.
func OpenReaderAt(r io.ReaderAt, size int64, displayName string) (*Book, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("不是有效的 EPUB/ZIP 文件: %w", err)
	}
	b := &Book{
		Path:    displayName,
		zip:     zr,
		byName:  make(map[string]*zip.File, len(zr.File)),
		byLower: make(map[string]*zip.File, len(zr.File)),
	}
	for _, f := range zr.File {
		name := normaliseZipPath(f.Name)
		if name == "" {
			continue
		}
		if _, exists := b.byName[name]; !exists {
			b.byName[name] = f
		}
		lower := strings.ToLower(name)
		if _, exists := b.byLower[lower]; !exists {
			b.byLower[lower] = f
		}
	}

	if err := b.loadPackage(); err != nil {
		return nil, err
	}
	return b, nil
}

// Close releases the EPUB file, if it was opened by Open.
func (b *Book) Close() error {
	if b.closer != nil {
		return b.closer()
	}
	return nil
}

func (b *Book) loadPackage() error {
	containerData, err := b.readZip("META-INF/container.xml", maxChapterBytes)
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
			opfPath, _, _, err = b.resolveHref("", rf.FullPath)
			if err == nil {
				break
			}
		}
	}
	if opfPath == "" {
		for name := range b.byName {
			if strings.EqualFold(path.Ext(name), ".opf") {
				opfPath = name
				break
			}
		}
	}
	if opfPath == "" {
		return fmt.Errorf("找不到 OPF 包文件")
	}

	opfData, err := b.readZip(opfPath, maxChapterBytes)
	if err != nil {
		return fmt.Errorf("读取 OPF 失败: %w", err)
	}
	var pkg opfXML
	if err := decodeXML(opfData, &pkg); err != nil {
		return fmt.Errorf("解析 OPF 失败: %w", err)
	}
	if len(pkg.Metadata.Titles) > 0 {
		b.Title = CollapseSpace(pkg.Metadata.Titles[0])
	}
	if len(pkg.Metadata.Creators) > 0 {
		creators := make([]string, 0, len(pkg.Metadata.Creators))
		for _, c := range pkg.Metadata.Creators {
			if c = CollapseSpace(c); c != "" {
				creators = append(creators, c)
			}
		}
		b.Author = strings.Join(creators, " / ")
	}
	if len(pkg.Metadata.Languages) > 0 {
		b.Language = CollapseSpace(pkg.Metadata.Languages[0])
	}
	if len(pkg.Metadata.Identifiers) > 0 {
		b.Identifier = CollapseSpace(pkg.Metadata.Identifiers[0])
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
				if zp, _, _, e := b.resolveHref(opfPath, item.Href); e == nil {
					navPath = zp
				}
			}
		}
		if item.MediaType == "application/x-dtbncx+xml" {
			if zp, _, _, e := b.resolveHref(opfPath, item.Href); e == nil {
				ncxPath = zp
			}
		}
	}
	if pkg.Spine.Toc != "" {
		if item, ok := manifest[pkg.Spine.Toc]; ok {
			if zp, _, _, e := b.resolveHref(opfPath, item.Href); e == nil {
				ncxPath = zp
			}
		}
	}

	for _, ref := range pkg.Spine.Items {
		item, ok := manifest[ref.IDRef]
		if !ok {
			b.Warnings = append(b.Warnings, fmt.Sprintf("spine 引用了不存在的 manifest 项 %q", ref.IDRef))
			continue
		}
		zp, _, _, err := b.resolveHref(opfPath, item.Href)
		if err != nil {
			b.Warnings = append(b.Warnings, fmt.Sprintf("跳过无法定位的章节 %q: %v", item.Href, err))
			continue
		}
		if item.MediaType != "" && item.MediaType != "application/xhtml+xml" && item.MediaType != "text/html" {
			b.Warnings = append(b.Warnings, fmt.Sprintf("跳过非 XHTML spine 项 %q (%s)", zp, item.MediaType))
			continue
		}
		data, err := b.readZip(zp, maxChapterBytes)
		if err != nil {
			b.Warnings = append(b.Warnings, fmt.Sprintf("读取章节失败 %q: %v", zp, err))
			continue
		}
		ch, err := b.parseChapter(data, len(b.Chapters), zp)
		if err != nil {
			b.Warnings = append(b.Warnings, fmt.Sprintf("解析章节失败 %q: %v", zp, err))
			continue
		}
		ch.ID = ref.IDRef
		b.Chapters = append(b.Chapters, *ch)
	}
	if len(b.Chapters) == 0 {
		return fmt.Errorf("EPUB 中没有可阅读的 spine 章节")
	}

	var toc []TOCEntry
	if navPath != "" {
		if data, e := b.readZip(navPath, maxChapterBytes); e == nil {
			toc, e = b.parseNavTOC(data, navPath)
			if e != nil {
				b.Warnings = append(b.Warnings, fmt.Sprintf("解析 EPUB3 目录失败: %v", e))
			}
		}
	}
	if len(toc) == 0 && ncxPath != "" {
		if data, e := b.readZip(ncxPath, maxChapterBytes); e == nil {
			toc, e = b.parseNCXTOC(data, ncxPath)
			if e != nil {
				b.Warnings = append(b.Warnings, fmt.Sprintf("解析 NCX 目录失败: %v", e))
			}
		}
	}
	toc = b.resolveTOC(toc)
	if len(toc) == 0 {
		toc = b.fallbackTOC()
	} else {
		toc = b.fillMissingChapters(toc)
	}
	b.TOC = toc

	// Title priority: TOC, document <title>, first heading, filename.
	tocTitles := make(map[int]string)
	for _, entry := range toc {
		if entry.Chapter >= 0 {
			if _, ok := tocTitles[entry.Chapter]; !ok && entry.Title != "" {
				tocTitles[entry.Chapter] = entry.Title
			}
		}
	}
	for i := range b.Chapters {
		ch := &b.Chapters[i]
		ch.Index = i
		switch {
		case tocTitles[i] != "":
			ch.Title = tocTitles[i]
		case ch.Heading != "":
			ch.Title = ch.Heading
		case ch.DocTitle != "":
			ch.Title = ch.DocTitle
		default:
			base := path.Base(ch.Href)
			ch.Title = strings.TrimSuffix(base, path.Ext(base))
			if ch.Title == "" {
				ch.Title = fmt.Sprintf("第 %d 节", i+1)
			}
		}
	}
	return nil
}

func decodeXML(data []byte, v any) error {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.CharsetReader = charset.NewReaderLabel
	return dec.Decode(v)
}

// ChapterByHref returns the chapter index for a normalised zip path.
func (b *Book) ChapterByHref(href string) int {
	for i := range b.Chapters {
		if b.Chapters[i].Href == href {
			return i
		}
	}
	return -1
}
