package epub

import (
	"archive/zip"
	"sync"
)

// BlockKind identifies the kind of a block of reading content.
type BlockKind int

const (
	BlockParagraph BlockKind = iota
	BlockHeading
	BlockCode
	BlockImage
	BlockListItem
	BlockRule
	BlockTable
)

// Span is a styled run of inline text.
type Span struct {
	Text      string
	Break     bool // explicit <br>; source newlines are collapsed
	Bold      bool
	Italic    bool
	Code      bool
	Underline bool
	Link      string
}

// ImageRef points at an image inside the EPUB archive.
type ImageRef struct {
	Path     string // normalised path inside the zip; empty if unavailable
	Source   string // original src/href
	Alt      string
	Title    string
	External bool
}

// Block is the intermediate representation between XHTML and the renderer.
type Block struct {
	Kind    BlockKind
	Spans   []Span
	Level   int    // heading level
	Code    string // raw code for BlockCode
	Lang    string // detected/declared code language
	Image   *ImageRef
	Depth   int // list nesting depth, starting at 1
	Ordered bool
	Number  int
	Quote   bool
	Table   [][]string
}

// Chapter is one spine document, already parsed into blocks.
type Chapter struct {
	Index    int
	ID       string
	Href     string // normalised path inside the zip
	Title    string
	Heading  string // first heading, used as a title fallback
	DocTitle string // <title> from the XHTML head
	Blocks   []Block
	Anchors  map[string]int // fragment -> block index
}

// TOCEntry is a flattened table-of-contents item. Depth starts at 0.
type TOCEntry struct {
	Title    string
	Href     string
	Fragment string
	Chapter  int // -1 when the target is outside the reading spine
	Depth    int
}

// Book is a parsed EPUB.
type Book struct {
	Path       string
	Title      string
	Author     string
	Language   string
	Identifier string
	Chapters   []Chapter
	TOC        []TOCEntry
	Warnings   []string

	zip     *zip.Reader
	closer  func() error
	byName  map[string]*zip.File
	byLower map[string]*zip.File

	imageMu    sync.Mutex
	imageCache map[string][]byte
	imageBytes int64
}

// Position identifies a reading location. Percent is the chapter-local scroll
// percentage, which survives terminal-width changes better than a line number.
type Position struct {
	Href    string  `json:"href"`
	Percent float64 `json:"percent"`
}
