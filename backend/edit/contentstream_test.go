package edit

import (
	"strings"
	"testing"
)

func TestTokenise_LiteralString(t *testing.T) {
	src := []byte("BT\n/F1 12 Tf\n100 700 Td\n(Hello World) Tj\nET")
	tokens := tokenise(src)

	// Find the string token
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

func TestDecodeString_HexUTF16BE(t *testing.T) {
	// CID/Identity-H fonts encode text as 2-byte UTF-16BE glyph indices.
	// <00480065006C006C006F> = "Hello" in UTF-16BE
	decoded := decodeString("00480065006C006C006F", tokHexString)
	if decoded != "Hello" {
		t.Errorf("UTF-16BE hex: got %q want %q", decoded, "Hello")
	}
}

func TestDecodeString_HexSingleByteFallback(t *testing.T) {
	// Odd-length or non-multiple-of-4 hex should fall back to single-byte
	decoded := decodeString("48656C6C6F", tokHexString)
	if decoded != "Hello" {
		t.Errorf("single-byte hex: got %q want %q", decoded, "Hello")
	}
}

func TestParseContentStream_CIDHexStrings(t *testing.T) {
	// Simulate a CID font page with UTF-16BE hex strings in TJ array
	stream := []byte("BT\n/F1 12 Tf\n100 700 Td\n<00480069> Tj\nET")
	spans := parseContentStream(stream, 0, 1)
	if len(spans) != 1 {
		t.Fatalf("got %d spans want 1", len(spans))
	}
	if spans[0].Text != "Hi" {
		t.Errorf("CID hex text: got %q want %q", spans[0].Text, "Hi")
	}
}

func TestParseContentStream_BasicPositioning(t *testing.T) {
	stream := []byte(`BT
/F1 12 Tf
100 700 Td
(First line) Tj
0 -15 Td
(Second line) Tj
ET`)
	spans := parseContentStream(stream, 0, 1)
	if len(spans) != 2 {
		t.Fatalf("got %d spans want 2", len(spans))
	}
	if spans[0].Text != "First line"  { t.Errorf("span 0 text: %q", spans[0].Text) }
	if spans[0].X != 100              { t.Errorf("span 0 X: %v", spans[0].X) }
	if spans[0].Y != 700              { t.Errorf("span 0 Y: %v", spans[0].Y) }
	if spans[1].Text != "Second line" { t.Errorf("span 1 text: %q", spans[1].Text) }
	if spans[1].X != 100              { t.Errorf("span 1 X: %v", spans[1].X) }
	if spans[1].Y != 685              { t.Errorf("span 1 Y: %v want 685", spans[1].Y) }
}

func TestParseContentStream_TJArray(t *testing.T) {
	stream := []byte(`BT
/F1 10 Tf
50 500 Td
[(Hel) 2 (lo)] TJ
ET`)
	spans := parseContentStream(stream, 0, 1)
	if len(spans) != 1 {
		t.Fatalf("got %d spans want 1", len(spans))
	}
	if spans[0].Text != "Hello" {
		t.Errorf("TJ text: got %q want %q", spans[0].Text, "Hello")
	}
}

func TestParseContentStream_TextMatrix(t *testing.T) {
	// Tm sets position absolutely: a b c d x y Tm
	stream := []byte(`BT
1 0 0 1 200 400 Tm
/F1 14 Tf
(Absolute) Tj
ET`)
	spans := parseContentStream(stream, 0, 1)
	if len(spans) != 1 {
		t.Fatalf("got %d spans", len(spans))
	}
	if spans[0].X != 200 || spans[0].Y != 400 {
		t.Errorf("Tm position: got (%v,%v) want (200,400)", spans[0].X, spans[0].Y)
	}
}

func TestParseContentStream_FontName(t *testing.T) {
	stream := []byte("BT\n/Helvetica 12 Tf\n0 0 Td\n(test) Tj\nET")
	spans := parseContentStream(stream, 0, 1)
	if len(spans) == 0 {
		t.Fatal("no spans")
	}
	if !strings.Contains(spans[0].FontName, "Helvetica") {
		t.Errorf("font name: %q", spans[0].FontName)
	}
}
