// Package book defines the format-neutral reading model shared by every
// supported ebook format and the terminal renderer.
package book

// Format identifies the container format a Book was loaded from.
type Format string

const (
	FormatEPUB     Format = "epub"
	FormatPDF      Format = "pdf"
	FormatTXT      Format = "txt"
	FormatMarkdown Format = "markdown"
	FormatHTML     Format = "html"
	FormatCBZ      Format = "cbz"
	FormatMOBI     Format = "mobi" // MOBI and AZW3
)

// Reading modes for formats that support more than one presentation.
const (
	ModeText  = "text"
	ModeImage = "image"
)

// Label returns a human-readable format name.
func (f Format) Label() string {
	switch f {
	case FormatEPUB:
		return "EPUB"
	case FormatPDF:
		return "PDF"
	case FormatTXT:
		return "TXT"
	case FormatMarkdown:
		return "Markdown"
	case FormatHTML:
		return "HTML"
	case FormatCBZ:
		return "CBZ"
	case FormatMOBI:
		return "MOBI/AZW3"
	}
	return string(f)
}

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

// ImageRef points at an image inside the book container.
type ImageRef struct {
	Path     string // provider-specific key; empty if unavailable
	Source   string // original src/href
	Alt      string
	Title    string
	External bool
}

// TableCell is the inline content of one table cell.
type TableCell struct {
	Spans   []Span
	Header  bool   // <th> or a cell inside <thead>
	ColSpan int    // number of columns covered; 1 when unset
	RowSpan int    // number of rows covered; 1 when unset
	Align   string // "left", "center" or "right"; empty means default
}

// Block is the intermediate representation between markup and the renderer.
type Block struct {
	Kind         BlockKind
	Spans        []Span
	Level        int    // heading level
	Code         string // raw code for BlockCode
	Lang         string // detected/declared code language
	Image        *ImageRef
	Depth        int // list nesting depth, starting at 1
	Ordered      bool
	Number       int
	Quote        bool
	Table        [][]TableCell
	TableCaption []Span
}

// Chapter is one reading unit (a spine document, text section or page).
type Chapter struct {
	Index    int
	ID       string
	Href     string // stable logical path or pseudo-href
	Title    string
	Heading  string // first heading, used as a title fallback
	DocTitle string // document <title>
	PageMode bool   // render a single page image filling the viewport
	Blocks   []Block
	Anchors  map[string]int // fragment -> block index
}

// TOCEntry is a flattened table-of-contents item. Depth starts at 0.
type TOCEntry struct {
	Title    string
	Href     string
	Fragment string
	Chapter  int // -1 when the target is outside the reading content
	Depth    int
}

// HeadingRef records a heading that can become a TOC entry.
type HeadingRef struct {
	Block  int
	Level  int
	Title  string
	Anchor string
}

// Position identifies a reading location. Percent is the chapter-local scroll
// percentage, which survives terminal-width changes better than a line number.
type Position struct {
	Href    string  `json:"href"`
	Percent float64 `json:"percent"`
}
