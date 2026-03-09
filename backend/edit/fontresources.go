package edit

// Font resource loader.
//
// Reads /Font resources from a PDF page to extract:
//   - ToUnicode CMaps  → glyph ID → Unicode mapping
//   - /W width arrays  → glyph ID → advance width (in 1/1000 units)
//
// Spec: PDF 32000-1:2008 §9.7 (ToUnicode), §9.7.4.3 (CIDFont widths)

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// fontInfo holds decoded font metadata for one font resource.
type fontInfo struct {
	name         string
	toUnicode    map[uint16]rune   // glyph ID → Unicode code point
	fromUnicode  map[rune]uint16   // Unicode → glyph ID (reverse of toUnicode)
	widths       map[uint16]int    // glyph ID → width in 1/1000 units
	defaultWidth int               // default glyph width (1/1000 units)
	isCID        bool              // true for Type0/CID fonts
	encoding     *encodingTable    // byte → Unicode for non-CID fonts (nil = passthrough)
}

// pagefonts maps font resource names (e.g. "F5") to their fontInfo.
type pageFonts map[string]*fontInfo

// lookupRune maps a raw glyph code through the ToUnicode CMap.
// Returns the Unicode rune and true, or 0 and false if no mapping.
func (fi *fontInfo) lookupRune(glyphID uint16) (rune, bool) {
	if fi == nil || fi.toUnicode == nil {
		return 0, false
	}
	r, ok := fi.toUnicode[glyphID]
	return r, ok
}

// glyphWidth returns the advance width for a glyph in 1/1000 units.
func (fi *fontInfo) glyphWidth(glyphID uint16) int {
	if fi == nil || fi.widths == nil {
		return fi.defaultWidth
	}
	w, ok := fi.widths[glyphID]
	if !ok {
		return fi.defaultWidth
	}
	return w
}

// loadPageFonts extracts font resources from the given page.
func loadPageFonts(ctx *model.Context, pageNum int) pageFonts {
	fonts := make(pageFonts)

	pageDict, _, _, err := ctx.PageDict(pageNum, false)
	if err != nil {
		return fonts
	}

	resDict, err := resourceDict(ctx, pageDict)
	if err != nil || resDict == nil {
		return fonts
	}

	fontObj, found := resDict.Find("Font")
	if !found {
		return fonts
	}

	fontDict, err := ctx.DereferenceDict(fontObj)
	if err != nil || fontDict == nil {
		return fonts
	}

	for key, val := range fontDict {
		fi := parseFontResource(ctx, key, val)
		if fi != nil {
			fonts[key] = fi
		}
	}

	return fonts
}

// resourceDict gets the /Resources dict for a page, handling inheritance.
func resourceDict(ctx *model.Context, pageDict types.Dict) (types.Dict, error) {
	obj, found := pageDict.Find("Resources")
	if !found {
		return nil, nil
	}
	return ctx.DereferenceDict(obj)
}

// parseFontResource extracts fontInfo from a single font dictionary.
func parseFontResource(ctx *model.Context, key string, val types.Object) *fontInfo {
	fontDict, err := ctx.DereferenceDict(val)
	if err != nil || fontDict == nil {
		return nil
	}

	fi := &fontInfo{
		name:         key,
		defaultWidth: 600, // reasonable default for unknown fonts
	}

	// Check subtype
	subtype, _ := fontDict.Find("Subtype")
	if st, ok := subtype.(types.Name); ok && st.Value() == "Type0" {
		fi.isCID = true
	}

	// Parse ToUnicode CMap (highest priority for glyph → Unicode mapping)
	tuObj, found := fontDict.Find("ToUnicode")
	if found {
		parseToUnicode(ctx, tuObj, fi)
	}

	// Parse widths from descendant CID font
	if fi.isCID {
		descObj, found := fontDict.Find("DescendantFonts")
		if found {
			parseDescendantWidths(ctx, descObj, fi)
		}
	}

	// For non-CID fonts: parse /Encoding (WinAnsi, MacRoman, Standard, or custom)
	if !fi.isCID {
		parseFontEncoding(ctx, fontDict, fi)
		parseSimpleFontWidths(ctx, fontDict, fi)
	}

	return fi
}

// parseFontEncoding reads the /Encoding entry from a simple (non-CID) font.
// Handles:
//   - Name values: /WinAnsiEncoding, /MacRomanEncoding, /StandardEncoding
//   - Dict values: { /BaseEncoding /WinAnsiEncoding /Differences [...] }
func parseFontEncoding(ctx *model.Context, fontDict types.Dict, fi *fontInfo) {
	encObj, found := fontDict.Find("Encoding")
	if !found {
		return
	}

	deref, err := ctx.Dereference(encObj)
	if err != nil {
		return
	}

	switch v := deref.(type) {
	case types.Name:
		// Named encoding
		enc := getEncoding(v.Value())
		if enc != nil {
			fi.encoding = enc
		}

	case types.Dict:
		// Encoding dict with optional /BaseEncoding and /Differences
		var enc encodingTable

		// Start with base encoding (or StandardEncoding as default for Type1)
		baseObj, found := v.Find("BaseEncoding")
		if found {
			if baseName, ok := baseObj.(types.Name); ok {
				base := getEncoding(baseName.Value())
				if base != nil {
					enc = *base
				}
			}
		} else {
			// Default: for Type1 use StandardEncoding, for TrueType use built-in
			subtype, _ := fontDict.Find("Subtype")
			if st, ok := subtype.(types.Name); ok && st.Value() == "Type1" {
				enc = standardEncoding
			} else {
				enc = winAnsiEncoding // sensible default for TrueType
			}
		}

		// Apply /Differences array
		diffObj, found := v.Find("Differences")
		if found {
			applyDifferences(ctx, diffObj, &enc)
		}

		fi.encoding = &enc
	}
}

// applyDifferences modifies an encoding table using the /Differences array.
// Format: [code1 /name1 /name2 ... code2 /name3 ...]
// An integer sets the starting code; subsequent names are assigned to
// consecutive codes.
func applyDifferences(ctx *model.Context, obj types.Object, enc *encodingTable) {
	deref, err := ctx.Dereference(obj)
	if err != nil {
		return
	}
	arr, ok := deref.(types.Array)
	if !ok {
		return
	}

	code := 0
	for _, elem := range arr {
		elemDeref, _ := ctx.Dereference(elem)
		switch v := elemDeref.(type) {
		case types.Integer:
			code = int(v)
		case types.Name:
			glyphName := v.Value()
			if code >= 0 && code < 256 {
				r := resolveGlyphName(glyphName)
				if r != 0xFFFD {
					enc[code] = r
				}
			}
			code++
		}
	}
}

// parseSimpleFontWidths reads the /Widths array from Type1/TrueType fonts.
// Format: /FirstChar N /LastChar M /Widths [w0 w1 ... wM-N]
func parseSimpleFontWidths(ctx *model.Context, fontDict types.Dict, fi *fontInfo) {
	fcObj, found := fontDict.Find("FirstChar")
	if !found {
		return
	}
	lcObj, found := fontDict.Find("LastChar")
	if !found {
		return
	}
	wObj, found := fontDict.Find("Widths")
	if !found {
		return
	}

	fcDeref, _ := ctx.Dereference(fcObj)
	lcDeref, _ := ctx.Dereference(lcObj)

	firstChar, ok1 := toInt(fcDeref)
	lastChar, ok2 := toInt(lcDeref)
	if !ok1 || !ok2 {
		return
	}

	wDeref, err := ctx.Dereference(wObj)
	if err != nil {
		return
	}
	wArr, ok := wDeref.(types.Array)
	if !ok {
		return
	}

	if fi.widths == nil {
		fi.widths = make(map[uint16]int)
	}

	for i := 0; i < len(wArr) && firstChar+i <= lastChar; i++ {
		d, _ := ctx.Dereference(wArr[i])
		w, ok := toInt(d)
		if ok {
			fi.widths[uint16(firstChar+i)] = w
		}
	}
}

// parseToUnicode reads and parses a ToUnicode CMap stream.
func parseToUnicode(ctx *model.Context, obj types.Object, fi *fontInfo) {
	// Dereference to get the stream
	deref, err := ctx.Dereference(obj)
	if err != nil {
		return
	}

	sd, ok := deref.(types.StreamDict)
	if !ok {
		return
	}

	if err := sd.Decode(); err != nil {
		return
	}

	cmapData := string(sd.Content)
	fi.toUnicode = parseCMap(cmapData)

	// Build reverse map (Unicode → glyph ID) for editing
	if len(fi.toUnicode) > 0 {
		fi.fromUnicode = make(map[rune]uint16, len(fi.toUnicode))
		for gid, r := range fi.toUnicode {
			fi.fromUnicode[r] = gid
		}
	}
}

// parseCMap parses a CMap file and returns the glyph→unicode mapping.
func parseCMap(data string) map[uint16]rune {
	m := make(map[uint16]rune)

	// Parse beginbfchar / endbfchar sections
	bfcharRe := regexp.MustCompile(`(?s)beginbfchar\s*\n(.*?)\nendbfchar`)
	for _, match := range bfcharRe.FindAllStringSubmatch(data, -1) {
		lines := strings.Split(strings.TrimSpace(match[1]), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			parts := extractHexPairs(line)
			if len(parts) >= 2 {
				src := parseHex16(parts[0])
				dst := parseHex16(parts[1])
				m[src] = rune(dst)
			}
		}
	}

	// Parse beginbfrange / endbfrange sections
	bfrangeRe := regexp.MustCompile(`(?s)beginbfrange\s*\n(.*?)\nendbfrange`)
	for _, match := range bfrangeRe.FindAllStringSubmatch(data, -1) {
		lines := strings.Split(strings.TrimSpace(match[1]), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			parts := extractHexPairs(line)
			if len(parts) >= 3 {
				srcLo := parseHex16(parts[0])
				srcHi := parseHex16(parts[1])
				dstStart := parseHex16(parts[2])
				for gid := srcLo; gid <= srcHi; gid++ {
					m[gid] = rune(dstStart + (gid - srcLo))
				}
			}
		}
	}

	return m
}

// extractHexPairs pulls <XXXX> values out of a CMap line.
func extractHexPairs(line string) []string {
	re := regexp.MustCompile(`<([0-9A-Fa-f]+)>`)
	matches := re.FindAllStringSubmatch(line, -1)
	result := make([]string, len(matches))
	for i, m := range matches {
		result[i] = m[1]
	}
	return result
}

// parseHex16 parses a hex string into a uint16.
func parseHex16(hex string) uint16 {
	v, _ := strconv.ParseUint(hex, 16, 16)
	return uint16(v)
}

// parseDescendantWidths reads the /W array from a CID font descendant.
func parseDescendantWidths(ctx *model.Context, obj types.Object, fi *fontInfo) {
	deref, err := ctx.Dereference(obj)
	if err != nil {
		return
	}

	arr, ok := deref.(types.Array)
	if !ok || len(arr) == 0 {
		return
	}

	// Get first descendant font
	descRef := arr[0]
	descDeref, err := ctx.Dereference(descRef)
	if err != nil {
		return
	}
	descDict, ok := descDeref.(types.Dict)
	if !ok {
		return
	}

	// Read /DW (default width)
	if dwObj, found := descDict.Find("DW"); found {
		if dw, ok := dwObj.(types.Integer); ok {
			fi.defaultWidth = int(dw)
		}
	}

	// Read /W (width array)
	wObj, found := descDict.Find("W")
	if !found {
		return
	}
	wDeref, err := ctx.Dereference(wObj)
	if err != nil {
		return
	}
	wArr, ok := wDeref.(types.Array)
	if !ok {
		return
	}

	fi.widths = parseWArray(ctx, wArr)
}

// parseWArray decodes the CID font /W array format.
// Format: [startCID [w1 w2 ...]] or [startCID endCID sameWidth]
func parseWArray(ctx *model.Context, arr types.Array) map[uint16]int {
	widths := make(map[uint16]int)
	i := 0

	for i < len(arr) {
		// First element should be a CID (integer)
		startObj, err := ctx.Dereference(arr[i])
		if err != nil {
			i++
			continue
		}
		startCID, ok := toInt(startObj)
		if !ok {
			i++
			continue
		}
		i++
		if i >= len(arr) {
			break
		}

		nextObj, err := ctx.Dereference(arr[i])
		if err != nil {
			i++
			continue
		}

		switch v := nextObj.(type) {
		case types.Array:
			// [startCID [w1 w2 w3 ...]]
			cid := uint16(startCID)
			for _, wElem := range v {
				wDeref, _ := ctx.Dereference(wElem)
				if w, ok := toInt(wDeref); ok {
					widths[cid] = w
				}
				cid++
			}
			i++

		default:
			// [startCID endCID width]
			endCID, ok1 := toInt(v)
			if !ok1 || i+1 >= len(arr) {
				i++
				continue
			}
			i++
			wObj, _ := ctx.Dereference(arr[i])
			w, ok2 := toInt(wObj)
			if ok2 {
				for cid := uint16(startCID); cid <= uint16(endCID); cid++ {
					widths[cid] = w
				}
			}
			i++
		}
	}

	return widths
}

func toInt(obj types.Object) (int, bool) {
	switch v := obj.(type) {
	case types.Integer:
		return int(v), true
	case types.Float:
		return int(v), true
	}
	return 0, false
}

// decodeCIDString maps raw hex bytes through the font's ToUnicode CMap.
// Returns the decoded Unicode string.
func decodeCIDString(raw string, fi *fontInfo) string {
	if fi == nil || fi.toUnicode == nil || len(raw) == 0 {
		return ""
	}

	// Raw is already decoded hex bytes. For CID fonts with Identity-H,
	// each 2-byte pair is a glyph ID.
	var runes []rune
	rawBytes := []byte(raw)

	if len(rawBytes)%2 != 0 {
		// Odd length — try single-byte mode
		for _, b := range rawBytes {
			if r, ok := fi.lookupRune(uint16(b)); ok {
				runes = append(runes, r)
			}
		}
	} else {
		// 2-byte CID mode
		for i := 0; i+1 < len(rawBytes); i += 2 {
			gid := uint16(rawBytes[i])<<8 | uint16(rawBytes[i+1])
			if r, ok := fi.lookupRune(gid); ok {
				runes = append(runes, r)
			}
		}
		// If nothing decoded, try single-byte as fallback
		if len(runes) == 0 {
			for _, b := range rawBytes {
				if r, ok := fi.lookupRune(uint16(b)); ok {
					runes = append(runes, r)
				}
			}
		}
	}

	if len(runes) == 0 {
		return ""
	}
	return string(runes)
}

// decodeHexStringWithFont decodes a hex string using the font's CMap.
// hexRaw is the raw hex digits (e.g. "00340057").
func decodeHexStringWithFont(hexRaw string, fi *fontInfo) string {
	if fi == nil || fi.toUnicode == nil {
		return ""
	}

	// Pad to even length
	if len(hexRaw)%2 != 0 {
		hexRaw += "0"
	}

	// Decode hex to bytes
	var rawBytes []byte
	for i := 0; i+1 < len(hexRaw); i += 2 {
		b, err := strconv.ParseUint(hexRaw[i:i+2], 16, 8)
		if err == nil {
			rawBytes = append(rawBytes, byte(b))
		}
	}

	return decodeCIDString(string(rawBytes), fi)
}

// stringWidth calculates the advance width of a string in PDF text units.
// Returns the width in 1/1000 units (multiply by fontSize/1000 for points).
func stringWidth(hexRaw string, fi *fontInfo) float64 {
	if fi == nil || fi.widths == nil {
		return 0
	}

	if len(hexRaw)%2 != 0 {
		hexRaw += "0"
	}

	var rawBytes []byte
	for i := 0; i+1 < len(hexRaw); i += 2 {
		b, err := strconv.ParseUint(hexRaw[i:i+2], 16, 8)
		if err == nil {
			rawBytes = append(rawBytes, byte(b))
		}
	}

	var total float64
	if len(rawBytes)%2 == 0 && fi.isCID {
		for i := 0; i+1 < len(rawBytes); i += 2 {
			gid := uint16(rawBytes[i])<<8 | uint16(rawBytes[i+1])
			total += float64(fi.glyphWidth(gid))
		}
	} else {
		for _, b := range rawBytes {
			total += float64(fi.glyphWidth(uint16(b)))
		}
	}

	return total
}

func debugFontSummary(fonts pageFonts) string {
	var sb strings.Builder
	for key, fi := range fonts {
		sb.WriteString(fmt.Sprintf("Font %s: isCID=%v, toUnicode entries=%d, width entries=%d, defaultW=%d\n",
			key, fi.isCID, len(fi.toUnicode), len(fi.widths), fi.defaultWidth))
	}
	return sb.String()
}
