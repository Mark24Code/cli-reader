package htmlimport

import (
	"strings"
	"testing"

	"cli-reader/internal/book"
)

func parseTest(t *testing.T, data string) *book.Chapter {
	t.Helper()
	res, err := Parse([]byte(data), Options{Href: "OEBPS/ch.xhtml"})
	if err != nil {
		t.Fatal(err)
	}
	return res.Chapter
}

func TestSelfClosingAnchorDoesNotFlattenBlocks(t *testing.T) {
	// XHTML writers sometimes emit <a id="x"/> for non-void elements. Under
	// HTML parsing rules that leaves the anchor open across the following
	// blocks; the parser must still preserve paragraph boundaries.
	data := `<html><body><div>
		<h2><a id="x"/>Title</h2>
		<p>first paragraph</p>
		<p>second paragraph</p>
		<p>third paragraph</p>
	</div></body></html>`
	ch := parseTest(t, data)
	paragraphs := 0
	for _, b := range ch.Blocks {
		if b.Kind == book.BlockParagraph {
			paragraphs++
		}
	}
	if paragraphs != 3 {
		t.Fatalf("paragraphs = %d, want 3 (blocks=%#v)", paragraphs, ch.Blocks)
	}
}

func TestTableStructureIsPreserved(t *testing.T) {
	data := `<html><body>
		<table>
			<caption>快捷键</caption>
			<thead><tr><th>功能</th><th align="right">入口</th></tr></thead>
			<tbody>
				<tr><td rowspan="2"><p>合并</p><p>两行</p></td><td colspan="2" style="text-align: center">宽</td></tr>
				<tr><td><ul><li>甲</li><li>乙</li></ul></td><td>丙</td></tr>
			</tbody>
		</table>
	</body></html>`
	ch := parseTest(t, data)
	var table *book.Block
	for i := range ch.Blocks {
		if ch.Blocks[i].Kind == book.BlockTable {
			table = &ch.Blocks[i]
			break
		}
	}
	if table == nil {
		t.Fatal("table block missing")
	}
	if got := spansText(table.TableCaption); got != "快捷键" {
		t.Fatalf("caption = %q", got)
	}
	if len(table.Table) != 3 {
		t.Fatalf("rows = %d", len(table.Table))
	}
	if !table.Table[0][0].Header || !table.Table[0][1].Header {
		t.Fatal("cells inside thead/th should be marked as headers")
	}
	if table.Table[0][0].Align != "" {
		t.Fatalf("align = %q, want default", table.Table[0][0].Align)
	}
	if table.Table[0][1].Align != "right" {
		t.Fatalf("align = %q, want right", table.Table[0][1].Align)
	}

	merged := table.Table[1][0]
	if merged.RowSpan != 2 {
		t.Fatalf("rowspan = %d", merged.RowSpan)
	}
	if got := spansText(merged.Spans); !strings.Contains(got, "合并") || !strings.Contains(got, "\n") {
		t.Fatalf("block content inside a cell lost its break: %q", got)
	}
	wide := table.Table[1][1]
	if wide.ColSpan != 2 {
		t.Fatalf("colspan = %d", wide.ColSpan)
	}
	if wide.Align != "center" {
		t.Fatalf("style text-align = %q, want center", wide.Align)
	}
	var listCell book.TableCell
	for _, row := range table.Table {
		for _, cell := range row {
			if strings.Contains(spansText(cell.Spans), "• ") {
				listCell = cell
			}
		}
	}
	if got := spansText(table.Table[2][0].Spans); !strings.Contains(got, "• 甲") {
		t.Fatalf("list cell location changed: %q", got)
	}
	if got := spansText(listCell.Spans); !strings.Contains(got, "• 甲") || !strings.Contains(got, "• 乙") {
		t.Fatalf("list inside a cell was flattened: %q", got)
	}
}

func TestSourceNewlinesCollapseButBrBreaks(t *testing.T) {
	data := `<html><body>
		<p>first line
		second line</p>
		<p>alpha<br/>beta</p>
	</body></html>`
	ch := parseTest(t, data)
	var paragraphs []string
	for _, b := range ch.Blocks {
		if b.Kind == book.BlockParagraph {
			paragraphs = append(paragraphs, spansText(b.Spans))
		}
	}
	if len(paragraphs) != 2 {
		t.Fatalf("paragraphs = %#v", paragraphs)
	}
	if paragraphs[0] != "first line second line" {
		t.Fatalf("source newline was not collapsed: %q", paragraphs[0])
	}
	if paragraphs[1] != "alpha\nbeta" {
		t.Fatalf("explicit br missing: %q", paragraphs[1])
	}
}

func TestGenerateHeadingAnchors(t *testing.T) {
	data := `<html><body>
		<h1>第一章</h1>
		<h2 id="given">小节</h2>
		<h2>另一个小节</h2>
	</body></html>`
	res, err := Parse([]byte(data), Options{Href: "ch.html", GenerateHeadingAnchors: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Headings) != 3 {
		t.Fatalf("headings = %d: %#v", len(res.Headings), res.Headings)
	}
	if res.Headings[0].Anchor == "" || res.Headings[1].Anchor != "given" || res.Headings[2].Anchor == "" {
		t.Fatalf("unexpected anchors: %#v", res.Headings)
	}
	for _, h := range res.Headings {
		if _, ok := res.Chapter.Anchors[h.Anchor]; !ok {
			t.Fatalf("anchor %q missing from chapter", h.Anchor)
		}
	}
}

func TestImageResolver(t *testing.T) {
	data := `<html><body>
		<img src="img/a.png" alt="本地"/>
		<img src="https://example.com/x.png" alt="远程"/>
		<img src="data:image/png;base64,AAAA" alt="内嵌"/>
	</body></html>`
	var refs []book.ImageRef
	res, err := Parse([]byte(data), Options{
		Href: "ch.html",
		ResolveImage: func(ref *book.ImageRef) {
			ref.Path = "resolved:" + ref.Source
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range res.Chapter.Blocks {
		if b.Image != nil {
			refs = append(refs, *b.Image)
		}
	}
	if len(refs) != 3 {
		t.Fatalf("images = %d", len(refs))
	}
	if refs[0].Path != "resolved:img/a.png" {
		t.Fatalf("local image path = %q", refs[0].Path)
	}
	if !refs[1].External || !refs[2].External {
		t.Fatalf("remote/data images should be external: %#v", refs)
	}
}
