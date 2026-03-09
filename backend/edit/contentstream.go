package edit

// PDF content stream decoder — production-grade text extraction.
//
// Implements the full text rendering pipeline from ISO 32000-1:2008 §9:
//   - CTM stack with q/Q push/pop and cm concatenation (§8.3, §8.4.4)
//   - All text positioning operators: Tm, Td, TD, T*, TL (§9.4.2)
//   - All text showing operators: Tj, TJ, ', " (§9.4.3)
//   - Text state: Tf, Tc, Tw, Tz, TL, Tr, Ts (§9.3)
//   - Font decoding via ToUnicode CMap (§9.10.2)
//   - Standard font encodings: WinAnsi, MacRoman, Standard (§9.6.6)
//   - CID font glyph width tables for position tracking (§9.7.4.3)
//   - Form XObject traversal via Do operator (§8.10)
//   - Span merging: per-character spans → readable word/line spans
//
// Architecture: streamParser holds all state (CTM stack, text state, fonts,
// pdfcpu context) and accumulates TextSpans. This enables recursive parsing
// of Form XObjects and clean state management.

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpupkg "github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// TextSpan is a single positioned run of text extracted from the stream.
type TextSpan struct {
	Text     string  `json:"text"`
	X        float64 `json:"x"`    // page-space X (in PDF points, origin bottom-left)
	Y        float64 `json:"y"`    // page-space Y
	Width    float64 `json:"width"`    // page-space width in points (0 = unknown, use estimate)
	Rotation float64 `json:"rotation"` // degrees, CCW, 0 = normal horizontal
	FontName string  `json:"fontName"`
	FontSize float64 `json:"fontSize"` // effective size on the page (includes CTM scale)
	PageNum  int     `json:"pageNum"`

	// Stream location — needed for in-place editing
	StreamIndex int `json:"streamIndex"`
	OpStart     int `json:"opStart"` // byte offset of opening ( or <
	OpEnd       int `json:"opEnd"`   // byte offset just past closing ) or >

	// BT/ET block boundaries — needed for block-level rewriting
	BlockStart int     `json:"blockStart"` // byte offset of the BT operator
	BlockEnd   int     `json:"blockEnd"`   // byte offset just past ET
	TfSize     float64 `json:"tfSize"`     // raw Tf font size (before CTM scaling)
	TmA        float64 `json:"tmA"`        // text matrix at block start [a b c d e f]
	TmB        float64 `json:"tmB"`
	TmC        float64 `json:"tmC"`
	TmD        float64 `json:"tmD"`
	TmE        float64 `json:"tmE"`
	TmF        float64 `json:"tmF"`
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

	// Get the content stream
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

	// Parse with full CTM tracking
	p := newStreamParser(ctx, fonts, pageNum)
	p.parse(stream, 0)

	// Filter empty/whitespace spans but do NOT merge.
	// Merging is done client-side for display only.
	// Raw spans preserve correct OpStart/OpEnd for safe stream editing.
	filtered := filterSpans(p.spans)

	return filtered, nil
}

// ── Graphics state ────────────────────────────────────────────────────────────

// graphicsState holds the mutable state that q/Q saves and restores.
type graphicsState struct {
	ctm       Matrix  // current transformation matrix
	fontKey   string  // current font resource name
	curFont   *fontInfo
	textRenderMode int // Tr — 0=fill, 1=stroke, 2=fill+stroke, 3=invisible
}

// ── Text state ────────────────────────────────────────────────────────────────

type textState struct {
	tm        Matrix  // text matrix
	tlm       Matrix  // text line matrix
	tfSize    float64 // font size from Tf
	charSpace float64 // Tc — extra space after each character
	wordSpace float64 // Tw — extra space after space characters (char code 32)
	leading   float64 // TL — leading between lines
	hScale    float64 // Tz — horizontal scaling (percentage, default 100)
	rise      float64 // Ts — text rise (superscript/subscript offset)
}

// ── Stream parser ─────────────────────────────────────────────────────────────

type streamParser struct {
	ctx       *model.Context
	fonts     pageFonts
	pageNum   int

	gs        graphicsState   // current graphics state
	gsStack   []graphicsState // saved states (q pushes, Q pops)
	ts        textState       // current text state
	inBT      bool            // inside BT...ET block

	// Block tracking for BT/ET rewriting
	btStart        int     // byte offset of current BT
	blockTm        Matrix  // the Tm set in the current BT block
	blockSpanStart int     // index into p.spans where current block starts

	spans     []TextSpan      // accumulated text spans
	depth     int             // Form XObject recursion depth
}

const maxFormXObjectDepth = 10 // prevent infinite recursion

func newStreamParser(ctx *model.Context, fonts pageFonts, pageNum int) *streamParser {
	return &streamParser{
		ctx:     ctx,
		fonts:   fonts,
		pageNum: pageNum,
		gs: graphicsState{
			ctm: Identity(),
		},
		ts: textState{
			tm:     Identity(),
			tlm:    Identity(),
			tfSize: 12,
			hScale: 100,
		},
	}
}

// effectiveSize returns the rendered font size on the page.
// This accounts for the text matrix scale and the CTM scale.
func (p *streamParser) effectiveSize() float64 {
	// Font size as affected by text matrix
	tmScale := p.ts.tm.ScaleX()
	if tmScale <= 0 {
		tmScale = 1
	}
	size := p.ts.tfSize * tmScale

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

// advanceTx moves the text position rightward by the given user-space width.
// This is used after rendering a glyph string to update the text matrix.
func (p *streamParser) advanceTx(userWidth float64) {
	// In text space, horizontal advance is along the text matrix X axis.
	// We apply horizontal scaling and add to the text matrix E component.
	p.ts.tm.E += userWidth * (p.ts.hScale / 100.0)
}

// glyphAdvance calculates the user-space advance width for a hex string.
func (p *streamParser) glyphAdvance(hexRaw string, charCount int) float64 {
	fi := p.gs.curFont
	if fi != nil && fi.isCID && hexRaw != "" {
		w := stringWidth(hexRaw, fi)
		return w * p.ts.tfSize / 1000.0
	}
	if fi != nil && fi.widths != nil && hexRaw != "" {
		// Non-CID font with widths
		w := stringWidth(hexRaw, fi)
		return w * p.ts.tfSize / 1000.0
	}
	// Fallback: estimate based on average character width
	return float64(charCount) * p.ts.tfSize * 0.5
}

// ── Parsing ───────────────────────────────────────────────────────────────────

func (p *streamParser) parse(stream []byte, streamIdx int) {
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
			p.blockTm = Identity()
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
				p.ts.tfSize = parseFloat(operands[len(operands)-1].value)
				if p.fonts != nil {
					p.gs.curFont = p.fonts[p.gs.fontKey]
				}
			}

		case "Tc": // character spacing
			if len(operands) >= 1 {
				p.ts.charSpace = parseFloat(operands[0].value)
			}

		case "Tw": // word spacing
			if len(operands) >= 1 {
				p.ts.wordSpace = parseFloat(operands[0].value)
			}

		case "TL": // leading
			if len(operands) >= 1 {
				p.ts.leading = parseFloat(operands[0].value)
			}

		case "Tz": // horizontal scaling
			if len(operands) >= 1 {
				p.ts.hScale = parseFloat(operands[0].value)
				if p.ts.hScale == 0 {
					p.ts.hScale = 100
				}
			}

		case "Ts": // text rise
			if len(operands) >= 1 {
				p.ts.rise = parseFloat(operands[0].value)
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
				// Record the first Tm in this BT block for reconstruction
				if p.inBT && len(p.spans) == p.blockSpanStart {
					p.blockTm = p.ts.tm
				}
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
				p.ts.leading = -ty
				t := Translate(tx, ty)
				p.ts.tlm = t.Multiply(p.ts.tlm)
				p.ts.tm = p.ts.tlm
			}

		case "T*":
			t := Translate(0, -p.ts.leading)
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
			t := Translate(0, -p.ts.leading)
			p.ts.tlm = t.Multiply(p.ts.tlm)
			p.ts.tm = p.ts.tlm
			if p.inBT && len(operands) >= 1 {
				p.showString(operands[len(operands)-1], streamIdx)
			}

		case `"`:
			// Set word spacing, char spacing, move to next line, show string
			if p.inBT && len(operands) >= 3 {
				p.ts.wordSpace = parseFloat(operands[len(operands)-3].value)
				p.ts.charSpace = parseFloat(operands[len(operands)-2].value)
				t := Translate(0, -p.ts.leading)
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

	// Advance text position and compute width
	advance := p.glyphAdvance(hexRaw, len([]rune(text)))
	p.advanceTx(advance)

	// Width in page space: advance is in text space (uses tfSize),
	// must apply text matrix scale AND CTM scale to get page points.
	tmScaleX := p.ts.tm.ScaleX()
	if tmScaleX <= 0 {
		tmScaleX = 1
	}
	ctmScaleX := p.gs.ctm.ScaleX()
	if ctmScaleX <= 0 {
		ctmScaleX = 1
	}
	pageWidth := advance * (p.ts.hScale / 100.0) * tmScaleX * ctmScaleX

	p.spans = append(p.spans, TextSpan{
		Text:     text,
		X:        px,
		Y:        py,
		Width:    pageWidth,
		Rotation: rot,
		FontName: p.gs.fontKey,
		FontSize: fsize,
		PageNum:  p.pageNum,
		StreamIndex: streamIdx,
		OpStart:  tok.start,
		OpEnd:    tok.end,
		// Block fields (BlockEnd filled in at ET)
		BlockStart: p.btStart,
		TfSize:    p.ts.tfSize,
		TmA: p.blockTm.A, TmB: p.blockTm.B,
		TmC: p.blockTm.C, TmD: p.blockTm.D,
		TmE: p.blockTm.E, TmF: p.blockTm.F,
	})
}

// showTJArray handles a TJ array of strings and kerning values.
func (p *streamParser) showTJArray(operands []token, streamIdx int) {
	var sb strings.Builder
	firstStart, lastEnd := -1, -1

	px, py := p.textOrigin()
	rot := p.textRotation()
	fsize := p.effectiveSize()

	// Track starting text matrix E to compute total advance
	startTmE := p.ts.tm.E

	for _, op := range operands {
		switch op.kind {
		case tokString, tokHexString:
			text, hexRaw := p.decodeToken(op)
			sb.WriteString(text)
			if firstStart == -1 {
				firstStart = op.start
			}
			lastEnd = op.end

			// Advance text position by glyph width
			advance := p.glyphAdvance(hexRaw, len([]rune(text)))
			p.advanceTx(advance)

		case tokNumber:
			// TJ displacement: positive = move left, negative = move right
			// Large displacements indicate word breaks
			kern := parseFloat(op.value)
			if kern < -200 || kern > 200 {
				sb.WriteByte(' ')
			}
			// TJ displacement in thousandths of a unit of text space
			displacement := -kern / 1000.0 * p.ts.tfSize
			p.advanceTx(displacement)
		}
	}

	fullText := sb.String()
	if fullText != "" && firstStart >= 0 {
		// Total text-space advance
		totalAdvance := p.ts.tm.E - startTmE

		// Width in page space: totalAdvance is in text space,
		// apply text matrix scale AND CTM scale
		tmScaleX := p.ts.tm.ScaleX()
		if tmScaleX <= 0 {
			tmScaleX = 1
		}
		ctmScaleX := p.gs.ctm.ScaleX()
		if ctmScaleX <= 0 {
			ctmScaleX = 1
		}
		pageWidth := totalAdvance * tmScaleX * ctmScaleX
		if pageWidth < 0 {
			pageWidth = -pageWidth
		}

		p.spans = append(p.spans, TextSpan{
			Text:        fullText,
			X:           px,
			Y:           py,
			Width:       pageWidth,
			Rotation:    rot,
			FontName:    p.gs.fontKey,
			FontSize:    fsize,
			PageNum:     p.pageNum,
			StreamIndex: streamIdx,
			OpStart:     firstStart,
			OpEnd:       lastEnd,
			BlockStart:  p.btStart,
			TfSize:      p.ts.tfSize,
			TmA: p.blockTm.A, TmB: p.blockTm.B,
			TmC: p.blockTm.C, TmD: p.blockTm.D,
			TmE: p.blockTm.E, TmF: p.blockTm.F,
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

	// Standard encoding for non-CID fonts
	if fi != nil && !fi.isCID && fi.encoding != nil {
		var sb strings.Builder
		for _, b := range []byte(tok.value) {
			r := fi.encoding[b]
			if r == 0 {
				r = rune(b)
			}
			sb.WriteRune(r)
		}
		return sb.String(), ""
	}

	return decodeString(tok.value, tokString), ""
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

	// Get the Form XObject's own font resources (if any)
	formFonts := p.fonts // inherit page fonts
	resObj, found := xobjDict.Find("Resources")
	if found {
		resDict, err := p.ctx.DereferenceDict(resObj)
		if err == nil && resDict != nil {
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

	// Save graphics state, apply form matrix, parse recursively, restore
	savedGS := p.gs
	savedTS := p.ts
	savedBT := p.inBT
	savedFonts := p.fonts

	p.gs.ctm = formMatrix.Multiply(p.gs.ctm)
	p.fonts = formFonts
	p.inBT = false
	p.depth++

	p.parse(xobjDict.Content, 0)

	p.depth--
	p.gs = savedGS
	p.ts = savedTS
	p.inBT = savedBT
	p.fonts = savedFonts
}

func (p *streamParser) lookupXObject(name string) types.Object {
	if p.ctx == nil {
		return nil
	}

	pageDict, _, _, err := p.ctx.PageDict(p.pageNum, false)
	if err != nil {
		return nil
	}

	resDict, err := resourceDict(p.ctx, pageDict)
	if err != nil || resDict == nil {
		return nil
	}

	xobjObj, found := resDict.Find("XObject")
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

// mergeAdjacentSpans combines spans on the same line that are close together.
// This turns per-character spans into readable word/sentence spans.
func mergeAdjacentSpans(spans []TextSpan) []TextSpan {
	if len(spans) == 0 {
		return nil
	}

	var merged []TextSpan
	current := spans[0]

	for i := 1; i < len(spans); i++ {
		next := spans[i]

		sameLine := math.Abs(current.Y-next.Y) < current.FontSize*0.4
		sameFont := current.FontName == next.FontName
		sameSize := math.Abs(current.FontSize-next.FontSize) < 1.0
		sameRot := math.Abs(current.Rotation-next.Rotation) < 1.0

		// Calculate where the current span visually ends
		var currentEndX float64
		if current.Width > 0 {
			currentEndX = current.X + current.Width
		} else {
			// Fallback estimate
			currentEndX = current.X + float64(len([]rune(current.Text)))*current.FontSize*0.5
		}

		gap := next.X - currentEndX

		// A typical space character is roughly 0.25× font size.
		// We consider a gap to be "close enough to merge" if it's less than
		// about 3 character widths. We insert a space if the gap is larger
		// than ~0.15× font size (roughly half a space width — conservative
		// to avoid missing spaces).
		spaceThreshold := current.FontSize * 0.15
		maxGap := current.FontSize * 2.0
		minGap := -current.FontSize * 0.5 // allow slight overlap

		closeEnough := gap < maxGap && gap > minGap

		// Also allow merging for same-line rightward spans within a
		// reasonable range (handles cases where width estimation is off)
		wideLineRange := sameLine && next.X > current.X && (next.X-current.X) < current.FontSize*40

		canMerge := sameFont && sameSize && sameRot && sameLine && (closeEnough || wideLineRange)

		if canMerge {
			needsSpace := gap > spaceThreshold

			if needsSpace {
				current.Text += " " + next.Text
			} else {
				current.Text += next.Text
			}
			// Extend the byte range
			if next.OpEnd > current.OpEnd {
				current.OpEnd = next.OpEnd
			}
			// Update width to cover from current.X to the end of next span
			if next.Width > 0 {
				current.Width = (next.X + next.Width) - current.X
			} else if current.Width > 0 {
				// At minimum extend to the start of next span + its estimated width
				nextEndX := next.X + float64(len([]rune(next.Text)))*next.FontSize*0.5
				current.Width = nextEndX - current.X
			}
		} else {
			merged = append(merged, current)
			current = next
		}
	}
	merged = append(merged, current)

	return merged
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
