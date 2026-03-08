package edit

// PDF content stream decoder.
//
// Uses pdfcpu.ExtractPageContent to get the decoded content stream bytes,
// then parses text operators to extract positioned TextSpans.
//
// Spec reference: PDF 32000-1:2008 §9 — Text

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpupkg "github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// TextSpan is a single positioned run of text extracted from the stream.
type TextSpan struct {
	Text     string  `json:"text"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Rotation float64 `json:"rotation"` // degrees, CCW, 0 = normal horizontal
	FontName string  `json:"fontName"`
	FontSize float64 `json:"fontSize"`
	PageNum  int     `json:"pageNum"`

	// Stream location — needed for in-place editing
	StreamIndex int `json:"streamIndex"`
	OpStart     int `json:"opStart"` // byte offset of opening ( or <
	OpEnd       int `json:"opEnd"`   // byte offset just past closing ) or >
}

// ExtractText returns all text spans from the given page of a PDF.
func ExtractText(filePath string, pageNum int) ([]TextSpan, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	conf := model.NewDefaultConfiguration()
	ctx, err := api.ReadValidateAndOptimize(f, conf)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "password") || strings.Contains(errStr, "encrypt") ||
			strings.Contains(errStr, "hex literal") || strings.Contains(errStr, "corrupt") {
			return nil, fmt.Errorf("encrypted PDF: text editing requires decrypting the file first (Security panel → Remove Protection)")
		}
		return nil, fmt.Errorf("read PDF: %w", err)
	}

	if pageNum < 1 || pageNum > ctx.PageCount {
		return nil, fmt.Errorf("page %d out of range (1-%d)", pageNum, ctx.PageCount)
	}

	// ExtractPageContent returns a reader over the decoded, concatenated
	// content stream for the page — no internal type navigation needed.
	r, err := pdfcpupkg.ExtractPageContent(ctx, pageNum)
	if err != nil {
		return nil, fmt.Errorf("extract content: %w", err)
	}
	if r == nil {
		return nil, nil
	}

	stream, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read stream: %w", err)
	}

	return parseContentStream(stream, 0, pageNum), nil
}

// ── Text state ────────────────────────────────────────────────────────────────

type textState struct {
	tx, ty   float64 // current text position
	lx, ly   float64 // text line matrix origin
	tfSize   float64 // size from Tf operator
	tmScale  float64 // scale magnitude from Tm matrix
	tmAngle  float64 // rotation angle in degrees (CCW) from Tm matrix
	fontKey  string
	leading  float64
}

// effectiveSize returns the actual rendered font size.
// In PDF: rendered size = Tf size × Tm scale.
// Google Docs and many exporters set Tf size=1 and put the real size in Tm.
func (ts *textState) effectiveSize() float64 {
	s := ts.tfSize * ts.tmScale
	if s <= 0 { s = ts.tfSize }
	if s <= 0 { s = 12 }
	return s
}

// ── Content stream parser ─────────────────────────────────────────────────────

func parseContentStream(stream []byte, streamIdx, pageNum int) []TextSpan {
	tokens := tokenise(stream)
	var spans []TextSpan
	var ts textState
	inBT := false

	for i, tok := range tokens {
		if tok.kind != tokOperator {
			continue
		}
		op       := tok.value
		operands := collectOperands(tokens, i)

		switch op {
		case "BT":
			inBT = true
			ts = textState{tfSize: 12, tmScale: 1}

		case "ET":
			inBT = false

		case "Tf":
			if len(operands) >= 2 {
				ts.fontKey = strings.TrimPrefix(operands[len(operands)-2].value, "/")
				ts.tfSize, _ = strconv.ParseFloat(operands[len(operands)-1].value, 64)
				if ts.tmScale == 0 {
					ts.tmScale = 1
				}
			}

		case "Tm":
			if len(operands) >= 6 {
				ts.tx, _ = strconv.ParseFloat(operands[len(operands)-2].value, 64)
				ts.ty, _ = strconv.ParseFloat(operands[len(operands)-1].value, 64)
				ts.lx, ts.ly = ts.tx, ts.ty
				// The text matrix [a b c d e f] encodes scale and rotation.
				// Scale = sqrt(a²+b²), angle = atan2(b, a) in degrees.
				a, _ := strconv.ParseFloat(operands[len(operands)-6].value, 64)
				b, _ := strconv.ParseFloat(operands[len(operands)-5].value, 64)
				scale := math.Sqrt(a*a + b*b)
				if scale > 0 {
					ts.tmScale = scale
					ts.tmAngle = math.Atan2(b, a) * 180 / math.Pi
				}
			}

		case "Td":
			if len(operands) >= 2 {
				dx, _ := strconv.ParseFloat(operands[len(operands)-2].value, 64)
				dy, _ := strconv.ParseFloat(operands[len(operands)-1].value, 64)
				ts.lx += dx
				ts.ly += dy
				ts.tx, ts.ty = ts.lx, ts.ly
			}

		case "TD":
			if len(operands) >= 2 {
				dx, _ := strconv.ParseFloat(operands[len(operands)-2].value, 64)
				dy, _ := strconv.ParseFloat(operands[len(operands)-1].value, 64)
				ts.leading = -dy
				ts.lx += dx
				ts.ly += dy
				ts.tx, ts.ty = ts.lx, ts.ly
			}

		case "TL":
			if len(operands) >= 1 {
				ts.leading, _ = strconv.ParseFloat(operands[0].value, 64)
			}

		case "T*":
			ts.ly -= ts.leading
			ts.tx, ts.ty = ts.lx, ts.ly

		case "Tj":
			if inBT && len(operands) >= 1 {
				last := operands[len(operands)-1]
				spans = append(spans, TextSpan{
					Text: decodeString(last.value, last.kind),
					X: ts.tx, Y: ts.ty,
					FontName: ts.fontKey, FontSize: ts.effectiveSize(), Rotation: ts.tmAngle,
					PageNum: pageNum, StreamIndex: streamIdx,
					OpStart: last.start, OpEnd: last.end,
				})
			}

		case "TJ":
			if inBT {
				var sb strings.Builder
				firstStart, lastEnd := -1, -1
				for _, op := range operands {
					if op.kind == tokString || op.kind == tokHexString {
						sb.WriteString(decodeString(op.value, op.kind))
						if firstStart == -1 {
							firstStart = op.start
						}
						lastEnd = op.end
					}
				}
				if sb.Len() > 0 && firstStart >= 0 {
					spans = append(spans, TextSpan{
						Text: sb.String(),
						X: ts.tx, Y: ts.ty,
						FontName: ts.fontKey, FontSize: ts.effectiveSize(), Rotation: ts.tmAngle,
						PageNum: pageNum, StreamIndex: streamIdx,
						OpStart: firstStart, OpEnd: lastEnd,
					})
				}
			}

		case "'":
			ts.ly -= ts.leading
			ts.tx, ts.ty = ts.lx, ts.ly
			if inBT && len(operands) >= 1 {
				last := operands[len(operands)-1]
				spans = append(spans, TextSpan{
					Text: decodeString(last.value, last.kind),
					X: ts.tx, Y: ts.ty,
					FontName: ts.fontKey, FontSize: ts.effectiveSize(), Rotation: ts.tmAngle,
					PageNum: pageNum, StreamIndex: streamIdx,
					OpStart: last.start, OpEnd: last.end,
				})
			}

		case `"`:
			if inBT && len(operands) >= 3 {
				ts.ly -= ts.leading
				ts.tx, ts.ty = ts.lx, ts.ly
				last := operands[len(operands)-1]
				spans = append(spans, TextSpan{
					Text: decodeString(last.value, last.kind),
					X: ts.tx, Y: ts.ty,
					FontName: ts.fontKey, FontSize: ts.effectiveSize(), Rotation: ts.tmAngle,
					PageNum: pageNum, StreamIndex: streamIdx,
					OpStart: last.start, OpEnd: last.end,
				})
			}
		}
	}
	return spans
}

// ── Tokeniser ─────────────────────────────────────────────────────────────────

type tokenKind int

const (
	tokOperator tokenKind = iota
	tokNumber
	tokName
	tokString
	tokHexString
	tokArrayOpen
	tokArrayClose
)

type token struct {
	kind  tokenKind
	value string
	start int
	end   int
}

func tokenise(src []byte) []token {
	var tokens []token
	i, n := 0, len(src)

	for i < n {
		if isWS(src[i]) { i++; continue }

		if src[i] == '%' {
			for i < n && src[i] != '\n' && src[i] != '\r' { i++ }
			continue
		}

		if src[i] == '(' {
			start := i; i++; depth := 1
			var buf bytes.Buffer
			for i < n && depth > 0 {
				c := src[i]
				if c == '\\' && i+1 < n {
					i++
					switch src[i] {
					case 'n': buf.WriteByte('\n')
					case 'r': buf.WriteByte('\r')
					case 't': buf.WriteByte('\t')
					case 'b': buf.WriteByte('\b')
					case 'f': buf.WriteByte('\f')
					case '(': buf.WriteByte('(')
					case ')': buf.WriteByte(')')
					case '\\': buf.WriteByte('\\')
					default:
						if src[i] >= '0' && src[i] <= '7' {
							octal := string(src[i])
							for j := 1; j < 3 && i+1 < n && src[i+1] >= '0' && src[i+1] <= '7'; j++ {
								i++; octal += string(src[i])
							}
							v, _ := strconv.ParseUint(octal, 8, 8)
							buf.WriteByte(byte(v))
						} else {
							buf.WriteByte(src[i])
						}
					}
				} else if c == '(' {
					depth++; buf.WriteByte(c)
				} else if c == ')' {
					depth--
					if depth > 0 { buf.WriteByte(c) }
				} else {
					buf.WriteByte(c)
				}
				i++
			}
			tokens = append(tokens, token{kind: tokString, value: buf.String(), start: start, end: i})
			continue
		}

		if src[i] == '<' && i+1 < n && src[i+1] != '<' {
			start := i; i++
			var buf bytes.Buffer
			for i < n && src[i] != '>' { buf.WriteByte(src[i]); i++ }
			if i < n { i++ }
			tokens = append(tokens, token{kind: tokHexString, value: buf.String(), start: start, end: i})
			continue
		}

		if src[i] == '[' { tokens = append(tokens, token{kind: tokArrayOpen,  value: "[", start: i, end: i+1}); i++; continue }
		if src[i] == ']' { tokens = append(tokens, token{kind: tokArrayClose, value: "]", start: i, end: i+1}); i++; continue }

		if src[i] == '<' && i+1 < n && src[i+1] == '<' {
			i += 2
			for i+1 < n && !(src[i] == '>' && src[i+1] == '>') { i++ }
			i += 2
			continue
		}

		if src[i] == '/' {
			start := i; i++
			var buf bytes.Buffer
			for i < n && !isWS(src[i]) && !isDelim(src[i]) { buf.WriteByte(src[i]); i++ }
			tokens = append(tokens, token{kind: tokName, value: "/" + buf.String(), start: start, end: i})
			continue
		}

		start := i
		var buf bytes.Buffer
		for i < n && !isWS(src[i]) && !isDelim(src[i]) { buf.WriteByte(src[i]); i++ }
		word := buf.String()
		if word == "" { i++; continue }
		kind := tokOperator
		if isNumber(word) { kind = tokNumber }
		tokens = append(tokens, token{kind: kind, value: word, start: start, end: i})
	}
	return tokens
}

func collectOperands(tokens []token, opIdx int) []token {
	var ops []token
	for i := opIdx - 1; i >= 0; i-- {
		t := tokens[i]
		if t.kind == tokOperator { break }
		if t.kind != tokArrayOpen && t.kind != tokArrayClose {
			ops = append([]token{t}, ops...)
		}
	}
	return ops
}

func decodeString(raw string, kind tokenKind) string {
	if kind == tokHexString {
		if len(raw)%2 != 0 { raw += "0" }

		// Many PDFs use CID fonts (Identity-H) where hex strings contain
		// 2-byte UTF-16BE glyph indices, e.g. <00480065006C006C006F> = "Hello".
		// Detect this by checking if the hex length is a multiple of 4 (2-byte pairs)
		// and if decoding as UTF-16BE yields valid text (no replacement chars, no nulls).
		if len(raw) >= 4 && len(raw)%4 == 0 {
			decoded := decodeHexAsUTF16BE(raw)
			if decoded != "" {
				return decoded
			}
		}

		// Fallback: single-byte decoding
		var buf bytes.Buffer
		for i := 0; i+1 < len(raw); i += 2 {
			b, err := strconv.ParseUint(raw[i:i+2], 16, 8)
			if err == nil { buf.WriteByte(byte(b)) }
		}
		return buf.String()
	}
	var buf bytes.Buffer
	for _, c := range []byte(raw) {
		if c < 128 { buf.WriteByte(c) } else { buf.WriteRune(rune(c)) }
	}
	return buf.String()
}

// decodeHexAsUTF16BE attempts to interpret hex data as 2-byte UTF-16BE
// code points. Returns the decoded string if all code points are valid
// printable characters, or "" if this doesn't look like UTF-16BE text.
func decodeHexAsUTF16BE(hex string) string {
	var runes []rune
	for i := 0; i+3 < len(hex); i += 4 {
		hi, err1 := strconv.ParseUint(hex[i:i+2], 16, 8)
		lo, err2 := strconv.ParseUint(hex[i+2:i+4], 16, 8)
		if err1 != nil || err2 != nil {
			return ""
		}
		cp := rune(hi)<<8 | rune(lo)
		// Reject null characters and low control chars (except tab/newline/CR)
		if cp == 0 {
			return ""
		}
		if cp < 0x20 && cp != '\t' && cp != '\n' && cp != '\r' {
			return ""
		}
		runes = append(runes, cp)
	}
	if len(runes) == 0 {
		return ""
	}
	return string(runes)
}

func isWS(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == 0
}
func isDelim(c byte) bool {
	return c == '(' || c == ')' || c == '<' || c == '>' ||
		c == '[' || c == ']' || c == '{' || c == '}' || c == '/' || c == '%'
}
func isNumber(s string) bool {
	if s == "" { return false }
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}
