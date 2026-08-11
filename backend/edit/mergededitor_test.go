package edit

import (
	"math"
	"strings"
	"testing"

	"veruspdf/backend/internal/pdftest"
)

// approx reports whether a and b are within tol of each other.
func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// ── Rune diffing ─────────────────────────────────────────────────────────────

// applyOps replays a diff against the old text so tests can assert on the
// result rather than on the exact op sequence, which has several valid forms.
func applyOps(old []rune, ops []editOp) string {
	var sb strings.Builder
	oi := 0
	for _, op := range ops {
		switch op.kind {
		case opKeep:
			if oi < len(old) {
				sb.WriteRune(old[oi])
			}
			oi++
		case opReplace:
			sb.WriteRune(op.newChar)
			oi++
		case opInsert:
			sb.WriteRune(op.newChar)
		case opDelete:
			oi++
		}
	}
	return sb.String()
}

func TestDiffRunes_ProducesTargetText(t *testing.T) {
	tests := []struct{ name, old, new string }{
		{"identical", "Hello", "Hello"},
		{"append", "Hello", "Hello World"},
		{"prepend", "World", "Hello World"},
		{"delete tail", "Hello World", "Hello"},
		{"delete all", "Hello", ""},
		{"insert into empty", "", "Hello"},
		{"single replace", "Hello", "Hallo"},
		{"replace all", "abc", "xyz"},
		{"middle insert", "abcd", "abXcd"},
		{"middle delete", "abXcd", "abcd"},
		{"longer", "the quick brown fox", "the slow brown cat"},
		{"unicode", "café", "cafés"},
		{"repeated chars", "aaaa", "aa"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old, want := []rune(tt.old), tt.new
			ops := diffRunes(old, []rune(tt.new))
			if got := applyOps(old, ops); got != want {
				t.Errorf("diffRunes(%q, %q) replays to %q", tt.old, tt.new, got)
			}
		})
	}
}

func TestDiffRunes_IdenticalIsAllKeeps(t *testing.T) {
	ops := diffRunes([]rune("Hello"), []rune("Hello"))
	if len(ops) != 5 {
		t.Fatalf("got %d ops, want 5", len(ops))
	}
	for i, op := range ops {
		if op.kind != opKeep {
			t.Errorf("op %d is %v, want opKeep", i, op.kind)
		}
	}
}

func TestDiffRunes_MinimalEditForSingleChange(t *testing.T) {
	// One character changed in the middle should produce exactly one non-keep
	// op — the common prefix/suffix trim should handle everything else.
	ops := diffRunes([]rune("Hello"), []rune("Hallo"))
	changed := 0
	for _, op := range ops {
		if op.kind != opKeep {
			changed++
		}
	}
	if changed != 1 {
		t.Errorf("got %d non-keep ops, want 1: %+v", changed, ops)
	}
}

func TestDpDiff_ReplaysCorrectly(t *testing.T) {
	// Exercise dpDiff directly (no shared prefix or suffix to trim).
	old, new := []rune("kitten"), []rune("sitting")
	if got := applyOps(old, dpDiff(old, new)); got != "sitting" {
		t.Errorf("dpDiff replays to %q, want %q", got, "sitting")
	}
}

func TestMin3(t *testing.T) {
	tests := []struct{ a, b, c, want int }{
		{1, 2, 3, 1}, {3, 1, 2, 1}, {3, 2, 1, 1},
		{2, 2, 2, 2}, {-1, 0, 1, -1}, {5, 5, 1, 1},
	}
	for _, tt := range tests {
		if got := min3(tt.a, tt.b, tt.c); got != tt.want {
			t.Errorf("min3(%d,%d,%d) = %d, want %d", tt.a, tt.b, tt.c, got, tt.want)
		}
	}
}

// ── Character mapping ────────────────────────────────────────────────────────

func TestBuildCharMap_MapsEachCharToItsSubSpan(t *testing.T) {
	subs := []SubSpanInfo{{Text: "Hello"}, {Text: "World"}}
	cm := buildCharMap(subs, "HelloWorld")

	if len(cm) != 10 {
		t.Fatalf("got %d mappings, want 10", len(cm))
	}
	for i := 0; i < 5; i++ {
		if cm[i].subSpanIdx != 0 || cm[i].charInSpan != i {
			t.Errorf("mapping %d = %+v, want {0 %d}", i, cm[i], i)
		}
	}
	for i := 5; i < 10; i++ {
		if cm[i].subSpanIdx != 1 || cm[i].charInSpan != i-5 {
			t.Errorf("mapping %d = %+v, want {1 %d}", i, cm[i], i-5)
		}
	}
}

// A space the merger inserted between two sub-spans belongs to no sub-span and
// must be marked with index -1, or edits shift by one character.
func TestBuildCharMap_MarksMergeInsertedSpace(t *testing.T) {
	subs := []SubSpanInfo{{Text: "Hello"}, {Text: "World"}}
	cm := buildCharMap(subs, "Hello World")

	if len(cm) != 11 {
		t.Fatalf("got %d mappings, want 11", len(cm))
	}
	if cm[5].subSpanIdx != -1 {
		t.Errorf("mapping for the inserted space = %+v, want subSpanIdx -1", cm[5])
	}
	if cm[6].subSpanIdx != 1 || cm[6].charInSpan != 0 {
		t.Errorf("mapping after the space = %+v, want {1 0}", cm[6])
	}
}

func TestBuildCharMap_SingleSubSpan(t *testing.T) {
	cm := buildCharMap([]SubSpanInfo{{Text: "abc"}}, "abc")
	if len(cm) != 3 {
		t.Fatalf("got %d mappings, want 3", len(cm))
	}
	for i, m := range cm {
		if m.subSpanIdx != 0 || m.charInSpan != i {
			t.Errorf("mapping %d = %+v, want {0 %d}", i, m, i)
		}
	}
}

func TestBuildCharMap_Empty(t *testing.T) {
	if cm := buildCharMap(nil, ""); len(cm) != 0 {
		t.Errorf("got %d mappings, want 0", len(cm))
	}
}

// ── Number formatting ────────────────────────────────────────────────────────

func TestFormatFloat(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0.0"},
		{1, "1.0"},
		{100, "100.0"},
		{12.5, "12.5"},
		{-3.25, "-3.25"},
		{0.125, "0.125"},
		{72.0, "72.0"},
	}
	for _, tt := range tests {
		if got := formatFloat(tt.in); got != tt.want {
			t.Errorf("formatFloat(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Whatever the formatting, the output has to be a legal PDF numeric object —
// no exponent notation, which PDF does not accept. ISO 32000-1:2008, §7.3.3.
func TestFormatFloat_NeverUsesExponentNotation(t *testing.T) {
	for _, v := range []float64{1e-7, 1e21, 0.0000001, 123456789012.5} {
		if got := formatFloat(v); strings.ContainsAny(got, "eE") {
			t.Errorf("formatFloat(%v) = %q, which PDF cannot parse", v, got)
		}
	}
}

// ── Glyph encoding ───────────────────────────────────────────────────────────

func TestEncodeRuneToHex_ASCIIFallback(t *testing.T) {
	tests := []struct {
		r    rune
		want string
	}{
		{'A', "41"},
		{'a', "61"},
		{' ', "20"},
		{'0', "30"},
	}
	for _, tt := range tests {
		if got := encodeRuneToHex(tt.r, nil); got != tt.want {
			t.Errorf("encodeRuneToHex(%q, nil) = %q, want %q", tt.r, got, tt.want)
		}
	}
}

func TestEncodeRuneToHex_OutOfRangeBecomesSpace(t *testing.T) {
	if got := encodeRuneToHex('\U0001F600', nil); got != "20" {
		t.Errorf("got %q, want %q (space fallback)", got, "20")
	}
}

func TestEncodeRuneToHex_UsesEncodingTable(t *testing.T) {
	fi := &fontInfo{encoding: &winAnsiEncoding}
	// € is 0x80 in WinAnsi but U+20AC in Unicode — a straight byte cast would
	// be wrong, so this proves the reverse lookup runs.
	if got := encodeRuneToHex('\u20AC', fi); got != "80" {
		t.Errorf("encodeRuneToHex(€) = %q, want %q", got, "80")
	}
}

func TestEncodeRuneToHex_CIDUsesGlyphIDs(t *testing.T) {
	fi := &fontInfo{isCID: true, fromUnicode: map[rune]uint16{'A': 0x0024, ' ': 0x0003}}
	if got := encodeRuneToHex('A', fi); got != "0024" {
		t.Errorf("got %q, want %q", got, "0024")
	}
	// Unmapped runes fall back to the space glyph, not to a raw code point.
	if got := encodeRuneToHex('Z', fi); got != "0003" {
		t.Errorf("unmapped rune gave %q, want the space glyph %q", got, "0003")
	}
}

// ── Glyph widths — ISO 32000-1:2008, §9.2.4 ──────────────────────────────────

// Widths are in 1/1000 text-space units, so the advance is w/1000 × fontSize.
// The font size multiplies in; it does not cancel.
func TestGlyphWidthForRune_ScalesByFontSize(t *testing.T) {
	fi := &fontInfo{widths: map[uint16]int{'A': 722}, defaultWidth: 500}

	if got := glyphWidthForRune('A', fi, 12); !approx(got, 722*12/1000.0, 1e-9) {
		t.Errorf("got %v, want %v", got, 722*12/1000.0)
	}
	// Double the size, double the advance.
	if got := glyphWidthForRune('A', fi, 24); !approx(got, 722*24/1000.0, 1e-9) {
		t.Errorf("got %v, want %v", got, 722*24/1000.0)
	}
}

func TestGlyphWidthForRune_FallsBackToDefault(t *testing.T) {
	fi := &fontInfo{widths: map[uint16]int{'A': 722}, defaultWidth: 500}
	if got := glyphWidthForRune('Z', fi, 10); !approx(got, 5.0, 1e-9) {
		t.Errorf("got %v, want 5.0 (defaultWidth 500 at 10pt)", got)
	}
}

func TestGlyphWidthForRune_NilFont(t *testing.T) {
	if got := glyphWidthForRune('A', nil, 10); !approx(got, 5.0, 1e-9) {
		t.Errorf("got %v, want 5.0", got)
	}
}

func TestGlyphWidthForRune_CIDUsesGlyphTable(t *testing.T) {
	fi := &fontInfo{
		isCID:        true,
		fromUnicode:  map[rune]uint16{'A': 7},
		widths:       map[uint16]int{7: 600},
		defaultWidth: 1000,
	}
	if got := glyphWidthForRune('A', fi, 10); !approx(got, 6.0, 1e-9) {
		t.Errorf("got %v, want 6.0", got)
	}
}

// ── BT/ET block construction ─────────────────────────────────────────────────

func TestBuildBTBlock_Structure(t *testing.T) {
	fi := &fontInfo{widths: map[uint16]int{'A': 500, 'B': 500}, defaultWidth: 500}
	runs := []positionedRun{{
		fontName: "F1", tfSize: 12,
		tm:    Matrix{A: 1, D: 1, E: 72, F: 700},
		chars: []rune("AB"),
	}}
	block := string(buildBTBlock([]byte("BT\n"), runs, pageFonts{"F1": fi}))

	for _, want := range []string{"BT\n", "/F1 12.0 Tf\n", "1.0 0.0 0.0 1.0 72.0 700.0 Tm\n", "ET\n"} {
		if !strings.Contains(block, want) {
			t.Errorf("block missing %q:\n%s", want, block)
		}
	}
	// First glyph shows at the Tm origin; the second is offset by the first
	// glyph's advance (500/1000 × 12 = 6).
	if !strings.Contains(block, "<41> Tj") {
		t.Errorf("block missing the first glyph:\n%s", block)
	}
	if !strings.Contains(block, "6.0 0 Td <42> Tj") {
		t.Errorf("block missing the advanced second glyph:\n%s", block)
	}
}

// The prologue is copied verbatim so state the block set before drawing —
// colour, Tc, Tz, the render mode — is not lost.
func TestBuildBTBlock_CopiesPrologueVerbatim(t *testing.T) {
	prologue := []byte("BT\n/F1 12 Tf\n1 0 0 rg\n3 Tc\n")
	runs := []positionedRun{{fontName: "F1", tfSize: 12, tm: Identity(), chars: []rune("A")}}

	block := string(buildBTBlock(prologue, runs, nil))
	if !strings.HasPrefix(block, string(prologue)) {
		t.Errorf("prologue was not preserved:\n%s", block)
	}
}

func TestBuildBTBlock_EmptyTextStillWellFormed(t *testing.T) {
	block := string(buildBTBlock([]byte("BT\n"), nil, nil))
	if !strings.HasPrefix(block, "BT\n") || !strings.HasSuffix(block, "ET\n") {
		t.Errorf("block is not bracketed by BT/ET:\n%s", block)
	}
	if strings.Contains(block, "Tj") {
		t.Errorf("empty text should show no glyphs:\n%s", block)
	}
}

// A rewritten block has to survive a round trip through the decoder.
func TestBuildBTBlock_IsReparseable(t *testing.T) {
	fi := &fontInfo{encoding: &winAnsiEncoding, widths: map[uint16]int{}, defaultWidth: 500}
	runs := []positionedRun{{
		fontName: "F1", tfSize: 12,
		tm:    Matrix{A: 1, D: 1, E: 72, F: 700},
		chars: []rune("Hi"),
	}}
	block := buildBTBlock([]byte("BT\n"), runs, pageFonts{"F1": fi})

	p := newStreamParser(nil, pageFonts{"F1": fi}, 1)
	p.parse(block, 0)
	spans := filterSpans(p.spans)

	if len(spans) == 0 {
		t.Fatalf("rewritten block produced no spans:\n%s", block)
	}
	var text strings.Builder
	for _, s := range spans {
		text.WriteString(s.Text)
	}
	if text.String() != "Hi" {
		t.Errorf("round trip gave %q, want %q\n%s", text.String(), "Hi", block)
	}
}

// ── End to end ───────────────────────────────────────────────────────────────

func TestEditMergedSpans_ReplacesText(t *testing.T) {
	path := pdftest.TextPage(t, "in.pdf", "BT\n/F1 12 Tf\n72 700 Td\n(Hello) Tj\nET\n")

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	s := spans[0]

	subs := []SubSpanInfo{{
		StreamIndex: s.StreamIndex, OpStart: s.OpStart, OpEnd: s.OpEnd,
		Text: s.Text, FontName: s.FontName,
		BlockStart: s.BlockStart, BlockEnd: s.BlockEnd, TfSize: s.TfSize,
		TmA: s.TmA, TmB: s.TmB, TmC: s.TmC, TmD: s.TmD, TmE: s.TmE, TmF: s.TmF,
	}}

	out := path + ".out.pdf"
	res := New().EditMergedSpans(path, out, 1, subs, "Hello", "Goodbye")
	if res.Error != "" {
		t.Fatalf("EditMergedSpans: %s", res.Error)
	}

	got, err := ExtractText(out, 1)
	if err != nil {
		t.Fatalf("ExtractText(out): %v", err)
	}
	var text strings.Builder
	for _, sp := range got {
		text.WriteString(sp.Text)
	}
	if text.String() != "Goodbye" {
		t.Errorf("got %q, want %q", text.String(), "Goodbye")
	}
}

func TestEditMergedSpans_RejectsEmptyInput(t *testing.T) {
	path := pdftest.TextPage(t, "in.pdf", "BT\n/F1 12 Tf\n72 700 Td\n(Hello) Tj\nET\n")
	if res := New().EditMergedSpans(path, path+".out.pdf", 1, nil, "Hello", "Bye"); res.Error == "" {
		t.Error("expected an error when no sub-spans are supplied")
	}
}

func TestEditMergedSpans_RejectsBadPage(t *testing.T) {
	path := pdftest.TextPage(t, "in.pdf", "BT\n/F1 12 Tf\n72 700 Td\n(Hello) Tj\nET\n")
	subs := []SubSpanInfo{{Text: "Hello", FontName: "F1", TfSize: 12}}
	for _, page := range []int{0, -1, 5} {
		if res := New().EditMergedSpans(path, path+".out.pdf", page, subs, "Hello", "Bye"); res.Error == "" {
			t.Errorf("page %d was accepted", page)
		}
	}
}
