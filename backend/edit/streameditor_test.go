package edit

import (
	"strings"
	"testing"

	"veruspdf/backend/internal/pdftest"
)

// ── String escaping — ISO 32000-1:2008, §7.3.4.2 ─────────────────────────────

func TestEscapePDFString(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Hello", "Hello"},
		{"open paren", "a(b", `a\(b`},
		{"close paren", "a)b", `a\)b`},
		{"backslash", `a\b`, `a\\b`},
		{"balanced pair", "(x)", `\(x\)`},
		{"all three", `(\)`, `\(\\\)`},
		{"empty", "", ""},
		// Latin-1 range: one byte per rune, NOT Go's UTF-8 encoding.
		{"latin1 e-acute", "é", "\xe9"},
		{"latin1 y-diaeresis", "ÿ", "\xff"},
		// Out of range for a simple font encoding table.
		{"out of range", "€", "?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapePDFString(tt.in); got != tt.want {
				t.Errorf("escapePDFString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// A non-ASCII rune must occupy exactly one byte in the stream. Emitting UTF-8
// here is what turns é into Ã© when the font decodes it as WinAnsi.
func TestEscapePDFString_NonASCIIIsSingleByte(t *testing.T) {
	got := escapePDFString("café")
	if len(got) != 4 {
		t.Errorf("len = %d, want 4 (one byte per character), got %q", len(got), got)
	}
	if got[3] != 0xE9 {
		t.Errorf("last byte = %#x, want 0xe9 (WinAnsi é)", got[3])
	}
}

func TestUnescapePDFLiteral(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Hello", "Hello"},
		{`a\(b`, "a(b"},
		{`a\)b`, "a)b"},
		{`a\\b`, `a\b`},
		{`\n`, "\n"},
		{`\r`, "\r"},
		{`\t`, "\t"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := unescapePDFLiteral(tt.in); got != tt.want {
			t.Errorf("unescapePDFLiteral(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEscapeUnescapeRoundTrip(t *testing.T) {
	for _, s := range []string{"Hello", "a(b)c", `back\slash`, `((()))`, `\\`, "mixed (a) \\ b"} {
		if got := unescapePDFLiteral(escapePDFString(s)); got != s {
			t.Errorf("round trip %q -> %q", s, got)
		}
	}
}

// ── Length fitting ───────────────────────────────────────────────────────────

func TestFitToLength(t *testing.T) {
	tests := []struct {
		name                      string
		in                        string
		origLen                   int
		want                      string
		wantTruncated, wantPadded bool
	}{
		{"exact", "abcd", 4, "abcd", false, false},
		{"pad", "ab", 4, "ab  ", false, true},
		{"truncate", "abcdef", 4, "abcd", true, false},
		{"empty to padded", "", 3, "   ", false, true},
		{"zero budget", "abc", 0, "", true, false},
		// Runes, not bytes: a 3-character accented string must survive a
		// 3-character budget intact.
		{"multibyte fits", "éàü", 3, "éàü", false, false},
		{"multibyte truncates on runes", "éàü", 2, "éà", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, truncated, padded := fitToLength(tt.in, tt.origLen)
			if got != tt.want || truncated != tt.wantTruncated || padded != tt.wantPadded {
				t.Errorf("fitToLength(%q, %d) = (%q, %v, %v), want (%q, %v, %v)",
					tt.in, tt.origLen, got, truncated, padded, tt.want, tt.wantTruncated, tt.wantPadded)
			}
		})
	}
}

// Truncation happens before escaping, so a cut can never land between a
// backslash and the character it escapes.
func TestBuildLiteralReplacement_TruncationNeverSplitsEscape(t *testing.T) {
	// "ab(cd" cut to 3 characters is "ab(" -> escaped "ab\(" -> "(ab\()".
	got, actual, truncated, padded := buildLiteralReplacement("ab(cd", 3)

	if !truncated || padded {
		t.Errorf("truncated=%v padded=%v, want true/false", truncated, padded)
	}
	if actual != "ab(" {
		t.Errorf("actual = %q, want %q", actual, "ab(")
	}
	if string(got) != `(ab\()` {
		t.Fatalf("replacement = %q, want %q", got, `(ab\()`)
	}
	// The escape must be intact: an odd number of trailing backslashes before
	// the closing paren would mean the delimiter was escaped away.
	inner := string(got[1 : len(got)-1])
	if unescapePDFLiteral(inner) != actual {
		t.Errorf("inner %q does not unescape to %q", inner, actual)
	}
}

func TestBuildLiteralReplacement_Padding(t *testing.T) {
	got, actual, truncated, padded := buildLiteralReplacement("hi", 5)
	if truncated || !padded {
		t.Errorf("truncated=%v padded=%v, want false/true", truncated, padded)
	}
	if actual != "hi   " {
		t.Errorf("actual = %q, want %q", actual, "hi   ")
	}
	if string(got) != "(hi   )" {
		t.Errorf("replacement = %q, want %q", got, "(hi   )")
	}
}

func TestBuildHexReplacement(t *testing.T) {
	got, actual, truncated, padded := buildHexReplacement("Hi", 2)
	if truncated || padded {
		t.Errorf("truncated=%v padded=%v, want false/false", truncated, padded)
	}
	if actual != "Hi" {
		t.Errorf("actual = %q, want %q", actual, "Hi")
	}
	if string(got) != "<4869>" {
		t.Errorf("replacement = %q, want %q", got, "<4869>")
	}
}

func TestBuildHexReplacement_PadsAndTruncates(t *testing.T) {
	// Padded to 4 with spaces (0x20).
	if got, _, _, padded := buildHexReplacement("Hi", 4); string(got) != "<48692020>" || !padded {
		t.Errorf("pad: got %q padded=%v, want <48692020> true", got, padded)
	}
	// Truncated to 1.
	if got, _, truncated, _ := buildHexReplacement("Hi", 1); string(got) != "<48>" || !truncated {
		t.Errorf("truncate: got %q truncated=%v, want <48> true", got, truncated)
	}
}

// Every byte written into a hex string must be a single byte, so the digit
// count is always exactly twice the character count.
func TestBuildHexReplacement_OneBytePerRune(t *testing.T) {
	got, _, _, _ := buildHexReplacement("café", 4)
	digits := len(got) - 2 // strip < >
	if digits != 8 {
		t.Errorf("got %q with %d hex digits, want 8 (2 per character)", got, digits)
	}
	if !strings.HasSuffix(string(got), "e9>") {
		t.Errorf("got %q, want it to end with the WinAnsi byte for é (e9)", got)
	}
}

// ── End to end against a real PDF ────────────────────────────────────────────

func TestReplaceSpanText_RewritesLiteral(t *testing.T) {
	path := pdftest.TextPage(t, "in.pdf", "BT\n/F1 12 Tf\n72 700 Td\n(Hello World) Tj\nET\n")

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	span := spans[0]

	out := path + ".out.pdf"
	svc := New()
	res := svc.ReplaceSpanText(path, out, 1, span.StreamIndex, span.OpStart, span.OpEnd, "Howdy Earth")
	if res.Error != "" {
		t.Fatalf("ReplaceSpanText: %s", res.Error)
	}

	got, err := ExtractText(out, 1)
	if err != nil {
		t.Fatalf("ExtractText(out): %v", err)
	}
	if len(got) != 1 || got[0].Text != "Howdy Earth" {
		t.Fatalf("got %+v, want single span %q", got, "Howdy Earth")
	}
}

// Parentheses in replacement text must be escaped, or the string terminates
// early and the rest of the content stream is reinterpreted as operators.
func TestReplaceSpanText_EscapesParensInReplacement(t *testing.T) {
	path := pdftest.TextPage(t, "in.pdf", "BT\n/F1 12 Tf\n72 700 Td\n(Smith Junior) Tj\nET\n")

	spans, err := ExtractText(path, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	span := spans[0]

	out := path + ".out.pdf"
	res := New().ReplaceSpanText(path, out, 1, span.StreamIndex, span.OpStart, span.OpEnd, "Smith (Jr.)")
	if res.Error != "" {
		t.Fatalf("ReplaceSpanText: %s", res.Error)
	}

	got, err := ExtractText(out, 1)
	if err != nil {
		t.Fatalf("ExtractText(out): %v — the content stream was likely corrupted", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d spans, want 1 — unbalanced parens split the stream: %+v", len(got), got)
	}
	if !strings.HasPrefix(got[0].Text, "Smith (Jr.)") {
		t.Errorf("got %q, want it to start with %q", got[0].Text, "Smith (Jr.)")
	}
}

func TestReplaceSpanText_RejectsOutOfRangeCharacters(t *testing.T) {
	path := pdftest.TextPage(t, "in.pdf", "BT\n/F1 12 Tf\n72 700 Td\n(Hello) Tj\nET\n")
	res := New().ReplaceSpanText(path, path+".out.pdf", 1, 0, 23, 30, "emoji \U0001F600")
	if res.Error == "" {
		t.Error("expected an error for a character outside the encoding range")
	}
}

func TestReplaceSpanText_RejectsBadOffsets(t *testing.T) {
	path := pdftest.TextPage(t, "in.pdf", "BT\n/F1 12 Tf\n72 700 Td\n(Hello) Tj\nET\n")
	svc := New()

	for _, tc := range []struct {
		name           string
		opStart, opEnd int
	}{
		{"negative start", -1, 10},
		{"end past stream", 0, 1 << 20},
		{"inverted", 20, 10},
		{"empty range", 10, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := svc.ReplaceSpanText(path, path+".out.pdf", 1, 0, tc.opStart, tc.opEnd, "x")
			if res.Error == "" {
				t.Errorf("offsets [%d,%d] were accepted", tc.opStart, tc.opEnd)
			}
		})
	}
}

func TestReplaceSpanText_RejectsBadPage(t *testing.T) {
	path := pdftest.TextPage(t, "in.pdf", "BT\n/F1 12 Tf\n72 700 Td\n(Hello) Tj\nET\n")
	for _, page := range []int{0, -1, 2, 99} {
		if res := New().ReplaceSpanText(path, path+".out.pdf", page, 0, 23, 30, "x"); res.Error == "" {
			t.Errorf("page %d was accepted", page)
		}
	}
}
