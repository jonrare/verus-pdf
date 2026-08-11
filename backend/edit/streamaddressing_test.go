package edit

import (
	"strings"
	"testing"

	"veruspdf/backend/internal/pdftest"
)

// Byte offsets reported by the decoder are applied by the editors to an
// individual content stream object. A page's /Contents may be an ARRAY of
// streams (ISO 32000-1:2008, §7.8.2), so an offset measured against the
// concatenation of the parts addresses nothing real. These tests pin the
// addressing contract.

func TestExtractText_SplitContentsReportsPerStreamOffsets(t *testing.T) {
	const s0 = "BT\n/F1 12 Tf\n72 700 Td\n(First) Tj\nET\n"
	const s1 = "BT\n/F1 12 Tf\n72 600 Td\n(Second) Tj\nET\n"
	path := pdftest.SplitContentsPage(t, "split.pdf", s0, s1)

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2: %+v", len(spans), spans)
	}

	parts := []string{s0, s1}
	wants := []string{"(First)", "(Second)"}

	for i, s := range spans {
		if s.StreamIndex != i {
			t.Errorf("span %d reports StreamIndex %d, want %d", i, s.StreamIndex, i)
		}
		if !s.Editable {
			t.Errorf("span %d is marked not editable", i)
		}
		part := parts[s.StreamIndex]
		if s.OpEnd > len(part) {
			t.Fatalf("span %d offsets [%d,%d] exceed its %d-byte stream — "+
				"offsets were measured against the concatenation",
				i, s.OpStart, s.OpEnd, len(part))
		}
		if got := part[s.OpStart:s.OpEnd]; got != wants[i] {
			t.Errorf("span %d offsets select %q from stream %d, want %q",
				i, got, s.StreamIndex, wants[i])
		}
	}
}

// The whole point of the addressing fix: editing text in the second stream of
// a /Contents array must change that text and leave the first stream alone.
func TestReplaceSpanText_EditsCorrectStreamOfSplitContents(t *testing.T) {
	path := pdftest.SplitContentsPage(t, "split.pdf",
		"BT\n/F1 12 Tf\n72 700 Td\n(First) Tj\nET\n",
		"BT\n/F1 12 Tf\n72 600 Td\n(Second) Tj\nET\n")

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2", len(spans))
	}
	target := spans[1]
	if target.Text != "Second" {
		t.Fatalf("expected to target %q, got %q", "Second", target.Text)
	}

	out := path + ".out.pdf"
	res := New().ReplaceSpanText(path, out, 1,
		target.StreamIndex, target.OpStart, target.OpEnd, "Third!")
	if res.Error != "" {
		t.Fatalf("ReplaceSpanText: %s", res.Error)
	}

	got, err := ExtractText(out, 1)
	if err != nil {
		t.Fatalf("ExtractText(out): %v — the wrong stream was spliced", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d spans, want 2: %+v", len(got), got)
	}
	if got[0].Text != "First" {
		t.Errorf("the untouched stream now reads %q, want %q", got[0].Text, "First")
	}
	if got[1].Text != "Third!" {
		t.Errorf("the edited stream reads %q, want %q", got[1].Text, "Third!")
	}
}

// State is shared across the parts of a /Contents array — they are one logical
// stream divided at token boundaries, so a Tf in one part applies to the next.
func TestExtractText_SplitContentsSharesGraphicsState(t *testing.T) {
	path := pdftest.SplitContentsPage(t, "split.pdf",
		"BT\n/F1 18 Tf\n72 700 Td\n(Sized) Tj\nET\n",
		"BT\n72 600 Td\n(Inherits) Tj\nET\n") // no Tf of its own

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2: %+v", len(spans), spans)
	}
	if !approx(spans[1].FontSize, 18, 0.001) {
		t.Errorf("second stream reports font size %v, want 18 inherited from the first",
			spans[1].FontSize)
	}
	if spans[1].FontName != "F1" {
		t.Errorf("second stream reports font %q, want F1 inherited from the first", spans[1].FontName)
	}
}

// ── Form XObjects ────────────────────────────────────────────────────────────

// Text inside a Form XObject has offsets into the XObject's own stream, which
// is a different object. Those spans must be extracted (they are real text on
// the page) but flagged so the editors refuse them.
func TestExtractText_FormXObjectSpansAreNotEditable(t *testing.T) {
	path := pdftest.FormXObjectPage(t, "xobj.pdf",
		"BT /F1 12 Tf 72 700 Td (OnPage) Tj ET\n/X1 Do\n",
		"BT /F1 12 Tf 72 500 Td (InsideForm) Tj ET\n")

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}

	var page, form *TextSpan
	for i := range spans {
		switch spans[i].Text {
		case "OnPage":
			page = &spans[i]
		case "InsideForm":
			form = &spans[i]
		}
	}
	if page == nil {
		t.Fatalf("page text was not extracted: %+v", spans)
	}
	if form == nil {
		t.Fatalf("Form XObject text was not extracted: %+v", spans)
	}

	if !page.Editable || page.StreamIndex != 0 {
		t.Errorf("page span = {StreamIndex:%d Editable:%v}, want {0 true}",
			page.StreamIndex, page.Editable)
	}
	if form.Editable || form.StreamIndex != notEditable {
		t.Errorf("Form XObject span = {StreamIndex:%d Editable:%v}, want {%d false}",
			form.StreamIndex, form.Editable, notEditable)
	}
}

// Parsing a Form XObject must not leave the parser attributing the rest of the
// page to the wrong stream.
func TestExtractText_StreamIdentityRestoredAfterXObject(t *testing.T) {
	path := pdftest.FormXObjectPage(t, "xobj.pdf",
		"BT /F1 12 Tf 72 700 Td (Before) Tj ET\n/X1 Do\nBT /F1 12 Tf 72 400 Td (After) Tj ET\n",
		"BT /F1 12 Tf 72 500 Td (Inside) Tj ET\n")

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}

	for _, s := range spans {
		switch s.Text {
		case "Before", "After":
			if s.StreamIndex != 0 || !s.Editable {
				t.Errorf("page span %q = {StreamIndex:%d Editable:%v}, want {0 true}",
					s.Text, s.StreamIndex, s.Editable)
			}
		case "Inside":
			if s.Editable {
				t.Errorf("Form XObject span %q is marked editable", s.Text)
			}
		}
	}
}

func TestReplaceSpanText_RefusesFormXObjectSpans(t *testing.T) {
	path := pdftest.FormXObjectPage(t, "xobj.pdf",
		"BT /F1 12 Tf 72 700 Td (OnPage) Tj ET\n/X1 Do\n",
		"BT /F1 12 Tf 72 500 Td (InsideForm) Tj ET\n")

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	var form *TextSpan
	for i := range spans {
		if spans[i].Text == "InsideForm" {
			form = &spans[i]
		}
	}
	if form == nil {
		t.Fatal("Form XObject text was not extracted")
	}

	res := New().ReplaceSpanText(path, path+".out.pdf", 1,
		form.StreamIndex, form.OpStart, form.OpEnd, "Hacked")
	if res.Error == "" {
		t.Error("splicing a Form XObject span into the page stream was allowed")
	}
	if !strings.Contains(res.Error, "cannot be edited in place") {
		t.Errorf("error was %q, want it to explain why the span is not editable", res.Error)
	}
}

func TestEditMergedSpans_RefusesFormXObjectSpans(t *testing.T) {
	path := pdftest.FormXObjectPage(t, "xobj.pdf",
		"BT /F1 12 Tf 72 700 Td (OnPage) Tj ET\n/X1 Do\n",
		"BT /F1 12 Tf 72 500 Td (InsideForm) Tj ET\n")

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	var form *TextSpan
	for i := range spans {
		if spans[i].Text == "InsideForm" {
			form = &spans[i]
		}
	}
	if form == nil {
		t.Fatal("Form XObject text was not extracted")
	}

	subs := []SubSpanInfo{{
		StreamIndex: form.StreamIndex, OpStart: form.OpStart, OpEnd: form.OpEnd,
		Text: form.Text, FontName: form.FontName,
		BlockStart: form.BlockStart, BlockEnd: form.BlockEnd, TfSize: form.TfSize,
	}}

	res := New().EditMergedSpans(path, path+".out.pdf", 1, subs, form.Text, "Hacked")
	if res.Error == "" {
		t.Error("rewriting a block from a Form XObject was allowed")
	}
}
