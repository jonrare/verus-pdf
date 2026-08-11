package edit

// PDF content stream decoder — positioned text extraction.
//
// Spec: ISO 32000-1:2008. See docs/pdf-spec.md for the full clause index and
// the list of known deviations.
//
//   - Tokeniser over content stream syntax        §7.2, §7.3.4, §8.2
//   - CTM stack: q / Q / cm                       §8.3.3, §8.4.2
//   - Text state: Tf Tc Tw Tz TL Tr Ts            §9.3
//   - Text positioning: Tm Td TD T*               §9.4.2
//   - Text showing: Tj TJ ' "                     §9.4.3
//   - Font decoding via /ToUnicode CMap           §9.7.5
//   - Standard encodings: WinAnsi, MacRoman, …    §9.6.6, Annex D
//   - CID glyph widths for advance tracking       §9.7.4.3
//   - Form XObject traversal via Do               §8.10.1
//   - Span merging: per-character → word/line spans (not in the spec —
//     a display convenience, see mergeAdjacentSpans)
//
// Architecture: streamParser holds all state (CTM stack, text state, fonts,
// pdfcpu context) and accumulates TextSpans. This enables recursive parsing
// of Form XObjects and clean state management.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// TextSpan is a single positioned run of text extracted from the stream.
type TextSpan struct {
	Text     string  `json:"text"`
	X        float64 `json:"x"`        // page-space X (in PDF points, origin bottom-left)
	Y        float64 `json:"y"`        // page-space Y
	Width    float64 `json:"width"`    // page-space width in points (0 = unknown, use estimate)
	Rotation float64 `json:"rotation"` // degrees, CCW, 0 = normal horizontal
	FontName string  `json:"fontName"`
	FontSize float64 `json:"fontSize"` // effective size on the page (includes CTM scale)
	PageNum  int     `json:"pageNum"`

	// Stream location — needed for in-place editing.
	//
	// StreamIndex is an index into the page's /Contents array (0 for a single
	// stream). It is notEditable (-1) when the offsets do not address a page
	// content stream and so cannot be spliced: text inside a Form XObject,
	// whose offsets belong to the XObject's own stream, and text in a BT/ET
	// block split across two parts of a /Contents array.
	StreamIndex int  `json:"streamIndex"`
	OpStart     int  `json:"opStart"` // byte offset of opening ( or <
	OpEnd       int  `json:"opEnd"`   // byte offset just past closing ) or >
	Editable    bool `json:"editable"`

	// BT/ET block boundaries — needed for block-level rewriting
	BlockStart int     `json:"blockStart"` // byte offset of the BT operator
	BlockEnd   int     `json:"blockEnd"`   // byte offset just past ET
	TfSize     float64 `json:"tfSize"`     // raw Tf font size (before CTM scaling)

	// Text matrix [a b c d e f] at the moment this span was shown — not the
	// block's first Tm. Positioning inside a block is commonly done with Td
	// rather than Tm, so the block's Tm says nothing about where later runs
	// sit, and rebuilding from it drops everything back to the block origin.
	TmA float64 `json:"tmA"`
	TmB float64 `json:"tmB"`
	TmC float64 `json:"tmC"`
	TmD float64 `json:"tmD"`
	TmE float64 `json:"tmE"`
	TmF float64 `json:"tmF"`
}

// ── Public API ────────────────────────────────────────────────────────────────

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

	// Load font resources for this page
	fonts := loadPageFonts(ctx, pageNum)

	// Read the page's content streams individually — NOT via
	// pdfcpu.ExtractPageContent, which concatenates a /Contents array into one
	// buffer. The editors splice into individual stream objects, so offsets
	// measured against a concatenation would land in the wrong place on any
	// page whose /Contents is an array (§7.8.2).
	//
	// A /Contents array is one logical stream split at token boundaries, so
	// graphics and text state carry across the parts: one parser, parsed in
	// order, each part tagged with its own index.
	streams, _, err := pageContentStreamsWithRefs(ctx, pageNum)
	if err != nil {
		return nil, fmt.Errorf("extract content: %w", err)
	}
	if len(streams) == 0 {
		return nil, nil
	}

	p := newStreamParser(ctx, fonts, pageNum)
	for i, stream := range streams {
		p.parse(stream, i)
	}

	// Filter empty/whitespace spans but do NOT merge.
	// Merging is done client-side for display only.
	// Raw spans preserve correct OpStart/OpEnd for safe stream editing.
	filtered := filterSpans(p.spans)

	return filtered, nil
}

// ── Graphics state ────────────────────────────────────────────────────────────

// graphicsState holds the mutable state that q/Q saves and restores.
//
// §8.4.1 lists the text state parameters — font, size, Tc, Tw, Tz, TL, Ts and
// the rendering mode — as part of the graphics state, so they belong here and
// not in textState. Keeping the font name here but its size in textState, as
// this once did, meant a Q could leave the two disagreeing.
type graphicsState struct {
	ctm            Matrix // current transformation matrix
	fontKey        string // current font resource name
	curFont        *fontInfo
	textRenderMode int // Tr — 0=fill, 1=stroke, 2=fill+stroke, 3=invisible

	tfSize    float64 // Tf — font size
	charSpace float64 // Tc — extra space after each glyph
	wordSpace float64 // Tw — extra space after the single-byte code 32
	leading   float64 // TL — leading between lines
	hScale    float64 // Tz — horizontal scaling, as a percentage
	rise      float64 // Ts — text rise (superscript/subscript offset)
}

// ── Text state ────────────────────────────────────────────────────────────────

// textState holds the matrices BT resets. Unlike the parameters above, these
// are NOT part of the graphics state and are not saved by q/Q (§9.4.1).
type textState struct {
	tm  Matrix // text matrix
	tlm Matrix // text line matrix
}

// ── Stream parser ─────────────────────────────────────────────────────────────

type streamParser struct {
	ctx     *model.Context
	fonts   pageFonts
	pageNum int

	gs      graphicsState   // current graphics state
	gsStack []graphicsState // saved states (q pushes, Q pops)
	ts      textState       // current text state
	inBT    bool            // inside BT...ET block

	// Block tracking for BT/ET rewriting
	btStart        int // byte offset of current BT
	blockSpanStart int // index into p.spans where current block starts

	spans []TextSpan // accumulated text spans
	depth int        // Form XObject recursion depth

	// Stream identity. streamIdx is the index of the part currently being
	// parsed; btStream is the part the open BT block started in. inXObject is
	// set while recursing into a Form XObject, whose byte offsets belong to a
	// different object entirely.
	streamIdx int
	btStream  int
	inXObject bool

	// res is the /Resources dict currently in scope. It starts as the page's
	// and is replaced while inside a Form XObject that brings its own, so
	// nested XObject names resolve against the right dictionary (§8.10.1).
	res types.Dict
}

const maxFormXObjectDepth = 10 // prevent infinite recursion

// notEditable marks a span whose byte offsets do not address a page content
// stream, so it must never be spliced.
const notEditable = -1

// spanStream returns the stream index to record on a span emitted right now,
// or notEditable if its offsets cannot be safely spliced.
func (p *streamParser) spanStream() int {
	if p.inXObject {
		return notEditable
	}
	// A BT/ET block split across two parts of a /Contents array has its
	// BlockStart in one part and its operands in another; neither the
	// string-level nor the block-level editor can address that.
	if p.inBT && p.btStream != p.streamIdx {
		return notEditable
	}
	return p.streamIdx
}

func newStreamParser(ctx *model.Context, fonts pageFonts, pageNum int) *streamParser {
	p := &streamParser{
		ctx:     ctx,
		fonts:   fonts,
		pageNum: pageNum,
		gs: graphicsState{
			ctm:    Identity(),
			tfSize: 12,
			hScale: 100, // Tz default is 100 percent (§9.3.4)
		},
		ts: textState{
			tm:  Identity(),
			tlm: Identity(),
		},
	}

	// Seed the resource scope with the page's own /Resources.
	if ctx != nil {
		if pageDict, _, _, err := ctx.PageDict(pageNum, false); err == nil && pageDict != nil {
			if resDict, err := resourceDict(ctx, pageDict); err == nil {
				p.res = resDict
			}
		}
	}
	return p
}

// effectiveSize returns the rendered font size on the page.
// This accounts for the text matrix scale and the CTM scale.
func (p *streamParser) effectiveSize() float64 {
	// Font size as affected by text matrix
	tmScale := p.ts.tm.ScaleX()
	if tmScale <= 0 {
		tmScale = 1
	}
	size := p.gs.tfSize * tmScale

	// Apply CTM scale to get page-space size
	ctmScale := p.gs.ctm.ScaleX()
	if ctmScale <= 0 {
		ctmScale = 1
	}
	pageSize := size * ctmScale

	if pageSize < 0 {
		pageSize = -pageSize
	}
	if pageSize <= 0 {
		pageSize = 12
	}
	return pageSize
}

// textOrigin returns the current text position in page space.
// Transforms the text matrix origin through the CTM.
func (p *streamParser) textOrigin() (float64, float64) {
	// The text rendering position is at (0,0) in text space,
	// transformed by the text matrix to get user-space position,
	// then by the CTM to get page-space position.
	ux, uy := p.ts.tm.E, p.ts.tm.F
	return p.gs.ctm.Transform(ux, uy)
}

// textRotation returns the total rotation angle combining text matrix and CTM.
func (p *streamParser) textRotation() float64 {
	combined := p.ts.tm.Multiply(p.gs.ctm)
	return combined.Rotation()
}

// originDistance returns how far the text origin has moved in page space since
// it was at (fromX, fromY).
func (p *streamParser) originDistance(fromX, fromY float64) float64 {
	x, y := p.textOrigin()
	return math.Hypot(x-fromX, y-fromY)
}

// advanceText displaces the text matrix by tx in unscaled text space.
//
// §9.4.4 defines this as Tm' = Translate(tx, 0) × Tm. Adding tx straight to
// Tm.E instead — as this used to — is only correct when Tm is axis-aligned and
// unscaled; under rotation, skew or scale the position drifts with every glyph.
func (p *streamParser) advanceText(tx float64) {
	p.ts.tm = Translate(tx, 0).Multiply(p.ts.tm)
}

// glyphAdvance returns the text-space displacement for showing a string.
//
// §9.4.4: tx = ((w0 − Tj/1000) × Tfs + Tc + Tw) × Th
//
// The Tj term is handled separately by the TJ operator, so this covers the
// glyph widths plus character and word spacing, all scaled by Tz.
func (p *streamParser) glyphAdvance(hexRaw, text string) float64 {
	fi := p.gs.curFont
	runes := []rune(text)

	var w float64
	if fi != nil && fi.widths != nil && hexRaw != "" {
		w = stringWidth(hexRaw, fi) * p.gs.tfSize / 1000.0
	} else {
		// No resolvable metrics — estimate. See docs/pdf-spec.md on the
		// standard-14 metrics gap.
		w = float64(len(runes)) * p.gs.tfSize * 0.5
	}

	// Tc applies to every glyph shown; Tw applies to the single-byte code 32
	// only (§9.3.3), which for simple fonts is the space character. CID fonts
	// almost never use a single-byte code 32, so word spacing is skipped there.
	w += float64(len(runes)) * p.gs.charSpace
	if fi == nil || !fi.isCID {
		w += float64(strings.Count(text, " ")) * p.gs.wordSpace
	}

	return w * (p.gs.hScale / 100.0)
}

// ── Parsing ───────────────────────────────────────────────────────────────────

func (p *streamParser) parse(stream []byte, streamIdx int) {
	p.streamIdx = streamIdx
	tokens := tokenise(stream)

	for i, tok := range tokens {
		if tok.kind != tokOperator {
			continue
		}
		op := tok.value
		operands := collectOperands(tokens, i)

		switch op {

		// ── Graphics state operators ──────────────────────────────────
		case "q":
			p.gsStack = append(p.gsStack, p.gs)

		case "Q":
			n := len(p.gsStack)
			if n > 0 {
				p.gs = p.gsStack[n-1]
				p.gsStack = p.gsStack[:n-1]
			}

		case "cm":
			if len(operands) >= 6 {
				a := parseFloat(operands[len(operands)-6].value)
				b := parseFloat(operands[len(operands)-5].value)
				c := parseFloat(operands[len(operands)-4].value)
				d := parseFloat(operands[len(operands)-3].value)
				e := parseFloat(operands[len(operands)-2].value)
				f := parseFloat(operands[len(operands)-1].value)
				mArg := Matrix{A: a, B: b, C: c, D: d, E: e, F: f}
				// CTM' = M_arg × CTM (pre-multiplication)
				p.gs.ctm = mArg.Multiply(p.gs.ctm)
			}

		// ── Text object operators ─────────────────────────────────────
		case "BT":
			p.inBT = true
			p.ts.tm = Identity()
			p.ts.tlm = Identity()
			p.btStart = tok.start
			p.btStream = streamIdx
			p.blockSpanStart = len(p.spans)

		case "ET":
			// Fill in BlockEnd for all spans emitted in this BT block
			etEnd := tok.end
			for si := p.blockSpanStart; si < len(p.spans); si++ {
				p.spans[si].BlockEnd = etEnd
			}
			p.inBT = false

		// ── Text state operators ──────────────────────────────────────
		case "Tf":
			if len(operands) >= 2 {
				p.gs.fontKey = strings.TrimPrefix(operands[len(operands)-2].value, "/")
				p.gs.tfSize = parseFloat(operands[len(operands)-1].value)
				if p.fonts != nil {
					p.gs.curFont = p.fonts[p.gs.fontKey]
				}
			}

		case "Tc": // character spacing
			if len(operands) >= 1 {
				p.gs.charSpace = parseFloat(operands[0].value)
			}

		case "Tw": // word spacing
			if len(operands) >= 1 {
				p.gs.wordSpace = parseFloat(operands[0].value)
			}

		case "TL": // leading
			if len(operands) >= 1 {
				p.gs.leading = parseFloat(operands[0].value)
			}

		case "Tz": // horizontal scaling
			if len(operands) >= 1 {
				p.gs.hScale = parseFloat(operands[0].value)
				if p.gs.hScale == 0 {
					p.gs.hScale = 100
				}
			}

		case "Ts": // text rise
			if len(operands) >= 1 {
				p.gs.rise = parseFloat(operands[0].value)
			}

		case "Tr": // text rendering mode
			if len(operands) >= 1 {
				p.gs.textRenderMode = int(parseFloat(operands[0].value))
			}

		// ── Text positioning operators ────────────────────────────────
		case "Tm":
			if len(operands) >= 6 {
				a := parseFloat(operands[len(operands)-6].value)
				b := parseFloat(operands[len(operands)-5].value)
				c := parseFloat(operands[len(operands)-4].value)
				d := parseFloat(operands[len(operands)-3].value)
				e := parseFloat(operands[len(operands)-2].value)
				f := parseFloat(operands[len(operands)-1].value)
				p.ts.tm = Matrix{A: a, B: b, C: c, D: d, E: e, F: f}
				p.ts.tlm = p.ts.tm
			}

		case "Td":
			if len(operands) >= 2 {
				tx := parseFloat(operands[len(operands)-2].value)
				ty := parseFloat(operands[len(operands)-1].value)
				// Tlm = Translate(tx, ty) × Tlm ; Tm = Tlm
				t := Translate(tx, ty)
				p.ts.tlm = t.Multiply(p.ts.tlm)
				p.ts.tm = p.ts.tlm
			}

		case "TD":
			if len(operands) >= 2 {
				tx := parseFloat(operands[len(operands)-2].value)
				ty := parseFloat(operands[len(operands)-1].value)
				p.gs.leading = -ty
				t := Translate(tx, ty)
				p.ts.tlm = t.Multiply(p.ts.tlm)
				p.ts.tm = p.ts.tlm
			}

		case "T*":
			t := Translate(0, -p.gs.leading)
			p.ts.tlm = t.Multiply(p.ts.tlm)
			p.ts.tm = p.ts.tlm

		// ── Text showing operators ────────────────────────────────────
		case "Tj":
			if p.inBT && len(operands) >= 1 {
				p.showString(operands[len(operands)-1], streamIdx)
			}

		case "TJ":
			if p.inBT {
				p.showTJArray(operands, streamIdx)
			}

		case "'":
			// Move to next line, then show string
			t := Translate(0, -p.gs.leading)
			p.ts.tlm = t.Multiply(p.ts.tlm)
			p.ts.tm = p.ts.tlm
			if p.inBT && len(operands) >= 1 {
				p.showString(operands[len(operands)-1], streamIdx)
			}

		case `"`:
			// Set word spacing, char spacing, move to next line, show string
			if p.inBT && len(operands) >= 3 {
				p.gs.wordSpace = parseFloat(operands[len(operands)-3].value)
				p.gs.charSpace = parseFloat(operands[len(operands)-2].value)
				t := Translate(0, -p.gs.leading)
				p.ts.tlm = t.Multiply(p.ts.tlm)
				p.ts.tm = p.ts.tlm
				p.showString(operands[len(operands)-1], streamIdx)
			}

		// ── Form XObject invocation ───────────────────────────────────
		case "Do":
			if len(operands) >= 1 && p.depth < maxFormXObjectDepth {
				xobjName := strings.TrimPrefix(operands[len(operands)-1].value, "/")
				p.handleFormXObject(xobjName)
			}
		}
	}
}

// showString handles a single Tj string operand.
func (p *streamParser) showString(tok token, streamIdx int) {
	text, hexRaw := p.decodeToken(tok)
	if text == "" {
		return
	}

	px, py := p.textOrigin()
	rot := p.textRotation()
	fsize := p.effectiveSize()
	startTm := p.ts.tm

	p.advanceText(p.glyphAdvance(hexRaw, text))

	// Width is the distance the origin actually travelled in page space. Taking
	// it from the transformed positions rather than rescaling the text-space
	// advance by hand keeps Tz, the text matrix and the CTM applied exactly
	// once each, and stays correct under rotation and skew.
	pageWidth := p.originDistance(px, py)

	p.spans = append(p.spans, TextSpan{
		Text:        text,
		X:           px,
		Y:           py,
		Width:       pageWidth,
		Rotation:    rot,
		FontName:    p.gs.fontKey,
		FontSize:    fsize,
		PageNum:     p.pageNum,
		StreamIndex: p.spanStream(),
		Editable:    p.spanStream() != notEditable,
		OpStart:     tok.start,
		OpEnd:       tok.end,
		// Block fields (BlockEnd filled in at ET)
		BlockStart: p.btStart,
		TfSize:     p.gs.tfSize,
		TmA:        startTm.A, TmB: startTm.B,
		TmC: startTm.C, TmD: startTm.D,
		TmE: startTm.E, TmF: startTm.F,
	})
}

// showTJArray handles a TJ array of strings and kerning values.
func (p *streamParser) showTJArray(operands []token, streamIdx int) {
	var sb strings.Builder
	firstStart, lastEnd := -1, -1

	px, py := p.textOrigin()
	rot := p.textRotation()
	fsize := p.effectiveSize()
	startTm := p.ts.tm

	for _, op := range operands {
		switch op.kind {
		case tokString, tokHexString:
			text, hexRaw := p.decodeToken(op)
			sb.WriteString(text)
			if firstStart == -1 {
				firstStart = op.start
			}
			lastEnd = op.end
			p.advanceText(p.glyphAdvance(hexRaw, text))

		case tokNumber:
			// TJ displacement: positive moves left, negative moves right.
			// A large one is a word break the encoder expressed as kerning
			// rather than a space glyph.
			kern := parseFloat(op.value)
			if kern < -200 || kern > 200 {
				sb.WriteByte(' ')
			}
			// §9.4.3: the number is in thousandths of a text-space unit,
			// subtracted from the position and scaled by Tz.
			p.advanceText(-kern / 1000.0 * p.gs.tfSize * (p.gs.hScale / 100.0))
		}
	}

	fullText := sb.String()
	if fullText != "" && firstStart >= 0 {
		pageWidth := p.originDistance(px, py)

		p.spans = append(p.spans, TextSpan{
			Text:        fullText,
			X:           px,
			Y:           py,
			Width:       pageWidth,
			Rotation:    rot,
			FontName:    p.gs.fontKey,
			FontSize:    fsize,
			PageNum:     p.pageNum,
			StreamIndex: p.spanStream(),
			Editable:    p.spanStream() != notEditable,
			OpStart:     firstStart,
			OpEnd:       lastEnd,
			BlockStart:  p.btStart,
			TfSize:      p.gs.tfSize,
			TmA:         startTm.A, TmB: startTm.B,
			TmC: startTm.C, TmD: startTm.D,
			TmE: startTm.E, TmF: startTm.F,
		})
	}
}

// decodeToken decodes a string or hex string token using the current font.
// Returns (decoded_text, raw_hex_for_width_calc).
func (p *streamParser) decodeToken(tok token) (string, string) {
	fi := p.gs.curFont
	if tok.kind == tokHexString {
		if fi != nil && fi.isCID && fi.toUnicode != nil {
			text := decodeHexStringWithFont(tok.value, fi)
			if text != "" {
				return text, tok.value
			}
		}
		return decodeString(tok.value, tokHexString), tok.value
	}

	// Literal string
	if fi != nil && fi.isCID && fi.toUnicode != nil {
		text := decodeCIDString(tok.value, fi)
		if text != "" {
			return text, ""
		}
	}

	// Standard encoding for non-CID fonts.
	//
	// The second return value feeds stringWidth, which indexes the font's
	// width table by character code — so it must carry the *raw* bytes, hex
	// encoded, not the decoded Unicode. Returning "" here silently defeats
	// every /Widths lookup and falls back to the 0.5 em estimate.
	if fi != nil && !fi.isCID && fi.encoding != nil {
		var sb strings.Builder
		for _, b := range []byte(tok.value) {
			r := fi.encoding[b]
			if r == 0 {
				r = rune(b)
			}
			sb.WriteRune(r)
		}
		return sb.String(), hex.EncodeToString([]byte(tok.value))
	}

	return decodeString(tok.value, tokString), hex.EncodeToString([]byte(tok.value))
}

// ── Form XObject handling ─────────────────────────────────────────────────────

func (p *streamParser) handleFormXObject(name string) {
	if p.ctx == nil {
		return
	}

	// Look up the XObject in the page's resources.
	// We need to find it through the current page's resource chain.
	xobj := p.lookupXObject(name)
	if xobj == nil {
		return
	}

	xobjDict, ok := xobj.(types.StreamDict)
	if !ok {
		return
	}

	// Check Subtype — we only care about Form XObjects
	subtype, found := xobjDict.Find("Subtype")
	if !found {
		return
	}
	if st, ok := subtype.(types.Name); !ok || st.Value() != "Form" {
		return
	}

	// Decode the stream content
	if err := xobjDict.Decode(); err != nil {
		return
	}

	// Get the Form XObject's own resources (if any). A form that brings its
	// own /Resources shadows the enclosing scope for both fonts and nested
	// XObjects; one that does not inherits (§8.10.1).
	formFonts := p.fonts // inherit enclosing fonts
	formRes := p.res     // inherit enclosing resources
	resObj, found := xobjDict.Find("Resources")
	if found {
		resDict, err := p.ctx.DereferenceDict(resObj)
		if err == nil && resDict != nil {
			formRes = resDict
			fontObj, fontFound := resDict.Find("Font")
			if fontFound {
				fontDict, err := p.ctx.DereferenceDict(fontObj)
				if err == nil && fontDict != nil {
					formFonts = make(pageFonts)
					// Inherit page fonts first
					for k, v := range p.fonts {
						formFonts[k] = v
					}
					// Override with Form's own fonts
					for key, val := range fontDict {
						fi := parseFontResource(p.ctx, key, val)
						if fi != nil {
							formFonts[key] = fi
						}
					}
				}
			}
		}
	}

	// Get the Form XObject's Matrix (optional, default identity)
	formMatrix := Identity()
	matrixObj, found := xobjDict.Find("Matrix")
	if found {
		matArr, err := p.ctx.Dereference(matrixObj)
		if err == nil {
			if arr, ok := matArr.(types.Array); ok && len(arr) >= 6 {
				vals := make([]float64, 6)
				for j := 0; j < 6; j++ {
					d, _ := p.ctx.Dereference(arr[j])
					vals[j] = objToFloat(d)
				}
				formMatrix = Matrix{A: vals[0], B: vals[1], C: vals[2], D: vals[3], E: vals[4], F: vals[5]}
			}
		}
	}

	// Save graphics state, apply form matrix, parse recursively, restore.
	//
	// The stream identity is saved too: the form's content is a different
	// object, so byte offsets recorded inside it do not address the page's
	// content stream. inXObject makes every span emitted in there notEditable,
	// and the caller's streamIdx has to be restored afterwards or the rest of
	// the page would be attributed to the wrong stream.
	savedGS := p.gs
	savedTS := p.ts
	savedBT := p.inBT
	savedFonts := p.fonts
	savedStreamIdx := p.streamIdx
	savedBTStream := p.btStream
	savedInXObject := p.inXObject
	savedRes := p.res

	p.gs.ctm = formMatrix.Multiply(p.gs.ctm)
	p.fonts = formFonts
	p.res = formRes
	p.inBT = false
	p.inXObject = true
	p.depth++

	p.parse(xobjDict.Content, notEditable)

	p.depth--
	p.gs = savedGS
	p.ts = savedTS
	p.inBT = savedBT
	p.fonts = savedFonts
	p.streamIdx = savedStreamIdx
	p.btStream = savedBTStream
	p.inXObject = savedInXObject
	p.res = savedRes
}

func (p *streamParser) lookupXObject(name string) types.Object {
	if p.ctx == nil || p.res == nil {
		return nil
	}

	xobjObj, found := p.res.Find("XObject")
	if !found {
		return nil
	}

	xobjDict, err := p.ctx.DereferenceDict(xobjObj)
	if err != nil || xobjDict == nil {
		return nil
	}

	ref, found := xobjDict.Find(name)
	if !found {
		return nil
	}

	deref, err := p.ctx.Dereference(ref)
	if err != nil {
		return nil
	}

	return deref
}

// ── String decoding helpers ──────────────────────────────────────────────────

func decodeString(raw string, kind tokenKind) string {
	if kind == tokHexString {
		if len(raw)%2 != 0 {
			raw += "0"
		}

		// Try UTF-16BE for hex strings with 4-char aligned length
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
			if err == nil {
				buf.WriteByte(byte(b))
			}
		}
		return buf.String()
	}

	// Literal string — passthrough (already unescaped by tokeniser)
	var buf bytes.Buffer
	for _, c := range []byte(raw) {
		if c < 128 {
			buf.WriteByte(c)
		} else {
			buf.WriteRune(rune(c))
		}
	}
	return buf.String()
}

func decodeHexAsUTF16BE(hex string) string {
	var runes []rune
	for i := 0; i+3 < len(hex); i += 4 {
		hi, err1 := strconv.ParseUint(hex[i:i+2], 16, 8)
		lo, err2 := strconv.ParseUint(hex[i+2:i+4], 16, 8)
		if err1 != nil || err2 != nil {
			return ""
		}
		cp := rune(hi)<<8 | rune(lo)
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

// ── Post-processing ──────────────────────────────────────────────────────────

// filterSpans removes empty, whitespace-only, and non-printable spans.
func filterSpans(spans []TextSpan) []TextSpan {
	var out []TextSpan
	for _, s := range spans {
		trimmed := strings.TrimSpace(s.Text)
		if trimmed == "" {
			continue
		}
		hasPrintable := false
		for _, r := range trimmed {
			if unicode.IsPrint(r) && r != '\uFFFD' {
				hasPrintable = true
				break
			}
		}
		if hasPrintable {
			out = append(out, s)
		}
	}
	return out
}

// Span-merge tuning, all expressed in ems of the current span's font size.
const (
	// Vertical tolerance for "same baseline".
	sameLineEm = 0.4
	// Gap beyond the end of a known-width span that reads as a word space.
	// A word space is ~0.25 em in the common text faces; half of that is a
	// safe floor.
	spaceGapEm = 0.15
	// Gaps wider than this are column or cell breaks, not word breaks.
	maxGapEm = 2.0
	// An advance beyond this cannot be a single glyph in a proportional face
	// (the widest standard glyphs reach about 1.0 em), so it must contain a
	// space. Used for the first step of a run, where there is no history yet.
	implausibleGlyphEm = 1.2
	// How much wider than the run's widest glyph an advance must be before it
	// is read as containing a space.
	spaceAdvanceRatio = 1.35
)

// runState tracks what the current merged run has already seen, so the
// unknown-width path can compare a new advance against real evidence from this
// font rather than a fixed guess.
type runState struct {
	prevX     float64 // origin of the span most recently folded in
	prevRunes int     // its rune count
	maxAdv    float64 // widest per-glyph advance observed in this run
}

func newRunState(s TextSpan) runState {
	return runState{prevX: s.X, prevRunes: len([]rune(s.Text))}
}

// mergeAdjacentSpans combines spans on the same line that are close together.
// This turns per-character spans into readable word/sentence spans.
//
// Two regimes, depending on whether the decoder resolved glyph widths:
//
//   - Width is known: measure the gap from the true end of the span. Simple and
//     accurate.
//   - Width is unknown: fall back to the origin-to-origin advance of the
//     previous glyph run, compared against the widest advance seen so far in
//     this run. This is inherently lossy — a narrow glyph followed by a space
//     can advance less than a wide glyph alone — so it errs toward keeping
//     words intact rather than shattering them.
func mergeAdjacentSpans(spans []TextSpan) []TextSpan {
	if len(spans) == 0 {
		return nil
	}

	var merged []TextSpan
	current := spans[0]
	run := newRunState(current)

	for i := 1; i < len(spans); i++ {
		next := spans[i]

		sameLine := math.Abs(current.Y-next.Y) < current.FontSize*sameLineEm
		sameFont := current.FontName == next.FontName
		sameSize := math.Abs(current.FontSize-next.FontSize) < 1.0
		sameRot := math.Abs(current.Rotation-next.Rotation) < 1.0

		needsSpace, adjacent := false, false
		if sameLine && sameFont && sameSize && sameRot {
			needsSpace, adjacent = spanGap(current, next, &run)
		}

		if !adjacent {
			merged = append(merged, current)
			current = next
			run = newRunState(next)
			continue
		}

		if needsSpace {
			current.Text += " " + next.Text
		} else {
			current.Text += next.Text
		}
		// Extend the byte range
		if next.OpEnd > current.OpEnd {
			current.OpEnd = next.OpEnd
		}
		// Only widen from a real measurement — never fabricate a width, or the
		// error compounds across the run.
		if next.Width > 0 {
			current.Width = (next.X + next.Width) - current.X
		}
		run.prevX = next.X
		run.prevRunes = len([]rune(next.Text))
	}
	merged = append(merged, current)

	return merged
}

// spanGap reports whether next continues current on the same line, and whether
// a space belongs between them. It updates run with the advance it observed.
func spanGap(current, next TextSpan, run *runState) (needsSpace, adjacent bool) {
	size := current.FontSize
	if size <= 0 {
		size = 12
	}

	if current.Width > 0 {
		gap := next.X - (current.X + current.Width)
		if gap <= -size*0.5 || gap >= size*maxGapEm {
			return false, false
		}
		return gap > size*spaceGapEm, true
	}

	// Unknown width — measure what the previous glyph run actually consumed.
	//
	// This cannot reliably find word spaces, and no threshold can. With
	// per-character spans in 12pt Helvetica, "Hi there" advances 6.000 from
	// 'i' to 't' *including* the space, but 6.672 from 'h' to 'e' with no
	// space at all: the step containing a space is the smaller of the two.
	// Narrow glyphs plus a space simply overlap wide glyphs alone.
	//
	// So this errs toward keeping words intact — a missing space is easier to
	// read past than a shattered word — and only claims a space when the
	// advance is too large to be a single glyph. The real fix is to stop
	// landing here: see docs/pdf-spec.md on standard-14 font metrics.
	advance := next.X - run.prevX
	if run.prevRunes > 1 {
		advance /= float64(run.prevRunes)
	}
	if advance <= -size*0.5 || advance >= size*(1+maxGapEm) {
		return false, false
	}

	// With no history, the only defensible claim is that an advance wider than
	// any single glyph must contain a space.
	threshold := size * implausibleGlyphEm
	if run.maxAdv > 0 {
		threshold = math.Min(threshold, run.maxAdv*spaceAdvanceRatio)
	}
	needsSpace = advance > threshold

	// Only glyph advances (not word gaps) inform the baseline.
	if !needsSpace && advance > run.maxAdv {
		run.maxAdv = advance
	}
	return needsSpace, true
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
		if isWS(src[i]) {
			i++
			continue
		}

		// Comment
		if src[i] == '%' {
			for i < n && src[i] != '\n' && src[i] != '\r' {
				i++
			}
			continue
		}

		// Literal string
		if src[i] == '(' {
			start := i
			i++
			depth := 1
			var buf bytes.Buffer
			for i < n && depth > 0 {
				c := src[i]
				if c == '\\' && i+1 < n {
					i++
					switch src[i] {
					case 'n':
						buf.WriteByte('\n')
					case 'r':
						buf.WriteByte('\r')
					case 't':
						buf.WriteByte('\t')
					case 'b':
						buf.WriteByte('\b')
					case 'f':
						buf.WriteByte('\f')
					case '(':
						buf.WriteByte('(')
					case ')':
						buf.WriteByte(')')
					case '\\':
						buf.WriteByte('\\')
					case '\n':
						// Line continuation — emits nothing (§7.3.4.2)
					case '\r':
						// CR or CRLF continuation — emits nothing (§7.3.4.2)
						if i+1 < n && src[i+1] == '\n' {
							i++
						}
					default:
						if src[i] >= '0' && src[i] <= '7' {
							octal := string(src[i])
							for j := 1; j < 3 && i+1 < n && src[i+1] >= '0' && src[i+1] <= '7'; j++ {
								i++
								octal += string(src[i])
							}
							v, _ := strconv.ParseUint(octal, 8, 8)
							buf.WriteByte(byte(v))
						} else {
							buf.WriteByte(src[i])
						}
					}
				} else if c == '(' {
					depth++
					buf.WriteByte(c)
				} else if c == ')' {
					depth--
					if depth > 0 {
						buf.WriteByte(c)
					}
				} else {
					buf.WriteByte(c)
				}
				i++
			}
			tokens = append(tokens, token{kind: tokString, value: buf.String(), start: start, end: i})
			continue
		}

		// Hex string
		if src[i] == '<' && i+1 < n && src[i+1] != '<' {
			start := i
			i++
			var buf bytes.Buffer
			for i < n && src[i] != '>' {
				if !isWS(src[i]) { // hex strings can contain whitespace
					buf.WriteByte(src[i])
				}
				i++
			}
			if i < n {
				i++
			}
			tokens = append(tokens, token{kind: tokHexString, value: buf.String(), start: start, end: i})
			continue
		}

		// Array delimiters
		if src[i] == '[' {
			tokens = append(tokens, token{kind: tokArrayOpen, value: "[", start: i, end: i + 1})
			i++
			continue
		}
		if src[i] == ']' {
			tokens = append(tokens, token{kind: tokArrayClose, value: "]", start: i, end: i + 1})
			i++
			continue
		}

		// Dict delimiters — skip the dict contents entirely
		if src[i] == '<' && i+1 < n && src[i+1] == '<' {
			i += 2
			depth := 1
			for i+1 < n && depth > 0 {
				if src[i] == '<' && src[i+1] == '<' {
					depth++
					i += 2
				} else if src[i] == '>' && src[i+1] == '>' {
					depth--
					i += 2
				} else {
					i++
				}
			}
			continue
		}

		// Name
		if src[i] == '/' {
			start := i
			i++
			var buf bytes.Buffer
			for i < n && !isWS(src[i]) && !isDelim(src[i]) {
				buf.WriteByte(src[i])
				i++
			}
			tokens = append(tokens, token{kind: tokName, value: "/" + buf.String(), start: start, end: i})
			continue
		}

		// Number or operator
		start := i
		var buf bytes.Buffer
		for i < n && !isWS(src[i]) && !isDelim(src[i]) {
			buf.WriteByte(src[i])
			i++
		}
		word := buf.String()
		if word == "" {
			i++
			continue
		}
		kind := tokOperator
		if isNumber(word) {
			kind = tokNumber
		}
		tokens = append(tokens, token{kind: kind, value: word, start: start, end: i})
	}
	return tokens
}

func collectOperands(tokens []token, opIdx int) []token {
	var ops []token
	for i := opIdx - 1; i >= 0; i-- {
		t := tokens[i]
		if t.kind == tokOperator {
			break
		}
		if t.kind != tokArrayOpen && t.kind != tokArrayClose {
			ops = append([]token{t}, ops...)
		}
	}
	return ops
}

// ── Utilities ─────────────────────────────────────────────────────────────────

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func objToFloat(obj types.Object) float64 {
	switch v := obj.(type) {
	case types.Integer:
		return float64(v)
	case types.Float:
		return float64(v)
	}
	return 0
}

func isWS(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == 0
}

func isDelim(c byte) bool {
	return c == '(' || c == ')' || c == '<' || c == '>' ||
		c == '[' || c == ']' || c == '{' || c == '}' || c == '/' || c == '%'
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}
