package theme

import "github.com/alecthomas/chroma/v2"

const (
	Accent       = "#D97757"
	AccentBright = "#F08A65"
	AccentSoft   = "#B9634A"
	Muted        = "#8C8378"
	Dim          = "#655F57"
	Warm         = "#E8E2D6"
	CodeBG       = "#211F1C"
	CodeText     = "#D6D0C4"
	InlineCodeFG = "#E8C07D"
	InlineCodeBG = "#2C2925"
	Error        = "#E06C75"
	Success      = "#98C379"
)

var chromaStyle = chroma.MustNewStyle("claude-reader", chroma.StyleEntries{
	chroma.Text:            CodeText,
	chroma.Background:      CodeText,
	chroma.Comment:         "#7F8A78 italic",
	chroma.CommentPreproc:  "#C58A6A",
	chroma.Keyword:         Accent + " bold",
	chroma.KeywordType:     "#D9A05B",
	chroma.KeywordConstant: "#D9A05B",
	chroma.String:          "#A8C98A",
	chroma.StringEscape:    "#E5B567",
	chroma.Number:          "#E5B567",
	chroma.NameFunction:    "#E8C07D",
	chroma.NameClass:       "#7FB4CA",
	chroma.NameBuiltin:     "#7FB4CA",
	chroma.NameTag:         Accent,
	chroma.NameAttribute:   "#E8C07D",
	chroma.Operator:        "#C6C1B9",
	chroma.Punctuation:     "#B8B1A6",
	chroma.Error:           Error,
})

// ChromaStyle returns the shared syntax-highlighting style.
func ChromaStyle() *chroma.Style {
	return chromaStyle
}
