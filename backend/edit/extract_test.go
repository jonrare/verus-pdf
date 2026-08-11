package edit

import (
	"strings"
	"testing"

	"veruspdf/backend/internal/pdftest"
)

// End-to-end decoder tests: a real PDF on disk, read through the same pdfcpu
// path the application uses.

func TestExtractText_SinglePage(t *testing.T) {
	path := pdftest.TextPage(t, "a.pdf", "BT\n/F1 12 Tf\n72 700 Td\n(Hello World) Tj\nET\n")

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1: %+v", len(spans), spans)
	}

	s := spans[0]
	if s.Text != "Hello World" {
		t.Errorf("Text = %q, want %q", s.Text, "Hello World")
	}
	if !approx(s.X, 72, 0.5) || !approx(s.Y, 700, 0.5) {
		t.Errorf("position = (%v, %v), want (72, 700)", s.X, s.Y)
	}
	if !approx(s.FontSize, 12, 0.001) {
		t.Errorf("FontSize = %v, want 12", s.FontSize)
	}
	if s.FontName != "F1" {
		t.Errorf("FontName = %q, want %q", s.FontName, "F1")
	}
	if s.PageNum != 1 {
		t.Errorf("PageNum = %d, want 1", s.PageNum)
	}
}

// When the font supplies a /Widths array, the reported span width must come
// from those metrics rather than a character-count estimate.
//
// This is what makes overlay boxes line up and what lets span merging detect
// word spaces. If decodeToken stops handing the raw character codes to
// stringWidth, every width silently degrades to len(text) × fontSize × 0.5 and
// this test catches it.
func TestExtractText_WidthUsesFontMetrics(t *testing.T) {
	const text = "Hello World"
	path := pdftest.TextPage(t, "a.pdf", "BT\n/F1 12 Tf\n72 700 Td\n("+text+") Tj\nET\n")

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	// Sum the real Helvetica advances for the string.
	var thousandths int
	for _, r := range text {
		thousandths += pdftest.HelveticaWidths[int(r)-pdftest.FirstChar]
	}
	want := float64(thousandths) / 1000 * 12

	if !approx(spans[0].Width, want, 0.01) {
		estimate := float64(len(text)) * 12 * 0.5
		t.Errorf("Width = %v, want %v (the 0.5 em estimate would be %v)",
			spans[0].Width, want, estimate)
	}
}

// The reported byte offsets must select exactly the string operand, or the
// editor splices over the wrong bytes.
func TestExtractText_OffsetsSelectTheStringOperand(t *testing.T) {
	const content = "BT\n/F1 12 Tf\n72 700 Td\n(Hello) Tj\nET\n"
	path := pdftest.TextPage(t, "a.pdf", content)

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	s := spans[0]

	if s.OpStart < 0 || s.OpEnd > len(content) || s.OpStart >= s.OpEnd {
		t.Fatalf("offsets [%d,%d] out of range for a %d-byte stream", s.OpStart, s.OpEnd, len(content))
	}
	if got := content[s.OpStart:s.OpEnd]; got != "(Hello)" {
		t.Errorf("offsets select %q, want %q", got, "(Hello)")
	}
}

// BlockStart/BlockEnd must bracket the whole BT…ET block for block rewriting.
func TestExtractText_BlockBoundsCoverBTtoET(t *testing.T) {
	const content = "BT\n/F1 12 Tf\n72 700 Td\n(Hello) Tj\nET\n"
	path := pdftest.TextPage(t, "a.pdf", content)

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	s := spans[0]

	block := content[s.BlockStart:s.BlockEnd]
	if !strings.HasPrefix(block, "BT") {
		t.Errorf("block starts with %q, want it to start at BT", block[:min(4, len(block))])
	}
	if !strings.HasSuffix(block, "ET") {
		t.Errorf("block ends with %q, want it to end at ET", block[max(0, len(block)-4):])
	}
	if s.BlockStart > s.OpStart || s.BlockEnd < s.OpEnd {
		t.Errorf("block [%d,%d] does not contain the span [%d,%d]",
			s.BlockStart, s.BlockEnd, s.OpStart, s.OpEnd)
	}
}

func TestExtractText_MultiplePages(t *testing.T) {
	path := pdftest.Pages(t, "m.pdf",
		"BT /F1 12 Tf 72 700 Td (Page One) Tj ET",
		"BT /F1 12 Tf 72 700 Td (Page Two) Tj ET",
		"BT /F1 12 Tf 72 700 Td (Page Three) Tj ET")

	for i, want := range []string{"Page One", "Page Two", "Page Three"} {
		spans, err := ExtractText(path, i+1)
		if err != nil {
			t.Fatalf("page %d: %v", i+1, err)
		}
		if len(spans) != 1 || spans[0].Text != want {
			t.Errorf("page %d gave %+v, want a single %q", i+1, spans, want)
		}
		if spans[0].PageNum != i+1 {
			t.Errorf("page %d span reports PageNum %d", i+1, spans[0].PageNum)
		}
	}
}

func TestExtractText_PageOutOfRange(t *testing.T) {
	path := pdftest.TextPage(t, "a.pdf", "BT /F1 12 Tf 72 700 Td (x) Tj ET")
	for _, page := range []int{0, -1, 2, 100} {
		if _, err := ExtractText(path, page); err == nil {
			t.Errorf("page %d was accepted", page)
		}
	}
}

func TestExtractText_MissingFile(t *testing.T) {
	if _, err := ExtractText("/nonexistent/nope.pdf", 1); err == nil {
		t.Error("expected an error for a missing file")
	}
}

func TestExtractText_EmptyPageYieldsNoSpans(t *testing.T) {
	path := pdftest.TextPage(t, "a.pdf", "q 1 0 0 1 0 0 cm Q\n")
	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if len(spans) != 0 {
		t.Errorf("got %d spans, want 0: %+v", len(spans), spans)
	}
}

// Escaped delimiters inside a literal string must survive the round trip.
func TestExtractText_EscapedParens(t *testing.T) {
	path := pdftest.TextPage(t, "a.pdf", `BT /F1 12 Tf 72 700 Td (Smith \(Jr.\)) Tj ET`)
	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if len(spans) != 1 || spans[0].Text != "Smith (Jr.)" {
		t.Errorf("got %+v, want a single %q", spans, "Smith (Jr.)")
	}
}

func TestExtractText_MultipleSpansOnPage(t *testing.T) {
	path := pdftest.TextPage(t, "a.pdf",
		"BT /F1 12 Tf 72 700 Td (First) Tj ET\nBT /F1 12 Tf 72 600 Td (Second) Tj ET\n")
	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2: %+v", len(spans), spans)
	}
	if spans[0].Text != "First" || spans[1].Text != "Second" {
		t.Errorf("got %q and %q", spans[0].Text, spans[1].Text)
	}
	if !approx(spans[0].Y, 700, 0.5) || !approx(spans[1].Y, 600, 0.5) {
		t.Errorf("Y positions = %v, %v; want 700, 600", spans[0].Y, spans[1].Y)
	}
}

// ── Tokeniser: literal string escapes, ISO 32000-1:2008 §7.3.4.2 ─────────────

func TestTokenise_OctalEscape(t *testing.T) {
	// \101 is 'A', \60 is '0'.
	toks := tokenise([]byte(`(\101\60) Tj`))
	if len(toks) == 0 || toks[0].kind != tokString {
		t.Fatalf("expected a string token, got %+v", toks)
	}
	if toks[0].value != "A0" {
		t.Errorf("value = %q, want %q", toks[0].value, "A0")
	}
}

// A backslash before a newline is a line continuation and contributes nothing
// to the string value.
func TestTokenise_LineContinuationEmitsNothing(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"LF", "(abc\\\ndef) Tj"},
		{"CR", "(abc\\\rdef) Tj"},
		{"CRLF", "(abc\\\r\ndef) Tj"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			toks := tokenise([]byte(tc.src))
			if len(toks) == 0 || toks[0].kind != tokString {
				t.Fatalf("expected a string token, got %+v", toks)
			}
			if toks[0].value != "abcdef" {
				t.Errorf("value = %q, want %q", toks[0].value, "abcdef")
			}
		})
	}
}

func TestTokenise_BalancedNestedParens(t *testing.T) {
	toks := tokenise([]byte(`(a (nested) b) Tj`))
	if len(toks) == 0 || toks[0].kind != tokString {
		t.Fatalf("expected a string token, got %+v", toks)
	}
	if toks[0].value != "a (nested) b" {
		t.Errorf("value = %q, want %q", toks[0].value, "a (nested) b")
	}
}

func TestTokenise_CommentsSkipped(t *testing.T) {
	toks := tokenise([]byte("% a comment\n(text) Tj"))
	if len(toks) != 2 {
		t.Fatalf("got %d tokens, want 2: %+v", len(toks), toks)
	}
	if toks[0].kind != tokString || toks[1].value != "Tj" {
		t.Errorf("unexpected tokens: %+v", toks)
	}
}

func TestTokenise_NumbersVersusOperators(t *testing.T) {
	toks := tokenise([]byte("1 -2 3.5 -0.25 Tj BT"))
	kinds := map[string]tokenKind{}
	for _, tk := range toks {
		kinds[tk.value] = tk.kind
	}
	for _, num := range []string{"1", "-2", "3.5", "-0.25"} {
		if kinds[num] != tokNumber {
			t.Errorf("%q classified as %v, want tokNumber", num, kinds[num])
		}
	}
	for _, op := range []string{"Tj", "BT"} {
		if kinds[op] != tokOperator {
			t.Errorf("%q classified as %v, want tokOperator", op, kinds[op])
		}
	}
}

func TestCollectOperands_StopsAtPreviousOperator(t *testing.T) {
	toks := tokenise([]byte("1 0 0 1 72 700 cm 5 Tj"))
	var tjIdx int
	for i, tk := range toks {
		if tk.kind == tokOperator && tk.value == "Tj" {
			tjIdx = i
		}
	}
	ops := collectOperands(toks, tjIdx)
	if len(ops) != 1 || ops[0].value != "5" {
		t.Errorf("got %+v, want just the operand 5", ops)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
