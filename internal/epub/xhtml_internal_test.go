package epub

import "testing"

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
