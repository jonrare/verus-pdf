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

## Known deviations

Places where the implementation knowingly departs from the specification.
These are real gaps, not design choices — treat them as a work list.

| Area | Deviation | Clause |
|---|---|---|
| Graphics state | `q`/`Q` save and restore the font *name* but not the text state (`Tf` size, `Tc`, `Tw`, `TL`, `Tz`, `Ts`). The spec makes all of these part of the graphics state, so after a `Q` the font and its size can disagree. | §8.4.1 |
| Text advance | `advanceTx` adds directly to `Tm.E`, ignoring the matrix's rotation and skew, and ignores `Tc`/`Tw`. Rotated or letter-spaced text drifts. | §9.4.4 |
| Horizontal scaling | `Tz` is applied twice to the reported width in `showString` but once in `showTJArray`. | §9.3.4 |
| Form XObjects | Nested XObject names are resolved against the *page's* `/Resources` rather than the enclosing form's, so nested forms lose their content. Fonts are inherited correctly. | §8.10.1 |
| Literal strings | The tokeniser emits a newline for a backslash-newline line continuation; it should emit nothing. | §7.3.4.2 |
| Content streams | Byte offsets are computed against the *concatenation* of a page's `/Contents` array but applied to individual streams, so pages with multiple content streams splice at the wrong location. | §7.8.2 |
| String encoding | Replacement text is written as UTF-8 bytes into literal strings that the font decodes as WinAnsi/Standard, so non-ASCII characters become mojibake. | §7.9.2.2 |
| Button fields | `/V` for checkbox and radio fields is written as a string; the spec requires a name object. | §12.7.4.2 |
| Text strings | `HexLiteral` field values are returned as raw hex rather than decoded, so UTF-16BE values surface as `FEFF...`. | §7.9.2.2 |

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
