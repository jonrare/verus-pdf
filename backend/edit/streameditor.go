package edit

// PDF content stream text editor.
//
// Reads a page's content stream(s), splices in the replacement string at the
// byte offsets reported by the decoder, then writes the modified PDF back.

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
	if streamIndex < 0 || streamIndex >= len(streams) {
		return TextEditResult{Error: fmt.Sprintf("stream index %d out of range", streamIndex)}
	}

	stream := streams[streamIndex]
	if opStart < 0 || opEnd > len(stream) || opStart >= opEnd {
		return TextEditResult{Error: fmt.Sprintf(
			"invalid span offsets [%d,%d] in stream of len %d", opStart, opEnd, len(stream))}
	}

	original := stream[opStart:opEnd]
	isHex   := len(original) > 0 && original[0] == '<'

	var replacement []byte
	var actualText  string
	var truncated, padded bool

	if isHex {
		origByteLen := (opEnd - opStart - 2) / 2
		replacement, actualText, truncated, padded = buildHexReplacement(newText, origByteLen)
	} else {
		origInner    := unescapePDFLiteral(string(original[1 : len(original)-1]))
		replacement, actualText, truncated, padded = buildLiteralReplacement(
			escapePDFString(newText), newText, len(origInner))
	}

	modified := make([]byte, 0, len(stream))
	modified  = append(modified, stream[:opStart]...)
	modified  = append(modified, replacement...)
	modified  = append(modified, stream[opEnd:]...)

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
		var allRefs  []types.IndirectRef
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
			allRefs  = append(allRefs, ir)
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

func buildLiteralReplacement(escaped, original string, origByteLen int) ([]byte, string, bool, bool) {
	raw := []byte(escaped)
	truncated, padded := false, false
	actual := original
	if len(raw) < origByteLen {
		raw = append(raw, bytes.Repeat([]byte{' '}, origByteLen-len(raw))...)
		padded = true
	} else if len(raw) > origByteLen {
		raw = raw[:origByteLen]
		actual = string(raw)
		truncated = true
	}
	result := make([]byte, 0, len(raw)+2)
	result = append(result, '(')
	result = append(result, raw...)
	result = append(result, ')')
	return result, actual, truncated, padded
}

func buildHexReplacement(newText string, origByteLen int) ([]byte, string, bool, bool) {
	raw := []byte(newText)
	truncated, padded := false, false
	actual := newText
	if len(raw) < origByteLen {
		raw = append(raw, bytes.Repeat([]byte{' '}, origByteLen-len(raw))...)
		padded = true
	} else if len(raw) > origByteLen {
		raw = raw[:origByteLen]
		actual = string(raw)
		truncated = true
	}
	var buf bytes.Buffer
	buf.WriteByte('<')
	for _, b := range raw { fmt.Fprintf(&buf, "%02x", b) }
	buf.WriteByte('>')
	return buf.Bytes(), actual, truncated, padded
}

func escapePDFString(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch c {
		case '(':  b.WriteString(`\(`)
		case ')':  b.WriteString(`\)`)
		case '\\': b.WriteString(`\\`)
		default:   b.WriteByte(c)
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
			case 'n':  buf.WriteByte('\n')
			case 'r':  buf.WriteByte('\r')
			case 't':  buf.WriteByte('\t')
			case '(':  buf.WriteByte('(')
			case ')':  buf.WriteByte(')')
			case '\\': buf.WriteByte('\\')
			default:   buf.WriteByte(b[i])
			}
		} else {
			buf.WriteByte(b[i])
		}
	}
	return buf.String()
}
