package edit

// Merged span editor — block-level BT/ET rewriting.
//
// When the user edits merged text, this module rewrites entire BT/ET blocks
// in the content stream with recalculated glyph positioning. This properly
// handles:
//   - Deletions: characters removed, subsequent chars close up naturally
//   - Insertions: new characters get proper width-based spacing
//   - Replacements: character swapped with correct glyph ID and width
//
// The approach: instead of editing individual hex strings in place (which
// can't handle length changes due to fixed Td positioning), we reconstruct
// the full BT/ET block from scratch using the font's glyph widths.

import (
	"bytes"
	"fmt"
	"os"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// SubSpanInfo describes one raw span that is part of a merged display span.
type SubSpanInfo struct {
	StreamIndex int     `json:"streamIndex"`
	OpStart     int     `json:"opStart"`
	OpEnd       int     `json:"opEnd"`
	Text        string  `json:"text"`
	FontName    string  `json:"fontName"`
	BlockStart  int     `json:"blockStart"`
	BlockEnd    int     `json:"blockEnd"`
	TfSize      float64 `json:"tfSize"`
	TmA         float64 `json:"tmA"`
	TmB         float64 `json:"tmB"`
	TmC         float64 `json:"tmC"`
	TmD         float64 `json:"tmD"`
	TmE         float64 `json:"tmE"`
	TmF         float64 `json:"tmF"`
}

// EditMergedSpans applies edits from a merged display span by rewriting
// entire BT/ET blocks with recalculated glyph positioning.
func (s *Service) EditMergedSpans(
	inputPath, outputPath string,
	pageNum int,
	subSpans []SubSpanInfo,
	originalMerged string,
	newText string,
) TextEditResult {
	if len(subSpans) == 0 {
		return TextEditResult{Error: "no sub-spans provided"}
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

	// Load font resources for glyph encoding and widths
	fonts := loadPageFonts(ctx, pageNum)

	// Get content streams
	streams, refs, err := pageContentStreamsWithRefs(ctx, pageNum)
	if err != nil {
		return TextEditResult{Error: "content streams: " + err.Error()}
	}

	// Map characters from merged text to sub-spans
	charMap := buildCharMap(subSpans, originalMerged)

	// Use diff to determine which characters map to which sub-spans in the new text
	oldRunes := []rune(originalMerged)
	newRunes := []rune(newText)
	ops := diffRunes(oldRunes, newRunes)

	// Build per-sub-span new character lists
	spanChars := make([][]rune, len(subSpans))
	for i, sp := range subSpans {
		spanChars[i] = make([]rune, 0, len([]rune(sp.Text)))
	}

	oi := 0 // index into old text / charMap
	lastRealSpan := 0

	for _, op := range ops {
		switch op.kind {
		case opKeep:
			if oi < len(charMap) {
				cm := charMap[oi]
				if cm.subSpanIdx >= 0 {
					si := cm.subSpanIdx
					ci := cm.charInSpan
					origRunes := []rune(subSpans[si].Text)
					if ci < len(origRunes) {
						spanChars[si] = append(spanChars[si], origRunes[ci])
					}
					lastRealSpan = si
				} else {
					// Merge-inserted space — preserve it by appending to last real sub-span
					if lastRealSpan >= 0 && lastRealSpan < len(subSpans) {
						spanChars[lastRealSpan] = append(spanChars[lastRealSpan], ' ')
					}
				}
			}
			oi++

		case opDelete:
			// Skip this character — don't add it to any sub-span
			if oi < len(charMap) && charMap[oi].subSpanIdx >= 0 {
				lastRealSpan = charMap[oi].subSpanIdx
			}
			// Merge-inserted spaces that are deleted: just skip (correct already)
			oi++

		case opReplace:
			if oi < len(charMap) {
				cm := charMap[oi]
				if cm.subSpanIdx >= 0 {
					spanChars[cm.subSpanIdx] = append(spanChars[cm.subSpanIdx], op.newChar)
					lastRealSpan = cm.subSpanIdx
				} else {
					// Replacing a merge-inserted space — append to last real sub-span
					if lastRealSpan >= 0 && lastRealSpan < len(subSpans) {
						spanChars[lastRealSpan] = append(spanChars[lastRealSpan], op.newChar)
					}
				}
			}
			oi++

		case opInsert:
			// Append to the last sub-span that was active
			if lastRealSpan < len(subSpans) {
				spanChars[lastRealSpan] = append(spanChars[lastRealSpan], op.newChar)
			}
		}
	}

	// Group sub-spans by BT/ET block (identified by streamIndex + blockStart)
	type blockKey struct {
		streamIdx  int
		blockStart int
		blockEnd   int
	}
	type blockInfo struct {
		key      blockKey
		fontName string
		tfSize   float64
		tmA, tmB, tmC, tmD, tmE, tmF float64
		chars    []rune // all characters for this block after editing
	}

	blockMap := make(map[blockKey]*blockInfo)
	var blockOrder []blockKey // preserve order

	for i, sp := range subSpans {
		bk := blockKey{sp.StreamIndex, sp.BlockStart, sp.BlockEnd}
		bi, exists := blockMap[bk]
		if !exists {
			bi = &blockInfo{
				key:      bk,
				fontName: sp.FontName,
				tfSize:   sp.TfSize,
				tmA: sp.TmA, tmB: sp.TmB,
				tmC: sp.TmC, tmD: sp.TmD,
				tmE: sp.TmE, tmF: sp.TmF,
			}
			blockMap[bk] = bi
			blockOrder = append(blockOrder, bk)
		}
		bi.chars = append(bi.chars, spanChars[i]...)
	}

	// Build replacement BT/ET blocks and apply to streams.
	// Process in reverse byte order so earlier replacements don't shift later offsets.
	type blockEdit struct {
		streamIdx  int
		blockStart int
		blockEnd   int
		newBlock   []byte
	}
	var edits []blockEdit

	for _, bk := range blockOrder {
		bi := blockMap[bk]
		fi := fonts[bi.fontName]

		newBlock := buildBTBlock(bi.fontName, bi.tfSize,
			bi.tmA, bi.tmB, bi.tmC, bi.tmD, bi.tmE, bi.tmF,
			bi.chars, fi)

		edits = append(edits, blockEdit{
			streamIdx:  bk.streamIdx,
			blockStart: bk.blockStart,
			blockEnd:   bk.blockEnd,
			newBlock:   newBlock,
		})
	}

	// Sort edits by blockStart descending (reverse order for safe splicing)
	for i := 1; i < len(edits); i++ {
		j := i
		for j > 0 && edits[j].blockStart > edits[j-1].blockStart {
			edits[j], edits[j-1] = edits[j-1], edits[j]
			j--
		}
	}

	// Apply edits
	for _, e := range edits {
		if e.streamIdx < 0 || e.streamIdx >= len(streams) {
			continue
		}
		stream := streams[e.streamIdx]
		if e.blockStart < 0 || e.blockEnd > len(stream) || e.blockStart >= e.blockEnd {
			continue
		}

		modified := make([]byte, 0, len(stream)+len(e.newBlock))
		modified = append(modified, stream[:e.blockStart]...)
		modified = append(modified, e.newBlock...)
		modified = append(modified, stream[e.blockEnd:]...)
		streams[e.streamIdx] = modified
	}

	// Write back all modified streams
	for streamIdx := range streams {
		if streamIdx < len(refs) {
			if err := writeStreamBack(ctx, refs[streamIdx], streams[streamIdx]); err != nil {
				return TextEditResult{Error: "write stream: " + err.Error()}
			}
		}
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
		ActualText: newText,
	}
}

// buildBTBlock constructs a complete BT...ET content stream block.
//
// Output format:
//   BT
//   /FontKey TfSize Tf
//   tmA tmB tmC tmD tmE tmF Tm
//   <glyph0> Tj
//   width0 0 Td <glyph1> Tj
//   width1 0 Td <glyph2> Tj
//   ...
//   ET
func buildBTBlock(fontName string, tfSize float64,
	tmA, tmB, tmC, tmD, tmE, tmF float64,
	chars []rune, fi *fontInfo,
) []byte {
	var buf bytes.Buffer

	buf.WriteString("BT\n")
	buf.WriteString(fmt.Sprintf("/%s %s Tf\n", fontName, formatFloat(tfSize)))
	buf.WriteString(fmt.Sprintf("%s %s %s %s %s %s Tm\n",
		formatFloat(tmA), formatFloat(tmB),
		formatFloat(tmC), formatFloat(tmD),
		formatFloat(tmE), formatFloat(tmF)))

	for i, r := range chars {
		hexGlyph := encodeRuneToHex(r, fi)

		if i == 0 {
			// First character: just Tj at the Tm position
			buf.WriteString(fmt.Sprintf("<%s> Tj\n", hexGlyph))
		} else {
			// Subsequent characters: advance by previous character's width
			prevWidth := glyphWidthForRune(chars[i-1], fi, tfSize)
			buf.WriteString(fmt.Sprintf("%s 0 Td <%s> Tj\n",
				formatFloat(prevWidth), hexGlyph))
		}
	}

	buf.WriteString("ET\n")
	return buf.Bytes()
}

// encodeRuneToHex converts a Unicode rune to its hex glyph representation.
func encodeRuneToHex(r rune, fi *fontInfo) string {
	if fi != nil && fi.isCID && fi.fromUnicode != nil {
		if gid, ok := fi.fromUnicode[r]; ok {
			return fmt.Sprintf("%04x", gid)
		}
		// Space fallback
		if gid, ok := fi.fromUnicode[' ']; ok {
			return fmt.Sprintf("%04x", gid)
		}
		return "0000"
	}

	// Non-CID: single byte
	if fi != nil && fi.encoding != nil {
		for code := 0; code < 256; code++ {
			if fi.encoding[code] == r {
				return fmt.Sprintf("%02x", code)
			}
		}
	}

	// ASCII fallback
	if r < 256 {
		return fmt.Sprintf("%02x", byte(r))
	}
	return "20" // space
}

// glyphWidthForRune returns the advance width in text-space units for a rune.
func glyphWidthForRune(r rune, fi *fontInfo, tfSize float64) float64 {
	if fi == nil {
		return tfSize * 0.5 // rough fallback
	}

	var glyphWidth int
	if fi.isCID && fi.fromUnicode != nil {
		if gid, ok := fi.fromUnicode[r]; ok {
			glyphWidth = fi.glyphWidth(gid)
		} else {
			glyphWidth = fi.defaultWidth
		}
	} else if fi.widths != nil {
		// Non-CID: look up by byte value
		if r < 256 {
			if w, ok := fi.widths[uint16(r)]; ok {
				glyphWidth = w
			} else {
				glyphWidth = fi.defaultWidth
			}
		} else {
			glyphWidth = fi.defaultWidth
		}
	} else {
		glyphWidth = fi.defaultWidth
	}

	// Width is in 1/1000 units; multiply by font size to get text-space advance
	return float64(glyphWidth) * tfSize / 1000.0
}

// formatFloat formats a float for PDF content stream output.
// Uses minimal precision to keep output compact.
func formatFloat(v float64) string {
	s := fmt.Sprintf("%.6f", v)
	// Trim trailing zeros
	for len(s) > 1 && s[len(s)-1] == '0' && s[len(s)-2] != '.' {
		s = s[:len(s)-1]
	}
	return s
}

// ── Character mapping ─────────────────────────────────────────────────────────

type charMapping struct {
	subSpanIdx int
	charInSpan int
}

func buildCharMap(subSpans []SubSpanInfo, mergedText string) []charMapping {
	var result []charMapping
	mergedRunes := []rune(mergedText)
	mi := 0

	for si, sp := range subSpans {
		spRunes := []rune(sp.Text)

		// Detect inserted space from merge logic
		if mi < len(mergedRunes) && si > 0 {
			if mergedRunes[mi] == ' ' && (len(spRunes) == 0 || spRunes[0] != ' ') {
				prevText := subSpans[si-1].Text
				prevRunes := []rune(prevText)
				if len(prevRunes) == 0 || prevRunes[len(prevRunes)-1] != ' ' {
					result = append(result, charMapping{subSpanIdx: -1, charInSpan: 0})
					mi++
				}
			}
		}

		for ci := range spRunes {
			if mi < len(mergedRunes) {
				result = append(result, charMapping{subSpanIdx: si, charInSpan: ci})
				mi++
			}
		}
	}

	for mi < len(mergedRunes) {
		result = append(result, charMapping{subSpanIdx: -1, charInSpan: 0})
		mi++
	}

	return result
}

// ── Diff algorithm ────────────────────────────────────────────────────────────

type editOp struct {
	kind    editOpKind
	newChar rune
}

type editOpKind int

const (
	opKeep    editOpKind = iota
	opDelete
	opInsert
	opReplace
)

func diffRunes(old, new []rune) []editOp {
	m, n := len(old), len(new)

	// Common prefix
	prefix := 0
	for prefix < m && prefix < n && old[prefix] == new[prefix] {
		prefix++
	}

	// Common suffix
	suffix := 0
	for suffix < m-prefix && suffix < n-prefix &&
		old[m-1-suffix] == new[n-1-suffix] {
		suffix++
	}

	oldMid := old[prefix : m-suffix]
	newMid := new[prefix : n-suffix]

	ops := make([]editOp, 0, m+len(newMid))

	for i := 0; i < prefix; i++ {
		ops = append(ops, editOp{kind: opKeep})
	}

	if len(oldMid) == 0 {
		for _, r := range newMid {
			ops = append(ops, editOp{kind: opInsert, newChar: r})
		}
	} else if len(newMid) == 0 {
		for range oldMid {
			ops = append(ops, editOp{kind: opDelete})
		}
	} else {
		midOps := dpDiff(oldMid, newMid)
		ops = append(ops, midOps...)
	}

	for i := 0; i < suffix; i++ {
		ops = append(ops, editOp{kind: opKeep})
	}

	return ops
}

func dpDiff(old, new []rune) []editOp {
	m, n := len(old), len(new)

	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 0; i <= m; i++ {
		dp[i][0] = i
	}
	for j := 0; j <= n; j++ {
		dp[0][j] = j
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if old[i-1] == new[j-1] {
				dp[i][j] = dp[i-1][j-1]
			} else {
				del := dp[i-1][j] + 1
				ins := dp[i][j-1] + 1
				rep := dp[i-1][j-1] + 1
				dp[i][j] = min3(del, ins, rep)
			}
		}
	}

	var ops []editOp
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && old[i-1] == new[j-1] {
			ops = append(ops, editOp{kind: opKeep})
			i--
			j--
		} else if i > 0 && j > 0 && dp[i][j] == dp[i-1][j-1]+1 {
			ops = append(ops, editOp{kind: opReplace, newChar: new[j-1]})
			i--
			j--
		} else if j > 0 && dp[i][j] == dp[i][j-1]+1 {
			ops = append(ops, editOp{kind: opInsert, newChar: new[j-1]})
			j--
		} else {
			ops = append(ops, editOp{kind: opDelete})
			i--
		}
	}

	for l, r := 0, len(ops)-1; l < r; l, r = l+1, r-1 {
		ops[l], ops[r] = ops[r], ops[l]
	}

	return ops
}

func min3(a, b, c int) int {
	if a < b {
		if a < c { return a }
		return c
	}
	if b < c { return b }
	return c
}
