package epub

import (
	"strings"
	"testing"
)

func TestSelfClosingAnchorDoesNotFlattenBlocks(t *testing.T) {
	// XHTML writers sometimes emit <a id="x"/> for non-void elements. Under
	// HTML parsing rules that leaves the anchor open across the following
	// blocks; the parser must still preserve paragraph boundaries.
	data := []byte(`<html><body><div>
		<h2><a id="x"/>Title</h2>
		<p>first paragraph</p>
		<p>second paragraph</p>
		<p>third paragraph</p>
	</div></body></html>`)
	book := &Book{}
	ch, err := book.parseChapter(data, 0, "OEBPS/ch.xhtml")
	if err != nil {
		t.Fatal(err)
	}
	paragraphs := 0
	for _, b := range ch.Blocks {
		if b.Kind == BlockParagraph {
			paragraphs++
		}
	}
	if paragraphs != 3 {
		t.Fatalf("paragraphs = %d, want 3 (blocks=%#v)", paragraphs, ch.Blocks)
	}
}

func TestTableStructureIsPreserved(t *testing.T) {
	data := []byte(`<html><body>
		<table>
			<caption>快捷键</caption>
			<thead><tr><th>功能</th><th align="right">入口</th></tr></thead>
			<tbody>
				<tr><td rowspan="2"><p>合并</p><p>两行</p></td><td colspan="2" style="text-align: center">宽</td></tr>
				<tr><td><ul><li>甲</li><li>乙</li></ul></td><td>丙</td></tr>
			</tbody>
		</table>
	</body></html>`)
	book := &Book{}
	ch, err := book.parseChapter(data, 0, "OEBPS/ch.xhtml")
	if err != nil {
		t.Fatal(err)
	}
	var table *Block
	for i := range ch.Blocks {
		if ch.Blocks[i].Kind == BlockTable {
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
	var listCell TableCell
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
	data := []byte(`<html><body>
		<p>first line
		second line</p>
		<p>alpha<br/>beta</p>
	</body></html>`)
	book := &Book{}
	ch, err := book.parseChapter(data, 0, "OEBPS/ch.xhtml")
	if err != nil {
		t.Fatal(err)
	}
	var paragraphs []string
	for _, b := range ch.Blocks {
		if b.Kind == BlockParagraph {
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
