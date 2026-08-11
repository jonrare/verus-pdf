# PDF specification reference

VerusPDF parses and rewrites PDF byte streams directly in `backend/edit`, so
most of that code is a direct implementation of clauses in the PDF
specification. This document says which specification, how to get it, and which
clause each part of the code implements.

## Which specification

| Standard | Covers | Cost | Use in this repo |
|---|---|---|---|
| **ISO 32000-1:2008** (PDF 1.7) | Syntax, graphics, text, fonts, forms | Free from Adobe | **Primary reference.** Cite this by default. |
| **ISO 32000-2:2020** (PDF 2.0) | Same, plus AES-256, revised encryption | Free from the PDF Association | Cite only where 2.0 diverges. |

Both are freely downloadable, which is deliberate — a citation nobody can read
is not worth writing:

- ISO 32000-1:2008 — <https://opensource.adobe.com/dc-acrobat-sdk-docs/standards/pdfstandards/pdf/PDF32000_2008.pdf>
- ISO 32000-2:2020 — <https://pdfa.org/sponsored-standards/> (free registration)

### PDF 2.0 is not a superset of 1.7

This trips people up. ISO 32000-2 **removed** material rather than only adding
to it, so it cannot serve as the sole reference for reading real-world files —
the overwhelming majority of PDFs in the wild are 1.4–1.7.

Things 2.0 dropped or deprecated that this codebase still has to handle:

- **XFA forms** are deprecated. `backend/forms` specifically handles
  XFA-converted documents (the IRS W-9 is the canonical example), where widget
  annotations carry `/FT` directly and never appear in `/AcroForm /Fields`.
- **RC4 security handlers** (revisions 2 and 3) are removed. Files using them
  still exist and still need decrypting.
- **The standard 14 fonts** are no longer guaranteed to be available without
  embedding, so 1.7-era files rely on built-in metrics that 2.0 does not
  promise.

Things that only exist in 2.0, which this codebase does use:

- **AES-256 encryption (revision 6).** `backend/security` hardcodes
  `EncryptKeyLength = 256`. This is *not* in ISO 32000-1:2008 at all — it
  arrived via Adobe Extension Level 3 and was standardised in ISO 32000-2.
  Cite 2.0 there.

Everything this codebase leans on most heavily — §7 (syntax), §8 (graphics),
§9 (text and fonts) — is materially unchanged between the two, and the clause
numbering is stable, so a §9.4.3 citation is valid against either document.

## Citation convention

In Go source, cite as:

```go
// Spec: ISO 32000-1:2008, §9.4.3 (text-showing operators)
```

Short in-line references within a file that already names the standard in its
header may use the bare clause: `// §9.4.3`. Where 2.0 differs, say so
explicitly: `// ISO 32000-2:2020, §7.6.4.3 — AES-256 is 2.0 only`.

## Module index

### `backend/edit` — content stream decoding and rewriting

| File | Implements | Clauses |
|---|---|---|
| `contentstream.go` | Tokeniser, operator dispatch, text rendering pipeline | §7.2 (lexical), §7.3.4 (strings), §8.2 (content streams), §9.4 (text objects) |
| `contentstream.go` | Graphics state stack, `q`/`Q`/`cm` | §8.3.3 (CTM), §8.4.2 (state stack) |
| `contentstream.go` | Text state: `Tf` `Tc` `Tw` `Tz` `TL` `Tr` `Ts` | §9.3 |
| `contentstream.go` | Positioning: `Tm` `Td` `TD` `T*` | §9.4.2 |
| `contentstream.go` | Showing: `Tj` `TJ` `'` `"` | §9.4.3 |
| `contentstream.go` | Form XObject traversal via `Do` | §8.10.1 |
| `matrix.go` | Affine transforms, row-vector convention | §8.3.3 |
| `encoding.go` | WinAnsi / MacRoman / Standard encodings, `/Differences` | §9.6.6, Annex D |
| `fontresources.go` | `/ToUnicode` CMaps, simple and CID font widths | §9.7.5 (ToUnicode), §9.6.2.1 (`/Widths`), §9.7.4.3 (`/W`) |
| `streameditor.go` | In-place string replacement in content streams | §7.3.4.2 (literal strings), §7.3.4.3 (hex strings) |
| `mergededitor.go` | BT/ET block reconstruction with recomputed advances | §9.4.4 (text space details) |

### Other packages

| File | Implements | Clauses |
|---|---|---|
| `backend/forms/forms.go` | AcroForm field tree, widget annotations, `/DA`, field flags | §12.7.3 (field dictionaries), §12.7.4 (field types), §12.5.6.19 (widget annotations) |
| `backend/security/security.go` | Encryption, permission bitmask | §7.6 (encryption), Table 22 (permissions); **ISO 32000-2:2020 §7.6.4.3** for AES-256 |
| `backend/bookmarks/bookmarks.go` | Document outline hierarchy | §12.3.3 |
| `backend/optimize/optimize.go` | Object streams, cross-reference streams | §7.5.7, §7.5.8 |
| `backend/viewer/viewer.go` | Trailer `/Encrypt` detection, document info dictionary | §7.5.5 (trailer), §14.3.3 (document info) |
| `backend/internal/pdftest` | Test-only fixture builder: spec-valid PDFs with real Helvetica metrics | §9.6.2.1 (`/Widths`), Table 122 (font descriptors) |

## Testing

`backend/internal/pdftest` builds genuine, parseable PDFs on disk rather than
checked-in fixture blobs, so tests exercise the same pdfcpu read path the
application uses. Its `Pages` helper writes content streams verbatim, which
means a test can assert reported byte offsets against the exact bytes it
supplied.

The fixture font is deliberately spec-complete — real `/Widths`, a real font
descriptor — so it passes strict validation and the decoder resolves true glyph
advances. Tests that need the *unknown*-width code paths should call
`mergeAdjacentSpans` directly rather than going through a fixture.

## Known deviations

Places where the implementation knowingly departs from the specification.
These are real gaps, not design choices — treat them as a work list.

| Area | Deviation | Clause |
|---|---|---|
| Block rewriting | `EditMergedSpans` replaces a whole `BT`…`ET` block with only the edited span's glyphs, discarding any other text in that block along with its `Tc`/`Tw`/`Tz`/`Tr` and colour operators. | §9.4 |
| Font metrics | The standard 14 fonts carry no `/Widths` array, and no built-in metrics are compiled in, so their glyph advances are estimated at 0.5 em per character. See below — this is the highest-value gap. | §9.6.2.2 |
| Simple font encoding | Replacement text is narrowed to one byte per rune rather than reverse-mapped through the font's encoding table, so characters that exist in WinAnsi above U+00FF (smart quotes, en dash, €) cannot be typed. | §9.6.6 |

### Fixed, with regression tests

Recorded so the tests that pin them are easy to find.

| Area | Was | Clause |
|---|---|---|
| Glyph widths | `decodeToken` returned an empty raw string for simple fonts, so `stringWidth` never ran and every `/Widths` array was ignored — all widths silently fell back to the 0.5 em estimate. `TestExtractText_WidthUsesFontMetrics` | §9.2.4 |
| String encoding | Replacement text was written as Go's UTF-8 bytes into literal strings the font decodes as WinAnsi, so `é` became `Ã©`. `TestEscapePDFString_NonASCIIIsSingleByte` | §9.6.6 |
| Truncation | Replacement text was escaped and *then* cut to length, so a cut could land between a backslash and the character it escapes. `TestBuildLiteralReplacement_TruncationNeverSplitsEscape` | §7.3.4.2 |
| Literal strings | The tokeniser emitted a newline for a backslash-newline line continuation. `TestTokenise_LineContinuationEmitsNothing` | §7.3.4.2 |
| Button fields | `/V` for checkbox and radio fields was written as a string, leaving widgets rendering as off. `TestFieldValue_ButtonsGetNameObjects` | §12.7.4.2 |
| Text strings | Hex-literal field values were returned as raw hex, so UTF-16BE values surfaced as `FEFF…`. | §7.9.2.2 |
| Graphics state | `q`/`Q` restored the font *name* but not the text state parameters (`Tf` size, `Tc`, `Tw`, `TL`, `Tz`, `Ts`), which §8.4.1 makes part of the graphics state — so a `Q` could leave the font and its size disagreeing. They now live in `graphicsState`; only the matrices, which `BT` resets, remain in `textState`. `TestGraphicsState_QRestoresTextStateParameters` | §8.4.1 |
| Text advance | `advanceTx` added the displacement straight to `Tm.E`, correct only for an axis-aligned unscaled matrix, and ignored `Tc`/`Tw` entirely. Now `Tm' = Translate(tx,0) × Tm` with `tx = ((w0 − Tj/1000) × Tfs + Tc + Tw) × Th`. `TestAdvance_FollowsRotatedTextMatrix`, `TestTc_WidensTheAdvance`, `TestTw_AppliesToSpacesOnly` | §9.4.4 |
| Horizontal scaling | `Tz` was applied twice to the width reported by `Tj` and once by `TJ`. Width is now measured as the distance the origin actually travelled in page space, so `Tz`, the text matrix and the CTM each apply exactly once. `TestTz_AppliedOnceAndConsistentlyAcrossTjAndTJ` | §9.3.4 |
| Form XObjects | Nested XObject names resolved against the *page's* `/Resources` rather than the enclosing form's, so a form invoked from inside another form silently vanished. The parser now carries the resource dict in scope. `TestFormXObject_NestedResolvesAgainstFormResources` | §8.10.1 |
| Stream addressing | Byte offsets were measured against the *concatenation* of a page's `/Contents` array (via `pdfcpu.ExtractPageContent`) but applied to individual stream objects, so any page with a `/Contents` array spliced at the wrong location. Spans inside a Form XObject were worse: offsets into the form's own stream, tagged `StreamIndex 0`. Extraction now reads the parts individually and tags each span with its real stream; anything that cannot be addressed is marked `notEditable` and both editors refuse it. `TestExtractText_SplitContentsReportsPerStreamOffsets`, `TestReplaceSpanText_EditsCorrectStreamOfSplitContents`, `TestExtractText_FormXObjectSpansAreNotEditable` | §7.8.2, §8.10.1 |

### The standard-14 metrics gap

This one deserves calling out because it silently degrades several features at
once. Helvetica, Times, Courier, Symbol and ZapfDingbats may legally omit
`/Widths` (§9.6.2.2), and viewers are expected to know their metrics. This
codebase does not, so for those fonts — a large share of real documents —
`glyphAdvance` falls back to `charCount × fontSize × 0.5`.

Downstream, that estimate is why:

- overlay boxes in edit mode sit slightly wrong,
- `mergeAdjacentSpans` cannot recover word spaces (see below),
- rewritten `BT`/`ET` blocks reposition glyphs imprecisely.

Compiling in the AFM tables for the standard 14 would close all three at once.
`backend/internal/pdftest` already carries the Helvetica table as a starting
point.

**Word spaces are unrecoverable without real widths.** Not merely hard —
undecidable from geometry. With per-character spans in 12pt Helvetica,
`"Hi there"` advances 6.000 points from `i` to `t` *including the space*, but
6.672 from `h` to `e` with no space at all. The step containing a space is the
smaller of the two, because a narrow glyph plus a space is narrower than a wide
glyph alone. No threshold separates them. `mergeAdjacentSpans` therefore errs
toward keeping words intact and only claims a space when an advance is too wide
to be any single glyph; `TestMerge_UnknownWidths_SpaceRecoveryIsNotPossible`
documents the limit.

## Practical notes

**Coordinate systems.** PDF puts the origin at the bottom-left with Y
increasing upward; canvas puts it at the top-left with Y increasing downward.
`matrix.go` uses PDF's row-vector convention — points are transformed on the
*left*, `[x' y' 1] = [x y 1] × M` — which is the transpose of the column-vector
convention most graphics literature uses. Getting this backwards silently
produces transposed transforms.

**Text space vs. glyph space.** Glyph widths in `/Widths` and `/W` are in
1/1000 of a text-space unit (§9.2.4). `Td` displacements are in *unscaled*
text space, so an advance is `width / 1000 × fontSize` — the font size
multiplies in, it does not cancel.

**`TJ` displacements** are in thousandths of a text-space unit and are
*subtracted* from the position: a positive number moves left (§9.4.3).
