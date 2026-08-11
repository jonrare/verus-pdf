package edit

import "testing"

// Span merging has two regimes: one where the decoder resolved real glyph
// widths, and one where it did not and has to infer spacing from observed
// advances. Both need coverage — the second is the common case for the
// standard-14 fonts, which carry no /Widths array.

func spansAt(fontSize float64, texts []string, xs []float64, widths []float64) []TextSpan {
	out := make([]TextSpan, len(texts))
	for i := range texts {
		w := 0.0
		if widths != nil {
			w = widths[i]
		}
		out[i] = TextSpan{
			Text: texts[i], X: xs[i], Y: 100, Width: w,
			FontName: "F1", FontSize: fontSize,
		}
	}
	return out
}

func mergedTexts(spans []TextSpan) []string {
	out := make([]string, len(spans))
	for i, s := range spans {
		out[i] = s.Text
	}
	return out
}

func assertMerged(t *testing.T, got []TextSpan, want ...string) {
	t.Helper()
	texts := mergedTexts(got)
	if len(texts) != len(want) {
		t.Fatalf("got %d spans %q, want %d %q", len(texts), texts, len(want), want)
	}
	for i := range want {
		if texts[i] != want[i] {
			t.Errorf("span %d = %q, want %q (all: %q)", i, texts[i], want[i], texts)
		}
	}
}

// ── Known widths ─────────────────────────────────────────────────────────────

func TestMerge_KnownWidths_TightRunHasNoSpaces(t *testing.T) {
	// Each span ends exactly where the next begins.
	spans := spansAt(12,
		[]string{"He", "ll", "o"},
		[]float64{10, 24, 38},
		[]float64{14, 14, 7})
	assertMerged(t, mergeAdjacentSpans(spans), "Hello")
}

func TestMerge_KnownWidths_WordGapBecomesSpace(t *testing.T) {
	// "Hello" ends at 40; "World" starts at 44 — a 4pt gap at 12pt is a space.
	spans := spansAt(12,
		[]string{"Hello", "World"},
		[]float64{10, 44},
		[]float64{30, 30})
	assertMerged(t, mergeAdjacentSpans(spans), "Hello World")
}

func TestMerge_KnownWidths_ColumnGapSplits(t *testing.T) {
	// "Left" ends at 40; "Right" starts at 200 — far beyond a word space, so
	// this is a table column, not a continuation.
	spans := spansAt(12,
		[]string{"Left", "Right"},
		[]float64{10, 200},
		[]float64{30, 30})
	assertMerged(t, mergeAdjacentSpans(spans), "Left", "Right")
}

// ── Unknown widths ───────────────────────────────────────────────────────────

func TestMerge_UnknownWidths_PerCharacterRunJoins(t *testing.T) {
	// Real Helvetica advances for "Hello" at 12pt.
	spans := spansAt(12,
		[]string{"H", "e", "l", "l", "o"},
		[]float64{10, 18.7, 25.4, 28.1, 30.8},
		nil)
	assertMerged(t, mergeAdjacentSpans(spans), "Hello")
}

// A word space is only claimed when the advance is too wide to be one glyph.
func TestMerge_UnknownWidths_ImplausibleAdvanceBecomesSpace(t *testing.T) {
	// 20pt between origins at 12pt is 1.67 em — no single glyph is that wide.
	spans := spansAt(12, []string{"a", "b"}, []float64{10, 30}, nil)
	assertMerged(t, mergeAdjacentSpans(spans), "a b")
}

// Documents a real limitation rather than pretending it away.
//
// With per-character spans and no glyph widths, word spaces cannot be
// recovered from geometry. These are the true Helvetica 12pt origins for
// "Hi there": the step from 'i' to 't' is 6.000 and *contains a space*, while
// the step from 'h' to 'e' is 6.672 and does not. The step containing the
// space is the smaller one, so no threshold can separate them.
//
// The merger therefore drops the space rather than shattering the word. When
// standard-14 font metrics land, widths become known, this path stops being
// reached, and this test should be replaced by one asserting "Hi there".
func TestMerge_UnknownWidths_SpaceRecoveryIsNotPossible(t *testing.T) {
	spans := spansAt(12,
		[]string{"H", "i", "t", "h", "e", "r", "e"},
		[]float64{10, 18.664, 24.664, 28.0, 34.672, 41.344, 45.34},
		nil)

	got := mergedTexts(mergeAdjacentSpans(spans))
	if len(got) != 1 {
		t.Fatalf("got %d spans %q, want the word kept whole as 1 span", len(got), got)
	}
	if got[0] != "Hithere" {
		t.Errorf("got %q, want %q — the space is unrecoverable here, but no "+
			"character may be lost and the run must not shatter", got[0], "Hithere")
	}
}

func TestMerge_UnknownWidths_LargeJumpSplits(t *testing.T) {
	spans := spansAt(12, []string{"a", "b"}, []float64{10, 500}, nil)
	assertMerged(t, mergeAdjacentSpans(spans), "a", "b")
}

func TestMerge_UnknownWidths_BackwardsJumpSplits(t *testing.T) {
	spans := spansAt(12, []string{"a", "b"}, []float64{100, 10}, nil)
	assertMerged(t, mergeAdjacentSpans(spans), "a", "b")
}

// ── Split conditions independent of geometry ─────────────────────────────────

func TestMerge_DifferentLinesSplit(t *testing.T) {
	spans := []TextSpan{
		{Text: "top", X: 10, Y: 100, FontName: "F1", FontSize: 12},
		{Text: "bottom", X: 10, Y: 80, FontName: "F1", FontSize: 12},
	}
	assertMerged(t, mergeAdjacentSpans(spans), "top", "bottom")
}

func TestMerge_DifferentSizesSplit(t *testing.T) {
	spans := []TextSpan{
		{Text: "big", X: 10, Y: 100, FontName: "F1", FontSize: 24},
		{Text: "small", X: 40, Y: 100, FontName: "F1", FontSize: 10},
	}
	assertMerged(t, mergeAdjacentSpans(spans), "big", "small")
}

func TestMerge_DifferentRotationsSplit(t *testing.T) {
	spans := []TextSpan{
		{Text: "flat", X: 10, Y: 100, FontName: "F1", FontSize: 12, Rotation: 0},
		{Text: "turned", X: 20, Y: 100, FontName: "F1", FontSize: 12, Rotation: 90},
	}
	assertMerged(t, mergeAdjacentSpans(spans), "flat", "turned")
}

// Sub-pixel baseline jitter is common in generated PDFs and must not split a
// line.
func TestMerge_TinyBaselineJitterStillMerges(t *testing.T) {
	spans := []TextSpan{
		{Text: "ab", X: 10, Y: 100.0, Width: 12, FontName: "F1", FontSize: 12},
		{Text: "cd", X: 22, Y: 100.3, Width: 12, FontName: "F1", FontSize: 12},
	}
	assertMerged(t, mergeAdjacentSpans(spans), "abcd")
}

// ── Structural guarantees ────────────────────────────────────────────────────

func TestMerge_Empty(t *testing.T) {
	if got := mergeAdjacentSpans(nil); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestMerge_SingleSpanPassesThrough(t *testing.T) {
	in := []TextSpan{{Text: "solo", X: 10, Y: 100, FontName: "F1", FontSize: 12}}
	assertMerged(t, mergeAdjacentSpans(in), "solo")
}

// Merging must never drop characters — the concatenation of the merged texts,
// with inserted spaces removed, has to equal the original concatenation.
func TestMerge_PreservesAllCharacters(t *testing.T) {
	spans := spansAt(12,
		[]string{"Lorem", "ipsum", "dolor", "sit"},
		[]float64{10, 45, 80, 200},
		[]float64{30, 30, 30, 20})

	var original string
	for _, s := range spans {
		original += s.Text
	}

	var merged string
	for _, s := range mergeAdjacentSpans(spans) {
		merged += s.Text
	}
	stripped := ""
	for _, r := range merged {
		if r != ' ' {
			stripped += string(r)
		}
	}
	if stripped != original {
		t.Errorf("merging changed the characters: %q vs %q", stripped, original)
	}
}

// The byte range of a merged span must cover every sub-span it absorbed, or
// the editor rewrites less than the user selected.
func TestMerge_ExtendsByteRange(t *testing.T) {
	spans := []TextSpan{
		{Text: "ab", X: 10, Y: 100, Width: 12, FontName: "F1", FontSize: 12, OpStart: 5, OpEnd: 10},
		{Text: "cd", X: 22, Y: 100, Width: 12, FontName: "F1", FontSize: 12, OpStart: 11, OpEnd: 18},
	}
	got := mergeAdjacentSpans(spans)
	if len(got) != 1 {
		t.Fatalf("got %d spans, want 1", len(got))
	}
	if got[0].OpStart != 5 || got[0].OpEnd != 18 {
		t.Errorf("byte range = [%d,%d], want [5,18]", got[0].OpStart, got[0].OpEnd)
	}
}

// A merged width must come from a real measurement, never from an estimate
// stacked on an estimate.
func TestMerge_WidthOnlyGrowsFromMeasurements(t *testing.T) {
	spans := spansAt(12, []string{"a", "b", "c"}, []float64{10, 17, 24}, nil)
	got := mergeAdjacentSpans(spans)
	if len(got) != 1 {
		t.Fatalf("got %d spans, want 1", len(got))
	}
	if got[0].Width != 0 {
		t.Errorf("Width = %v, want 0 — no sub-span had a measured width", got[0].Width)
	}
}

func TestFilterSpans_DropsNonPrintable(t *testing.T) {
	spans := []TextSpan{
		{Text: "keep"},
		{Text: "��"},
		{Text: "\x00\x01"},
		{Text: "\t\n "},
		{Text: "also keep"},
	}
	got := filterSpans(spans)
	if len(got) != 2 {
		t.Fatalf("got %d spans %q, want 2", len(got), mergedTexts(got))
	}
	if got[0].Text != "keep" || got[1].Text != "also keep" {
		t.Errorf("got %q", mergedTexts(got))
	}
}
