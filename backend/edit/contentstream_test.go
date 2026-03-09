package edit

import (
	"math"
	"strings"
	"testing"
)

// ── Matrix tests ─────────────────────────────────────────────────────────────

func TestMatrixIdentity(t *testing.T) {
	m := Identity()
	if !m.IsIdentity() {
		t.Error("Identity() should be identity")
	}
	x, y := m.Transform(42, 99)
	if x != 42 || y != 99 {
		t.Errorf("Identity transform: got (%v, %v) want (42, 99)", x, y)
	}
}

func TestMatrixMultiply(t *testing.T) {
	// Amazon PDF: outer cm = 0.24 scale + Y flip + translate
	m1 := Matrix{A: 0.24, B: 0, C: 0, D: -0.24, E: 0, F: 792}
	// Inner cm = 2.55 scale
	m2 := Matrix{A: 2.55, B: 0, C: 0, D: 2.55, E: 0, F: 0}

	// cm concatenation: CTM = M_arg × CTM
	// After first cm: CTM = M1
	// After second cm: CTM = M2 × M1
	ctm := m2.Multiply(m1)

	// Expected: [0.612, 0, 0, -0.612, 0, 792]
	const eps = 0.001
	if math.Abs(ctm.A-0.612) > eps { t.Errorf("A: got %v want 0.612", ctm.A) }
	if math.Abs(ctm.B) > eps { t.Errorf("B: got %v want 0", ctm.B) }
	if math.Abs(ctm.D-(-0.612)) > eps { t.Errorf("D: got %v want -0.612", ctm.D) }
	if math.Abs(ctm.F-792) > eps { t.Errorf("F: got %v want 792", ctm.F) }

	// Transform point (40, 61) — "Order Summary" in Amazon PDF
	x, y := ctm.Transform(40, 61)
	if math.Abs(x-24.48) > eps { t.Errorf("x: got %v want ~24.48", x) }
	if math.Abs(y-754.668) > eps { t.Errorf("y: got %v want ~754.668", y) }
}

func TestMatrixInverse(t *testing.T) {
	m := Matrix{A: 2, B: 0, C: 0, D: 3, E: 10, F: 20}
	inv := m.Inverse()
	product := m.Multiply(inv)
	if !product.IsIdentity() {
		t.Errorf("M × M⁻¹ should be identity, got A=%v D=%v E=%v F=%v",
			product.A, product.D, product.E, product.F)
	}
}

func TestMatrixScale(t *testing.T) {
	m := Matrix{A: 3, B: 4, C: 0, D: 1, E: 0, F: 0}
	if math.Abs(m.ScaleX()-5.0) > 0.001 {
		t.Errorf("ScaleX: got %v want 5.0", m.ScaleX())
	}
}

// ── Tokeniser tests ──────────────────────────────────────────────────────────

func TestTokenise_LiteralString(t *testing.T) {
	src := []byte("BT\n/F1 12 Tf\n100 700 Td\n(Hello World) Tj\nET")
	tokens := tokenise(src)

	var found *token
	for i := range tokens {
		if tokens[i].kind == tokString {
			found = &tokens[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no string token found")
	}
	if found.value != "Hello World" {
		t.Errorf("got %q want %q", found.value, "Hello World")
	}
}

func TestTokenise_HexString(t *testing.T) {
	src := []byte("<48656c6c6f> Tj")
	tokens := tokenise(src)
	if tokens[0].kind != tokHexString {
		t.Fatalf("expected hex string, got kind %d", tokens[0].kind)
	}
	decoded := decodeString(tokens[0].value, tokHexString)
	if decoded != "Hello" {
		t.Errorf("got %q want %q", decoded, "Hello")
	}
}

func TestTokenise_HexStringWithWhitespace(t *testing.T) {
	// PDF spec allows whitespace inside hex strings
	src := []byte("<48 65 6c 6c 6f> Tj")
	tokens := tokenise(src)
	if tokens[0].kind != tokHexString {
		t.Fatalf("expected hex string, got kind %d", tokens[0].kind)
	}
	// Whitespace should be stripped
	if tokens[0].value != "48656c6c6f" {
		t.Errorf("hex value: got %q want %q", tokens[0].value, "48656c6c6f")
	}
}

func TestTokenise_NestedDicts(t *testing.T) {
	// Nested dicts should be skipped without crashing
	src := []byte("/Artifact <</Subtype /Watermark /Type <</Inner true>> >>BDC q Q")
	tokens := tokenise(src)
	// Should find the operators: /Artifact, BDC, q, Q
	var ops []string
	for _, tok := range tokens {
		if tok.kind == tokOperator {
			ops = append(ops, tok.value)
		}
	}
	if len(ops) < 2 {
		t.Errorf("expected at least BDC, q, Q operators, got %v", ops)
	}
}

// ── Decode tests ─────────────────────────────────────────────────────────────

func TestDecodeString_HexUTF16BE(t *testing.T) {
	decoded := decodeString("00480065006C006C006F", tokHexString)
	if decoded != "Hello" {
		t.Errorf("UTF-16BE hex: got %q want %q", decoded, "Hello")
	}
}

func TestDecodeString_HexSingleByteFallback(t *testing.T) {
	decoded := decodeString("48656C6C6F", tokHexString)
	if decoded != "Hello" {
		t.Errorf("single-byte hex: got %q want %q", decoded, "Hello")
	}
}

// ── CMap tests ───────────────────────────────────────────────────────────────

func TestParseCMap(t *testing.T) {
	cmap := `
/CIDInit /ProcSet findresource begin
begincmap
1 begincodespacerange
<0000> <FFFF>
endcodespacerange
2 beginbfchar
<0005> <0020>
<000B> <0026>
endbfchar
2 beginbfrange
<0026> <003E> <0041>
<0046> <0055> <0061>
endbfrange
endcmap
`
	m := parseCMap(cmap)

	tests := []struct {
		gid  uint16
		want rune
	}{
		{0x0005, ' '},   // bfchar
		{0x000B, '&'},   // bfchar
		{0x0026, 'A'},   // bfrange start
		{0x0034, 'O'},   // bfrange offset 0x0E
		{0x003E, 'Y'},   // bfrange end
		{0x0046, 'a'},   // second range start
		{0x0055, 'p'},   // second range end
	}

	for _, tt := range tests {
		if r, ok := m[tt.gid]; !ok || r != tt.want {
			t.Errorf("CMap[0x%04X]: got %q want %q (ok=%v)", tt.gid, string(r), string(tt.want), ok)
		}
	}
}

func TestCIDHexDecode(t *testing.T) {
	fi := &fontInfo{
		isCID: true,
		toUnicode: parseCMap(`
1 begincodespacerange
<0000> <FFFF>
endcodespacerange
1 beginbfrange
<0026> <003E> <0041>
endbfrange
`),
	}

	text := decodeHexStringWithFont("0034", fi)
	if text != "O" {
		t.Errorf("CID decode: got %q want %q", text, "O")
	}

	// Multiple characters
	text = decodeHexStringWithFont("00340057", fi)
	// 0x0034 → 'O', 0x0057 is outside the range so it might not decode
	if !strings.HasPrefix(text, "O") {
		t.Errorf("CID multi-char: got %q, want starts with 'O'", text)
	}
}

// ── Encoding tests ───────────────────────────────────────────────────────────

func TestWinAnsiEncoding(t *testing.T) {
	tests := []struct {
		code byte
		want rune
	}{
		{0x41, 'A'},      // standard ASCII
		{0x91, 0x2018},   // left single quote
		{0x92, 0x2019},   // right single quote
		{0x93, 0x201C},   // left double quote
		{0x94, 0x201D},   // right double quote
		{0x95, 0x2022},   // bullet
		{0x96, 0x2013},   // en dash
		{0x97, 0x2014},   // em dash
		{0x80, 0x20AC},   // euro sign
	}
	for _, tt := range tests {
		got := winAnsiEncoding[tt.code]
		if got != tt.want {
			t.Errorf("WinAnsi[0x%02X]: got U+%04X want U+%04X", tt.code, got, tt.want)
		}
	}
}

func TestGlyphNameResolve(t *testing.T) {
	tests := []struct {
		name string
		want rune
	}{
		{"fi", 0xFB01},
		{"bullet", 0x2022},
		{"emdash", 0x2014},
		{"Euro", 0x20AC},
		{"A", 'A'},
		{"space", ' '},
	}
	for _, tt := range tests {
		got := resolveGlyphName(tt.name)
		if got != tt.want {
			t.Errorf("resolveGlyphName(%q): got U+%04X want U+%04X", tt.name, got, tt.want)
		}
	}
}

// ── Parser integration tests ─────────────────────────────────────────────────

func TestParseContentStream_BasicPositioning(t *testing.T) {
	stream := []byte(`BT
/F1 12 Tf
100 700 Td
(First line) Tj
0 -15 Td
(Second line) Tj
ET`)
	p := newStreamParser(nil, nil, 1)
	p.parse(stream, 0)
	spans := filterSpans(p.spans)

	if len(spans) != 2 {
		t.Fatalf("got %d spans want 2", len(spans))
	}
	if spans[0].Text != "First line" { t.Errorf("span 0 text: %q", spans[0].Text) }
	if spans[0].X != 100             { t.Errorf("span 0 X: %v", spans[0].X) }
	if spans[0].Y != 700             { t.Errorf("span 0 Y: %v", spans[0].Y) }
	if spans[1].Text != "Second line" { t.Errorf("span 1 text: %q", spans[1].Text) }
}

func TestParseContentStream_TJArray(t *testing.T) {
	stream := []byte(`BT
/F1 10 Tf
50 500 Td
[(Hel) 2 (lo)] TJ
ET`)
	p := newStreamParser(nil, nil, 1)
	p.parse(stream, 0)
	spans := filterSpans(p.spans)

	if len(spans) != 1 {
		t.Fatalf("got %d spans want 1", len(spans))
	}
	if spans[0].Text != "Hello" {
		t.Errorf("TJ text: got %q want %q", spans[0].Text, "Hello")
	}
}

func TestParseContentStream_TextMatrix(t *testing.T) {
	stream := []byte(`BT
1 0 0 1 200 400 Tm
/F1 14 Tf
(Absolute) Tj
ET`)
	p := newStreamParser(nil, nil, 1)
	p.parse(stream, 0)
	spans := filterSpans(p.spans)

	if len(spans) != 1 {
		t.Fatalf("got %d spans", len(spans))
	}
	if spans[0].X != 200 || spans[0].Y != 400 {
		t.Errorf("Tm position: got (%v,%v) want (200,400)", spans[0].X, spans[0].Y)
	}
}

func TestParseContentStream_FontName(t *testing.T) {
	stream := []byte("BT\n/Helvetica 12 Tf\n0 0 Td\n(test) Tj\nET")
	p := newStreamParser(nil, nil, 1)
	p.parse(stream, 0)
	spans := filterSpans(p.spans)

	if len(spans) == 0 {
		t.Fatal("no spans")
	}
	if !strings.Contains(spans[0].FontName, "Helvetica") {
		t.Errorf("font name: %q", spans[0].FontName)
	}
}

func TestParseContentStream_CTMStack(t *testing.T) {
	// Simulate the Amazon PDF structure: outer scale + inner scale
	stream := []byte(`.24 0 0 -.24 0 792 cm
q
2.55 0 0 2.55 0 0 cm
BT
1 0 0 -1 40 61 Tm
/F1 28 Tf
(Hello) Tj
ET
Q`)
	p := newStreamParser(nil, nil, 1)
	p.parse(stream, 0)
	spans := filterSpans(p.spans)

	if len(spans) == 0 {
		t.Fatal("no spans extracted")
	}

	s := spans[0]
	const eps = 0.5

	// Expected page-space position: (24.48, 754.668)
	if math.Abs(s.X-24.48) > eps {
		t.Errorf("CTM X: got %v want ~24.48", s.X)
	}
	if math.Abs(s.Y-754.668) > eps {
		t.Errorf("CTM Y: got %v want ~754.668", s.Y)
	}

	// Font size should include CTM scale: 28 * 1 (Tm scale) * 0.612 (CTM scale) ≈ 17.14
	if s.FontSize < 15 || s.FontSize > 20 {
		t.Errorf("CTM-scaled font size: got %v want ~17.1", s.FontSize)
	}
}

func TestParseContentStream_GraphicsStateRestore(t *testing.T) {
	// After Q, CTM should revert to the saved state
	stream := []byte(`
q
2 0 0 2 100 100 cm
BT /F1 10 Tf 0 0 Td (Inside) Tj ET
Q
BT /F1 10 Tf 50 50 Td (Outside) Tj ET`)
	p := newStreamParser(nil, nil, 1)
	p.parse(stream, 0)
	spans := filterSpans(p.spans)

	if len(spans) != 2 {
		t.Fatalf("got %d spans want 2", len(spans))
	}

	// "Inside" should be at (100, 100) from CTM
	if math.Abs(spans[0].X-100) > 1 {
		t.Errorf("Inside X: got %v want ~100", spans[0].X)
	}

	// "Outside" should be at (50, 50) — CTM reverted to identity
	if math.Abs(spans[1].X-50) > 1 {
		t.Errorf("Outside X: got %v want ~50", spans[1].X)
	}
}

// ── Span merging tests ───────────────────────────────────────────────────────

func TestMergeAdjacentSpans(t *testing.T) {
	spans := []TextSpan{
		{Text: "H", X: 10, Y: 100, FontName: "F1", FontSize: 12},
		{Text: "e", X: 17, Y: 100, FontName: "F1", FontSize: 12},
		{Text: "l", X: 24, Y: 100, FontName: "F1", FontSize: 12},
		{Text: "l", X: 28, Y: 100, FontName: "F1", FontSize: 12},
		{Text: "o", X: 32, Y: 100, FontName: "F1", FontSize: 12},
		// Different line
		{Text: "New", X: 10, Y: 120, FontName: "F1", FontSize: 12},
	}

	merged := mergeAdjacentSpans(spans)

	if len(merged) != 2 {
		t.Fatalf("expected 2 merged spans, got %d", len(merged))
	}

	if !strings.Contains(merged[0].Text, "Hello") {
		t.Errorf("first merged span should contain Hello, got %q", merged[0].Text)
	}
	if merged[1].Text != "New" {
		t.Errorf("second span: got %q want %q", merged[1].Text, "New")
	}
}

func TestMergeAdjacentSpans_DifferentFonts(t *testing.T) {
	spans := []TextSpan{
		{Text: "Bold", X: 10, Y: 100, FontName: "F1", FontSize: 12},
		{Text: "Normal", X: 50, Y: 100, FontName: "F2", FontSize: 12},
	}
	merged := mergeAdjacentSpans(spans)
	if len(merged) != 2 {
		t.Errorf("different fonts should not merge, got %d spans", len(merged))
	}
}

func TestFilterSpans(t *testing.T) {
	spans := []TextSpan{
		{Text: "Hello"},
		{Text: "   "},
		{Text: ""},
		{Text: "World"},
	}
	filtered := filterSpans(spans)
	if len(filtered) != 2 {
		t.Errorf("expected 2 spans, got %d", len(filtered))
	}
}

// ── CID hex with CTM tests ──────────────────────────────────────────────────

func TestParseContentStream_CIDHexStrings(t *testing.T) {
	stream := []byte("BT\n/F1 12 Tf\n100 700 Td\n<00480069> Tj\nET")
	p := newStreamParser(nil, nil, 1)
	p.parse(stream, 0)
	spans := filterSpans(p.spans)

	if len(spans) != 1 {
		t.Fatalf("got %d spans want 1", len(spans))
	}
	if spans[0].Text != "Hi" {
		t.Errorf("CID hex text: got %q want %q", spans[0].Text, "Hi")
	}
}

func TestParseContentStream_TzHorizontalScale(t *testing.T) {
	stream := []byte("BT\n/F1 12 Tf\n80 Tz\n100 700 Td\n(Test) Tj\nET")
	p := newStreamParser(nil, nil, 1)
	p.parse(stream, 0)
	spans := filterSpans(p.spans)

	if len(spans) == 0 {
		t.Fatal("no spans")
	}
	if p.ts.hScale != 80 {
		t.Errorf("horizontal scale: got %v want 80", p.ts.hScale)
	}
}

func TestParseContentStream_TrRenderMode(t *testing.T) {
	stream := []byte("BT\n3 Tr\n/F1 12 Tf\n100 700 Td\n(Invisible) Tj\nET")
	p := newStreamParser(nil, nil, 1)
	p.parse(stream, 0)

	// Text rendering mode 3 = invisible, but we should still extract the text
	spans := filterSpans(p.spans)
	if len(spans) == 0 {
		t.Fatal("invisible text should still be extracted")
	}
	if spans[0].Text != "Invisible" {
		t.Errorf("got %q want %q", spans[0].Text, "Invisible")
	}
}

// ── WinAnsiEncoding tests ───────────────────────────────────────────────────

func TestWinAnsiEncoding_SpecialChars(t *testing.T) {
	// Verify the encoding table maps the critical 0x80-0x9F range correctly
	tests := []struct {
		code byte
		want rune
		name string
	}{
		{0x41, 'A', "uppercase A"},
		{0x80, 0x20AC, "euro sign"},
		{0x91, 0x2018, "left single quote"},
		{0x92, 0x2019, "right single quote"},
		{0x93, 0x201C, "left double quote"},
		{0x94, 0x201D, "right double quote"},
		{0x95, 0x2022, "bullet"},
		{0x96, 0x2013, "en dash"},
		{0x97, 0x2014, "em dash"},
		{0xA3, 0x00A3, "pound sign"},
		{0xA9, 0x00A9, "copyright"},
		{0xAE, 0x00AE, "registered"},
	}
	for _, tt := range tests {
		got := winAnsiEncoding[tt.code]
		if got != tt.want {
			t.Errorf("WinAnsi[0x%02X] (%s): got U+%04X want U+%04X", tt.code, tt.name, got, tt.want)
		}
	}
}

func TestDecodeWithWinAnsiEncoding(t *testing.T) {
	// Simulate what happens when a Type1 font with WinAnsiEncoding
	// has a literal string containing bytes 0xA3 and 0x96
	fi := &fontInfo{
		isCID:    false,
		encoding: &winAnsiEncoding,
	}

	// The tokenizer converts octal escapes: \243 → byte 0xA3, \226 → byte 0x96
	// Test the encoding lookup path in decodeToken
	raw := string([]byte{0xA3}) // £ in WinAnsi
	var result []rune
	for _, b := range []byte(raw) {
		r := fi.encoding[b]
		if r == 0 {
			r = rune(b)
		}
		result = append(result, r)
	}
	if len(result) != 1 || result[0] != '£' {
		t.Errorf("WinAnsi decode 0xA3: got %q want '£'", string(result))
	}

	// En dash
	raw2 := string([]byte{0x96})
	var result2 []rune
	for _, b := range []byte(raw2) {
		r := fi.encoding[b]
		if r == 0 {
			r = rune(b)
		}
		result2 = append(result2, r)
	}
	if len(result2) != 1 || result2[0] != 0x2013 {
		t.Errorf("WinAnsi decode 0x96: got U+%04X want U+2013 (en dash)", result2[0])
	}
}

func TestParseContentStream_WinAnsiFont(t *testing.T) {
	// Simulate a content stream with Type1/WinAnsi font
	// Octal \243 = 0xA3 = £ in WinAnsiEncoding
	stream := []byte("BT\n/F1 12 Tf\n72 700 Td\n(Price: \\2431,200) Tj\nET")

	// Create a font info with WinAnsiEncoding
	fonts := pageFonts{
		"F1": &fontInfo{
			name:         "F1",
			isCID:        false,
			encoding:     &winAnsiEncoding,
			defaultWidth: 600,
		},
	}

	p := newStreamParser(nil, fonts, 1)
	p.parse(stream, 0)
	spans := filterSpans(p.spans)

	if len(spans) == 0 {
		t.Fatal("no spans extracted")
	}

	// The text should decode £ correctly
	if !strings.Contains(spans[0].Text, "£") {
		t.Errorf("WinAnsi font decode: got %q, want text containing '£'", spans[0].Text)
	}
	expected := "Price: £1,200"
	if spans[0].Text != expected {
		t.Errorf("WinAnsi full text: got %q want %q", spans[0].Text, expected)
	}
}

// ── Form XObject tests ──────────────────────────────────────────────────────

func TestParseContentStream_FormXObjectDo(t *testing.T) {
	// Verify the parser recognizes Do operators
	// (Full Form XObject parsing requires a pdfcpu context, so we
	// test that the Do case fires without a context and doesn't crash)
	stream := []byte(`q
BT /F1 14 Tf 72 700 Td (Page text) Tj ET
/FormName Do
BT /F1 12 Tf 72 650 Td (After form) Tj ET
Q`)

	p := newStreamParser(nil, nil, 1)
	p.parse(stream, 0)
	spans := filterSpans(p.spans)

	// Should get the two page-level text spans (Form XObject can't
	// be resolved without ctx, but should not crash)
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans (without ctx), got %d", len(spans))
	}
	if spans[0].Text != "Page text" {
		t.Errorf("span 0: got %q", spans[0].Text)
	}
	if spans[1].Text != "After form" {
		t.Errorf("span 1: got %q", spans[1].Text)
	}
}

// ── Differences array tests ─────────────────────────────────────────────────

func TestGlyphNameResolve_Common(t *testing.T) {
	tests := []struct {
		name string
		want rune
	}{
		{"fi", 0xFB01},
		{"fl", 0xFB02},
		{"bullet", 0x2022},
		{"emdash", 0x2014},
		{"endash", 0x2013},
		{"Euro", 0x20AC},
		{"quoteleft", 0x2018},
		{"quoteright", 0x2019},
		{"quotedblleft", 0x201C},
		{"quotedblright", 0x201D},
		{"ellipsis", 0x2026},
		{"copyright", 0x00A9},
		{"registered", 0x00AE},
		{"trademark", 0x2122},
		{"degree", 0x00B0},
	}
	for _, tt := range tests {
		got := resolveGlyphName(tt.name)
		if got != tt.want {
			t.Errorf("resolveGlyphName(%q): got U+%04X want U+%04X", tt.name, got, tt.want)
		}
	}
}
