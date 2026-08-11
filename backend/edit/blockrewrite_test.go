package edit

import (
	"os"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"veruspdf/backend/internal/pdftest"
)

// Block rewriting replaces a whole BT…ET block. Everything else the block drew
// has to come back out the other side.

func subSpanFor(s TextSpan) SubSpanInfo {
	return SubSpanInfo{
		StreamIndex: s.StreamIndex, OpStart: s.OpStart, OpEnd: s.OpEnd,
		Text: s.Text, FontName: s.FontName,
		BlockStart: s.BlockStart, BlockEnd: s.BlockEnd, TfSize: s.TfSize,
		TmA: s.TmA, TmB: s.TmB, TmC: s.TmC, TmD: s.TmD, TmE: s.TmE, TmF: s.TmF,
	}
}

func spanNamed(t *testing.T, spans []TextSpan, text string) TextSpan {
	t.Helper()
	for _, s := range spans {
		if s.Text == text {
			return s
		}
	}
	t.Fatalf("span %q not found in %+v", text, spans)
	return TextSpan{}
}

func textsOf(spans []TextSpan) []string {
	out := make([]string, len(spans))
	for i, s := range spans {
		out[i] = s.Text
	}
	return out
}

// flatten concatenates every span's text with spaces removed.
//
// A rebuilt block emits one Tj per glyph, so extraction returns per-character
// spans and the space glyphs are dropped by filterSpans as whitespace-only.
// Assertions about *content* should not depend on how glyphs happen to be
// chunked into operators, so compare flattened text.
func flatten(spans []TextSpan) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return strings.ReplaceAll(b.String(), " ", "")
}

// spansOnLine returns the spans sitting at the given baseline, in order.
func spansOnLine(spans []TextSpan, y float64) []TextSpan {
	var out []TextSpan
	for _, s := range spans {
		if approx(s.Y, y, 0.5) {
			out = append(out, s)
		}
	}
	return out
}

// Editing one line of a multi-line BT/ET block must not delete the others.
func TestEditMergedSpans_PreservesOtherLinesInTheBlock(t *testing.T) {
	path := pdftest.TextPage(t, "in.pdf",
		"BT\n/F1 12 Tf\n72 700 Td\n(First line) Tj\n0 -20 Td\n(Second line) Tj\n0 -20 Td\n(Third line) Tj\nET\n")

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3: %v", len(spans), textsOf(spans))
	}
	// All three share one BT/ET block — that is the situation under test.
	if spans[0].BlockStart != spans[2].BlockStart {
		t.Fatalf("fixture does not put all spans in one block")
	}

	target := spanNamed(t, spans, "Second line")
	out := path + ".out.pdf"
	res := New().EditMergedSpans(path, out, 1,
		[]SubSpanInfo{subSpanFor(target)}, "Second line", "Middle line")
	if res.Error != "" {
		t.Fatalf("EditMergedSpans: %s", res.Error)
	}

	got, err := ExtractText(out, 1)
	if err != nil {
		t.Fatalf("ExtractText(out): %v", err)
	}
	content := flatten(got)
	for _, want := range []string{"Firstline", "Middleline", "Thirdline"} {
		if !strings.Contains(content, want) {
			t.Errorf("%q is missing after the edit; block now reads %q", want, content)
		}
	}
	if strings.Contains(content, "Secondline") {
		t.Errorf("the old text survived: %q", content)
	}
}

// The untouched lines must also stay where they were.
func TestEditMergedSpans_PreservesPositionsOfUntouchedLines(t *testing.T) {
	path := pdftest.TextPage(t, "in.pdf",
		"BT\n/F1 12 Tf\n72 700 Td\n(First line) Tj\n0 -20 Td\n(Second line) Tj\nET\n")

	before, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	first := spanNamed(t, before, "First line")
	target := spanNamed(t, before, "Second line")

	out := path + ".out.pdf"
	if res := New().EditMergedSpans(path, out, 1,
		[]SubSpanInfo{subSpanFor(target)}, "Second line", "Changed"); res.Error != "" {
		t.Fatalf("EditMergedSpans: %s", res.Error)
	}

	after, err := ExtractText(out, 1)
	if err != nil {
		t.Fatalf("ExtractText(out): %v", err)
	}
	untouchedLine := spansOnLine(after, first.Y)
	if len(untouchedLine) == 0 {
		t.Fatalf("nothing remains at the untouched line's baseline %v; spans are %v",
			first.Y, textsOf(after))
	}
	if !approx(untouchedLine[0].X, first.X, 0.5) {
		t.Errorf("untouched line starts at X %v, want %v", untouchedLine[0].X, first.X)
	}

	editedLine := spansOnLine(after, target.Y)
	if len(editedLine) == 0 {
		t.Fatalf("nothing remains at the edited line's baseline %v; spans are %v",
			target.Y, textsOf(after))
	}
	if !approx(editedLine[0].X, target.X, 0.5) {
		t.Errorf("edited line starts at X %v, want %v", editedLine[0].X, target.X)
	}
	if got := flatten(editedLine); got != "Changed" {
		t.Errorf("edited line reads %q, want %q", got, "Changed")
	}
	if got := flatten(untouchedLine); got != "Firstline" {
		t.Errorf("untouched line reads %q, want %q", got, "Firstline")
	}
}

// State set before the first text in a block — colour, Tc, Tz, the render mode
// — is part of how that block draws and must survive the rewrite.
func TestEditMergedSpans_PreservesBlockPrologue(t *testing.T) {
	path := pdftest.TextPage(t, "in.pdf",
		"BT\n/F1 12 Tf\n1 0 0 rg\n2 Tc\n90 Tz\n72 700 Td\n(Coloured) Tj\nET\n")

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	target := spanNamed(t, spans, "Coloured")

	out := path + ".out.pdf"
	if res := New().EditMergedSpans(path, out, 1,
		[]SubSpanInfo{subSpanFor(target)}, "Coloured", "Recoloured"); res.Error != "" {
		t.Fatalf("EditMergedSpans: %s", res.Error)
	}

	stream := pageStream(t, out, 1)
	for _, want := range []string{"1 0 0 rg", "2 Tc", "90 Tz"} {
		if !strings.Contains(stream, want) {
			t.Errorf("%q was dropped from the block:\n%s", want, stream)
		}
	}
}

// pageStream returns a page's decoded content streams, concatenated. Tests use
// it to assert on the bytes the editors actually wrote.
func pageStream(t *testing.T, path string, pageNum int) string {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	ctx, err := api.ReadValidateAndOptimize(f, model.NewDefaultConfiguration())
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	streams, _, err := pageContentStreamsWithRefs(ctx, pageNum)
	if err != nil {
		t.Fatalf("content streams: %v", err)
	}

	var b strings.Builder
	for _, st := range streams {
		b.Write(st)
	}
	return b.String()
}

// Editing two lines of the same block at once must apply both.
func TestEditMergedSpans_MultipleSpansInOneBlock(t *testing.T) {
	path := pdftest.TextPage(t, "in.pdf",
		"BT\n/F1 12 Tf\n72 700 Td\n(alpha) Tj\n0 -20 Td\n(beta) Tj\n0 -20 Td\n(gamma) Tj\nET\n")

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	a := spanNamed(t, spans, "alpha")
	g := spanNamed(t, spans, "gamma")

	out := path + ".out.pdf"
	res := New().EditMergedSpans(path, out, 1,
		[]SubSpanInfo{subSpanFor(a), subSpanFor(g)}, "alphagamma", "ALPHAGAMMA")
	if res.Error != "" {
		t.Fatalf("EditMergedSpans: %s", res.Error)
	}

	got, err := ExtractText(out, 1)
	if err != nil {
		t.Fatalf("ExtractText(out): %v", err)
	}
	content := flatten(got)
	if !strings.Contains(content, "beta") {
		t.Errorf("the untouched middle line was lost: %q", content)
	}
	if !strings.Contains(content, "ALPHA") || !strings.Contains(content, "GAMMA") {
		t.Errorf("both edits should have applied: %q", content)
	}
}

// Blocks the edit does not touch must be left byte-for-byte alone.
func TestEditMergedSpans_LeavesOtherBlocksUntouched(t *testing.T) {
	path := pdftest.TextPage(t, "in.pdf",
		"BT /F1 12 Tf 72 700 Td (Block one) Tj ET\nBT /F1 12 Tf 72 600 Td (Block two) Tj ET\n")

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	target := spanNamed(t, spans, "Block one")

	out := path + ".out.pdf"
	if res := New().EditMergedSpans(path, out, 1,
		[]SubSpanInfo{subSpanFor(target)}, "Block one", "Block ONE"); res.Error != "" {
		t.Fatalf("EditMergedSpans: %s", res.Error)
	}

	got, err := ExtractText(out, 1)
	if err != nil {
		t.Fatalf("ExtractText(out): %v", err)
	}
	// The untouched block keeps its original operator structure, so its text
	// still comes back as one span.
	if !strings.Contains(strings.Join(textsOf(got), "|"), "Block two") {
		t.Errorf("an unrelated block was damaged: %v", textsOf(got))
	}
	if !strings.Contains(flatten(got), "BlockONE") {
		t.Errorf("the edit did not apply: %q", flatten(got))
	}
}
