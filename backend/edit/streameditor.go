package edit

// PDF content stream text editor.
//
// Reads a page's content stream(s), splices in the replacement string at the
// byte offsets reported by the decoder, then writes the modified PDF back.
//
// Spec: ISO 32000-1:2008, §7.3.4.2 (literal strings), §7.3.4.3 (hex strings).
// See docs/pdf-spec.md — in particular the known deviations around multi-stream
// pages and non-ASCII replacement text.

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// TextEditResult describes what actually happened during an edit.
type TextEditResult struct {
	OutputPath string `json:"outputPath"`
	ActualText string `json:"actualText"`
	Truncated  bool   `json:"truncated"`
	Padded     bool   `json:"padded"`
	Error      string `json:"error,omitempty"`
}

// ReplaceSpanText replaces the text of a single span in the PDF content stream.
func (s *Service) ReplaceSpanText(
	inputPath, outputPath string,
	pageNum, streamIndex, opStart, opEnd int,
	newText string,
) TextEditResult {
	// Validate characters are in standard PDF encoding range
	for _, r := range newText {
		if r < 0x20 || r > 0xFF {
			return TextEditResult{Error: fmt.Sprintf(
				"character %q (U+%04X) outside standard PDF encoding range", r, r)}
		}
	}

	f, err := os.Open(inputPath)
	if err != nil {
		return TextEditResult{Error: "open: " + err.Error()}
	}
	conf := model.NewDefaultConfiguration()
	ctx, err := api.ReadValidateAndOptimize(f, conf)
	f.Close()
	if err != nil {
		return TextEditResult{Error: "read PDF: " + err.Error()}
	}

	if pageNum < 1 || pageNum > ctx.PageCount {
		return TextEditResult{Error: fmt.Sprintf("page %d out of range", pageNum)}
	}

	// Get all decoded content streams for this page
	streams, refs, err := pageContentStreamsWithRefs(ctx, pageNum)
	if err != nil {
		return TextEditResult{Error: "content streams: " + err.Error()}
	}
	if streamIndex == notEditable {
		return TextEditResult{Error: "this text cannot be edited in place: it lives in a Form XObject " +
			"or spans two content streams, so its offsets do not address the page content stream"}
	}
	if streamIndex < 0 || streamIndex >= len(streams) {
		return TextEditResult{Error: fmt.Sprintf("stream index %d out of range", streamIndex)}
	}

	stream := streams[streamIndex]
	if opStart < 0 || opEnd > len(stream) || opStart >= opEnd {
		return TextEditResult{Error: fmt.Sprintf(
			"invalid span offsets [%d,%d] in stream of len %d", opStart, opEnd, len(stream))}
	}

	original := stream[opStart:opEnd]
	isHex := len(original) > 0 && original[0] == '<'

	var replacement []byte
	var actualText string
	var truncated, padded bool

	if isHex {
		// Two hex digits per byte, minus the enclosing < >.
		origLen := (opEnd - opStart - 2) / 2
		replacement, actualText, truncated, padded = buildHexReplacement(newText, origLen)
	} else {
		origInner := unescapePDFLiteral(string(original[1 : len(original)-1]))
		replacement, actualText, truncated, padded = buildLiteralReplacement(
			newText, len([]rune(origInner)))
	}

	modified := make([]byte, 0, len(stream))
	modified = append(modified, stream[:opStart]...)
	modified = append(modified, replacement...)
	modified = append(modified, stream[opEnd:]...)

	// Write back into the xref table via the indirect reference
	if err := writeStreamBack(ctx, refs[streamIndex], modified); err != nil {
		return TextEditResult{Error: "write stream: " + err.Error()}
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return TextEditResult{Error: "create output: " + err.Error()}
	}
	defer out.Close()

	if err := api.WriteContext(ctx, out); err != nil {
		return TextEditResult{Error: "write PDF: " + err.Error()}
	}

	return TextEditResult{
		OutputPath: outputPath,
		ActualText: actualText,
		Truncated:  truncated,
		Padded:     padded,
	}
}

// pageContentStreamsWithRefs returns decoded stream bytes and their indirect
// references so we can write them back after editing.
func pageContentStreamsWithRefs(ctx *model.Context, pageNum int) ([][]byte, []types.IndirectRef, error) {
	pageDict, _, _, err := ctx.PageDict(pageNum, false)
	if err != nil {
		return nil, nil, err
	}

	obj, found := pageDict.Find("Contents")
	if !found {
		return nil, nil, nil
	}

	// Dereference to get the actual object
	obj, err = ctx.XRefTable.Dereference(obj)
	if err != nil {
		return nil, nil, err
	}

	switch v := obj.(type) {
	case types.StreamDict:
		// Single stream — find its indirect ref from the Contents entry
		contentsVal, _ := pageDict.Find("Contents")
		ir, ok := contentsVal.(types.IndirectRef)
		if !ok {
			return nil, nil, fmt.Errorf("Contents is not an indirect ref")
		}
		if err := v.Decode(); err != nil {
			return nil, nil, fmt.Errorf("decode stream: %w", err)
		}
		return [][]byte{v.Content}, []types.IndirectRef{ir}, nil

	case types.Array:
		var allBytes [][]byte
		var allRefs []types.IndirectRef
		for _, elem := range v {
			ir, ok := elem.(types.IndirectRef)
			if !ok {
				continue
			}
			derefed, err := ctx.XRefTable.Dereference(elem)
			if err != nil {
				continue
			}
			sd, ok := derefed.(types.StreamDict)
			if !ok {
				continue
			}
			if err := sd.Decode(); err != nil {
				continue
			}
			allBytes = append(allBytes, sd.Content)
			allRefs = append(allRefs, ir)
		}
		return allBytes, allRefs, nil
	}

	return nil, nil, fmt.Errorf("unexpected Contents type: %T", obj)
}

// writeStreamBack re-encodes modified content and updates the xref entry.
func writeStreamBack(ctx *model.Context, ir types.IndirectRef, content []byte) error {
	objNr := ir.ObjectNumber.Value()
	entry, ok := ctx.XRefTable.Find(objNr)
	if !ok {
		return fmt.Errorf("xref entry %d not found", objNr)
	}

	sd, ok := entry.Object.(types.StreamDict)
	if !ok {
		return fmt.Errorf("xref entry %d is not a StreamDict", objNr)
	}

	sd.Content = content
	if err := sd.Encode(); err != nil {
		return fmt.Errorf("encode stream: %w", err)
	}

	entry.Object = sd
	return nil
}

// ── String helpers ────────────────────────────────────────────────────────────

// fitToLength pads with spaces or truncates runes so the result is exactly
// origLen characters long.
//
// The budget is in characters rather than bytes because the surrounding
// content stream positions later text with absolute Td operators laid out
// against the original glyph count. Cutting runes (not bytes) also means a
// truncation can never split a multi-byte character or, once escaped, an
// escape sequence.
func fitToLength(newText string, origLen int) (string, bool, bool) {
	runes := []rune(newText)
	switch {
	case len(runes) > origLen:
		return string(runes[:origLen]), true, false
	case len(runes) < origLen:
		return newText + strings.Repeat(" ", origLen-len(runes)), false, true
	}
	return newText, false, false
}

func buildLiteralReplacement(newText string, origLen int) ([]byte, string, bool, bool) {
	actual, truncated, padded := fitToLength(newText, origLen)

	var buf bytes.Buffer
	buf.WriteByte('(')
	buf.WriteString(escapePDFString(actual))
	buf.WriteByte(')')
	return buf.Bytes(), actual, truncated, padded
}

func buildHexReplacement(newText string, origLen int) ([]byte, string, bool, bool) {
	actual, truncated, padded := fitToLength(newText, origLen)

	var buf bytes.Buffer
	buf.WriteByte('<')
	for _, r := range actual {
		fmt.Fprintf(&buf, "%02x", encodeByte(r))
	}
	buf.WriteByte('>')
	return buf.Bytes(), actual, truncated, padded
}

// encodeByte narrows a rune to the single byte a simple font's encoding table
// will look up. Callers validate the input range first; anything that slips
// through becomes '?' rather than silently emitting a different glyph.
//
// Writing Go's UTF-8 bytes here instead would turn é into Ã©, because the font
// decodes these bytes through WinAnsi/MacRoman/Standard, not UTF-8.
// ISO 32000-1:2008, §9.6.6.
func encodeByte(r rune) byte {
	if r < 0 || r > 0xFF {
		return '?'
	}
	return byte(r)
}

// escapePDFString renders text as the inner bytes of a PDF literal string.
// ISO 32000-1:2008, §7.3.4.2.
func escapePDFString(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '(':
			b.WriteString(`\(`)
		case ')':
			b.WriteString(`\)`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteByte(encodeByte(r))
		}
	}
	return b.String()
}

func unescapePDFLiteral(s string) string {
	var buf bytes.Buffer
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		if b[i] == '\\' && i+1 < len(b) {
			i++
			switch b[i] {
			case 'n':
				buf.WriteByte('\n')
			case 'r':
				buf.WriteByte('\r')
			case 't':
				buf.WriteByte('\t')
			case '(':
				buf.WriteByte('(')
			case ')':
				buf.WriteByte(')')
			case '\\':
				buf.WriteByte('\\')
			default:
				buf.WriteByte(b[i])
			}
		} else {
			buf.WriteByte(b[i])
		}
	}
	return buf.String()
}
