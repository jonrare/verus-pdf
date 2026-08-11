package edit

import (
	"math"
	"testing"

	"veruspdf/backend/internal/pdftest"
)

// Text state and advance handling — ISO 32000-1:2008, §8.4.1, §9.3, §9.4.4.

// ── q/Q and the text state (§8.4.1) ──────────────────────────────────────────

// The text state parameters are part of the graphics state, so Q must restore
// them. Keeping the font name in the graphics state but its size outside meant
// a Q left the two disagreeing.
func TestGraphicsState_QRestoresTextStateParameters(t *testing.T) {
	stream := []byte(
		"/F1 10 Tf 2 Tc 3 Tw 50 Tz 14 TL 5 Ts\n" +
			"q\n" +
			"/F1 30 Tf 9 Tc 9 Tw 200 Tz 40 TL 9 Ts\n" +
			"Q\n")

	p := newStreamParser(nil, nil, 1)
	p.parse(stream, 0)

	tests := []struct {
		name string
		got  float64
		want float64
	}{
		{"Tf size", p.gs.tfSize, 10},
		{"Tc", p.gs.charSpace, 2},
		{"Tw", p.gs.wordSpace, 3},
		{"Tz", p.gs.hScale, 50},
		{"TL", p.gs.leading, 14},
		{"Ts", p.gs.rise, 5},
	}
	for _, tt := range tests {
		if !approx(tt.got, tt.want, 1e-9) {
			t.Errorf("%s after Q = %v, want %v restored", tt.name, tt.got, tt.want)
		}
	}
}

// BT resets the text matrices, but those are not graphics state and Q must not
// be what restores them (§9.4.1).
func TestGraphicsState_BTResetsTextMatrix(t *testing.T) {
	p := newStreamParser(nil, nil, 1)
	p.parse([]byte("BT 100 200 Td ET\nBT ET"), 0)

	if !p.ts.tm.IsIdentity() {
		t.Errorf("text matrix after a fresh BT = %+v, want identity", p.ts.tm)
	}
}

// ── Advance under a transformed text matrix (§9.4.4) ─────────────────────────

// Tm' = Translate(tx, 0) × Tm. Under a scaled text matrix the advance scales
// with it; adding tx straight to Tm.E would not.
func TestAdvance_ScalesWithTextMatrix(t *testing.T) {
	// Two identical strings, the second under a 2× scaled text matrix.
	single := newStreamParser(nil, nil, 1)
	single.parse([]byte("BT /F1 10 Tf 1 0 0 1 0 0 Tm (AB) Tj ET"), 0)

	doubled := newStreamParser(nil, nil, 1)
	doubled.parse([]byte("BT /F1 10 Tf 2 0 0 2 0 0 Tm (AB) Tj ET"), 0)

	if len(single.spans) != 1 || len(doubled.spans) != 1 {
		t.Fatalf("got %d and %d spans, want 1 each", len(single.spans), len(doubled.spans))
	}
	if !approx(doubled.spans[0].Width, single.spans[0].Width*2, 1e-6) {
		t.Errorf("width under a 2x text matrix = %v, want %v (2x the unscaled %v)",
			doubled.spans[0].Width, single.spans[0].Width*2, single.spans[0].Width)
	}
}

// Under a rotated text matrix the origin travels diagonally. Adding the advance
// to Tm.E alone would move it horizontally and the position would drift.
func TestAdvance_FollowsRotatedTextMatrix(t *testing.T) {
	// 90° rotation: [0 1 -1 0 0 0]. Text runs up the page.
	p := newStreamParser(nil, nil, 1)
	p.parse([]byte("BT /F1 10 Tf 0 1 -1 0 100 100 Tm (AB) Tj (CD) Tj ET"), 0)

	if len(p.spans) != 2 {
		t.Fatalf("got %d spans, want 2", len(p.spans))
	}
	first, second := p.spans[0], p.spans[1]

	// The second span must start above the first, at the same X.
	if !approx(second.X, first.X, 0.001) {
		t.Errorf("X moved from %v to %v under a 90° rotation; it should not", first.X, second.X)
	}
	if second.Y <= first.Y {
		t.Errorf("Y went from %v to %v; rotated text should advance upward", first.Y, second.Y)
	}
}

// ── Tz (§9.3.4) ──────────────────────────────────────────────────────────────

// Horizontal scaling must be applied exactly once to a reported width. It used
// to be applied twice by Tj and once by TJ, so the two disagreed.
func TestTz_AppliedOnceAndConsistentlyAcrossTjAndTJ(t *testing.T) {
	width := func(stream string) float64 {
		p := newStreamParser(nil, nil, 1)
		p.parse([]byte(stream), 0)
		if len(p.spans) != 1 {
			t.Fatalf("got %d spans for %q, want 1", len(p.spans), stream)
		}
		return p.spans[0].Width
	}

	tjPlain := width("BT /F1 10 Tf 100 Tz 0 0 Td (AB) Tj ET")
	tjHalf := width("BT /F1 10 Tf 50 Tz 0 0 Td (AB) Tj ET")
	tjArrPlain := width("BT /F1 10 Tf 100 Tz 0 0 Td [(AB)] TJ ET")
	tjArrHalf := width("BT /F1 10 Tf 50 Tz 0 0 Td [(AB)] TJ ET")

	if !approx(tjHalf, tjPlain/2, 1e-6) {
		t.Errorf("Tj at 50%% Tz = %v, want %v (half of %v)", tjHalf, tjPlain/2, tjPlain)
	}
	if !approx(tjArrHalf, tjArrPlain/2, 1e-6) {
		t.Errorf("TJ at 50%% Tz = %v, want %v (half of %v)", tjArrHalf, tjArrPlain/2, tjArrPlain)
	}
	if !approx(tjHalf, tjArrHalf, 1e-6) {
		t.Errorf("Tj and TJ disagree at 50%% Tz: %v vs %v", tjHalf, tjArrHalf)
	}
}

// ── Tc and Tw (§9.3.2, §9.3.3) ───────────────────────────────────────────────

// Character spacing adds to the advance once per glyph.
func TestTc_WidensTheAdvance(t *testing.T) {
	plain := newStreamParser(nil, nil, 1)
	plain.parse([]byte("BT /F1 10 Tf 0 Tc 0 0 Td (ABC) Tj ET"), 0)

	spaced := newStreamParser(nil, nil, 1)
	spaced.parse([]byte("BT /F1 10 Tf 5 Tc 0 0 Td (ABC) Tj ET"), 0)

	if len(plain.spans) != 1 || len(spaced.spans) != 1 {
		t.Fatal("expected one span from each stream")
	}
	// Three glyphs at 5 units of extra spacing each.
	want := plain.spans[0].Width + 15
	if !approx(spaced.spans[0].Width, want, 1e-6) {
		t.Errorf("width with Tc 5 = %v, want %v", spaced.spans[0].Width, want)
	}
}

// Word spacing applies to the space character only.
func TestTw_AppliesToSpacesOnly(t *testing.T) {
	noSpaces := newStreamParser(nil, nil, 1)
	noSpaces.parse([]byte("BT /F1 10 Tf 7 Tw 0 0 Td (ABC) Tj ET"), 0)

	noSpacesBaseline := newStreamParser(nil, nil, 1)
	noSpacesBaseline.parse([]byte("BT /F1 10 Tf 0 Tw 0 0 Td (ABC) Tj ET"), 0)

	if !approx(noSpaces.spans[0].Width, noSpacesBaseline.spans[0].Width, 1e-6) {
		t.Errorf("Tw changed the width of a string with no spaces: %v vs %v",
			noSpaces.spans[0].Width, noSpacesBaseline.spans[0].Width)
	}

	withSpace := newStreamParser(nil, nil, 1)
	withSpace.parse([]byte("BT /F1 10 Tf 7 Tw 0 0 Td (A B) Tj ET"), 0)

	withSpaceBaseline := newStreamParser(nil, nil, 1)
	withSpaceBaseline.parse([]byte("BT /F1 10 Tf 0 Tw 0 0 Td (A B) Tj ET"), 0)

	want := withSpaceBaseline.spans[0].Width + 7
	if !approx(withSpace.spans[0].Width, want, 1e-6) {
		t.Errorf("width with Tw 7 and one space = %v, want %v", withSpace.spans[0].Width, want)
	}
}

// ── Nested Form XObject resources (§8.10.1) ──────────────────────────────────

// An XObject name inside a form resolves against that form's /Resources, not
// the page's. Resolving against the page meant nested forms silently vanished.
func TestFormXObject_NestedResolvesAgainstFormResources(t *testing.T) {
	path := pdftest.NestedFormXObjectPage(t, "nested.pdf",
		"BT /F1 12 Tf 72 700 Td (Page) Tj ET\n/X1 Do\n",
		"BT /F1 12 Tf 72 600 Td (Outer) Tj ET\n/Inner Do\n",
		"BT /F1 12 Tf 72 500 Td (Nested) Tj ET\n")

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}

	found := map[string]bool{}
	for _, s := range spans {
		found[s.Text] = true
	}
	for _, want := range []string{"Page", "Outer", "Nested"} {
		if !found[want] {
			t.Errorf("%q was not extracted; got %v", want, found)
		}
	}
}

// A form's Matrix positions its content on the page (§8.10.1).
func TestFormXObject_MatrixOffsetsContent(t *testing.T) {
	path := pdftest.FormXObjectPage(t, "xobj.pdf",
		"BT /F1 12 Tf 0 0 Td (Origin) Tj ET\n/X1 Do\n",
		"BT /F1 12 Tf 0 0 Td (Shifted) Tj ET\n")

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}

	var origin, shifted *TextSpan
	for i := range spans {
		switch spans[i].Text {
		case "Origin":
			origin = &spans[i]
		case "Shifted":
			shifted = &spans[i]
		}
	}
	if origin == nil || shifted == nil {
		t.Fatalf("expected both spans, got %+v", spans)
	}
	// The fixture gives the form a translation matrix, so its content must not
	// land at the same place as the page's own text at the same Td.
	if math.Abs(shifted.X-origin.X) < 1 && math.Abs(shifted.Y-origin.Y) < 1 {
		t.Errorf("form content at (%v,%v) ignored the form matrix; page text is at (%v,%v)",
			shifted.X, shifted.Y, origin.X, origin.Y)
	}
}
