package edit

// Standard PDF font encoding tables.
//
// PDF fonts can use several predefined encodings that map byte values
// to Unicode characters. Non-CID fonts (Type1, TrueType) commonly use:
//   - WinAnsiEncoding   — Windows code page 1252 (most common)
//   - MacRomanEncoding   — Mac OS Roman
//   - StandardEncoding   — Adobe Standard Encoding (Type1 default)
//
// Fonts may also specify a /Differences array that overrides individual
// code points with named glyphs.
//
// Spec: PDF 32000-1:2008 §9.6.1, §9.6.6, Annex D

// encodingTable maps byte values (0-255) to Unicode code points.
type encodingTable [256]rune

// ────────────────────────────────────────────────────────────────────────────
// WinAnsiEncoding (§D.1)
//
// Identical to Windows code page 1252. Bytes 0x00-0x7F match ASCII.
// Bytes 0xA0-0xFF match ISO 8859-1 (Latin-1).
// Bytes 0x80-0x9F differ from Latin-1 and map to specific Unicode chars.
// ────────────────────────────────────────────────────────────────────────────

var winAnsiEncoding encodingTable

func init() {
	// 0x00-0x7F: standard ASCII / control characters
	for i := 0; i < 128; i++ {
		winAnsiEncoding[i] = rune(i)
	}

	// 0x80-0x9F: WinAnsi-specific mappings (differs from Latin-1)
	winAnsiEncoding[0x80] = 0x20AC // €  Euro sign
	winAnsiEncoding[0x81] = 0xFFFD //    Undefined → replacement char
	winAnsiEncoding[0x82] = 0x201A // ‚  Single low-9 quotation mark
	winAnsiEncoding[0x83] = 0x0192 // ƒ  Latin small letter f with hook
	winAnsiEncoding[0x84] = 0x201E // „  Double low-9 quotation mark
	winAnsiEncoding[0x85] = 0x2026 // …  Horizontal ellipsis
	winAnsiEncoding[0x86] = 0x2020 // †  Dagger
	winAnsiEncoding[0x87] = 0x2021 // ‡  Double dagger
	winAnsiEncoding[0x88] = 0x02C6 // ˆ  Modifier letter circumflex accent
	winAnsiEncoding[0x89] = 0x2030 // ‰  Per mille sign
	winAnsiEncoding[0x8A] = 0x0160 // Š  Latin capital letter S with caron
	winAnsiEncoding[0x8B] = 0x2039 // ‹  Single left-pointing angle quote
	winAnsiEncoding[0x8C] = 0x0152 // Œ  Latin capital ligature OE
	winAnsiEncoding[0x8D] = 0xFFFD //    Undefined
	winAnsiEncoding[0x8E] = 0x017D // Ž  Latin capital letter Z with caron
	winAnsiEncoding[0x8F] = 0xFFFD //    Undefined
	winAnsiEncoding[0x90] = 0xFFFD //    Undefined
	winAnsiEncoding[0x91] = 0x2018 // '  Left single quotation mark
	winAnsiEncoding[0x92] = 0x2019 // '  Right single quotation mark
	winAnsiEncoding[0x93] = 0x201C // "  Left double quotation mark
	winAnsiEncoding[0x94] = 0x201D // "  Right double quotation mark
	winAnsiEncoding[0x95] = 0x2022 // •  Bullet
	winAnsiEncoding[0x96] = 0x2013 // –  En dash
	winAnsiEncoding[0x97] = 0x2014 // —  Em dash
	winAnsiEncoding[0x98] = 0x02DC // ˜  Small tilde
	winAnsiEncoding[0x99] = 0x2122 // ™  Trade mark sign
	winAnsiEncoding[0x9A] = 0x0161 // š  Latin small letter s with caron
	winAnsiEncoding[0x9B] = 0x203A // ›  Single right-pointing angle quote
	winAnsiEncoding[0x9C] = 0x0153 // œ  Latin small ligature oe
	winAnsiEncoding[0x9D] = 0xFFFD //    Undefined
	winAnsiEncoding[0x9E] = 0x017E // ž  Latin small letter z with caron
	winAnsiEncoding[0x9F] = 0x0178 // Ÿ  Latin capital letter Y with diaeresis

	// 0xA0-0xFF: same as ISO 8859-1 / Unicode Latin-1 Supplement
	for i := 0xA0; i <= 0xFF; i++ {
		winAnsiEncoding[i] = rune(i)
	}

	// 0xAD is soft hyphen in Latin-1, but in WinAnsiEncoding it's undefined
	// Some implementations map it to U+00AD, we'll keep it as Latin-1 for compat.
}

// ────────────────────────────────────────────────────────────────────────────
// MacRomanEncoding (§D.1)
// ────────────────────────────────────────────────────────────────────────────

var macRomanEncoding encodingTable

func init() {
	// 0x00-0x7F: standard ASCII
	for i := 0; i < 128; i++ {
		macRomanEncoding[i] = rune(i)
	}

	// 0x80-0xFF: Mac OS Roman specific
	macHigh := [128]rune{
		0x00C4, 0x00C5, 0x00C7, 0x00C9, 0x00D1, 0x00D6, 0x00DC, 0x00E1, // 80-87
		0x00E0, 0x00E2, 0x00E4, 0x00E3, 0x00E5, 0x00E7, 0x00E9, 0x00E8, // 88-8F
		0x00EA, 0x00EB, 0x00ED, 0x00EC, 0x00EE, 0x00EF, 0x00F1, 0x00F3, // 90-97
		0x00F2, 0x00F4, 0x00F6, 0x00F5, 0x00FA, 0x00F9, 0x00FB, 0x00FC, // 98-9F
		0x2020, 0x00B0, 0x00A2, 0x00A3, 0x00A7, 0x2022, 0x00B6, 0x00DF, // A0-A7
		0x00AE, 0x00A9, 0x2122, 0x00B4, 0x00A8, 0x2260, 0x00C6, 0x00D8, // A8-AF
		0x221E, 0x00B1, 0x2264, 0x2265, 0x00A5, 0x00B5, 0x2202, 0x2211, // B0-B7
		0x220F, 0x03C0, 0x222B, 0x00AA, 0x00BA, 0x03A9, 0x00E6, 0x00F8, // B8-BF
		0x00BF, 0x00A1, 0x00AC, 0x221A, 0x0192, 0x2248, 0x2206, 0x00AB, // C0-C7
		0x00BB, 0x2026, 0x00A0, 0x00C0, 0x00C3, 0x00D5, 0x0152, 0x0153, // C8-CF
		0x2013, 0x2014, 0x201C, 0x201D, 0x2018, 0x2019, 0x00F7, 0x25CA, // D0-D7
		0x00FF, 0x0178, 0x2044, 0x20AC, 0x2039, 0x203A, 0xFB01, 0xFB02, // D8-DF
		0x2021, 0x00B7, 0x201A, 0x201E, 0x2030, 0x00C2, 0x00CA, 0x00C1, // E0-E7
		0x00CB, 0x00C8, 0x00CD, 0x00CE, 0x00CF, 0x00CC, 0x00D3, 0x00D4, // E8-EF
		0xF8FF, 0x00D2, 0x00DA, 0x00DB, 0x00D9, 0x0131, 0x02C6, 0x02DC, // F0-F7
		0x00AF, 0x02D8, 0x02D9, 0x02DA, 0x00B8, 0x02DD, 0x02DB, 0x02C7, // F8-FF
	}
	for i := 0; i < 128; i++ {
		macRomanEncoding[0x80+i] = macHigh[i]
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Adobe Standard Encoding (§D.1)
//
// Default for Type1 fonts that don't specify an encoding.
// ────────────────────────────────────────────────────────────────────────────

var standardEncoding encodingTable

func init() {
	// Initialize with zeros (undefined)
	// Then set the defined code points.
	// StandardEncoding only defines a subset of 0-255.

	// ASCII subset (most printable chars are in the same position)
	for i := 0x20; i <= 0x7E; i++ {
		standardEncoding[i] = rune(i)
	}

	// Overrides and additions specific to StandardEncoding
	standardEncoding[0x27] = 0x2019 // quoteright (not ASCII apostrophe)
	standardEncoding[0x60] = 0x2018 // quoteleft (not ASCII grave)

	// Upper range (selected assignments from the spec)
	standardEncoding[0xA1] = 0x00A1 // exclamdown
	standardEncoding[0xA2] = 0x00A2 // cent
	standardEncoding[0xA3] = 0x00A3 // sterling
	standardEncoding[0xA4] = 0x2044 // fraction
	standardEncoding[0xA5] = 0x00A5 // yen
	standardEncoding[0xA6] = 0x0192 // florin
	standardEncoding[0xA7] = 0x00A7 // section
	standardEncoding[0xA8] = 0x00A4 // currency
	standardEncoding[0xA9] = 0x0027 // quotesingle
	standardEncoding[0xAA] = 0x201C // quotedblleft
	standardEncoding[0xAB] = 0x00AB // guillemotleft
	standardEncoding[0xAC] = 0x2039 // guilsinglleft
	standardEncoding[0xAD] = 0x203A // guilsinglright
	standardEncoding[0xAE] = 0xFB01 // fi
	standardEncoding[0xAF] = 0xFB02 // fl
	standardEncoding[0xB1] = 0x2013 // endash
	standardEncoding[0xB2] = 0x2020 // dagger
	standardEncoding[0xB3] = 0x2021 // daggerdbl
	standardEncoding[0xB4] = 0x00B7 // periodcentered
	standardEncoding[0xB6] = 0x00B6 // paragraph
	standardEncoding[0xB7] = 0x2022 // bullet
	standardEncoding[0xB8] = 0x201A // quotesinglbase
	standardEncoding[0xB9] = 0x201E // quotedblbase
	standardEncoding[0xBA] = 0x201D // quotedblright
	standardEncoding[0xBB] = 0x00BB // guillemotright
	standardEncoding[0xBC] = 0x2026 // ellipsis
	standardEncoding[0xBD] = 0x2030 // perthousand
	standardEncoding[0xC1] = 0x0060 // grave
	standardEncoding[0xC2] = 0x00B4 // acute
	standardEncoding[0xC3] = 0x02C6 // circumflex
	standardEncoding[0xC4] = 0x02DC // tilde
	standardEncoding[0xC5] = 0x00AF // macron
	standardEncoding[0xC6] = 0x02D8 // breve
	standardEncoding[0xC7] = 0x02D9 // dotaccent
	standardEncoding[0xC8] = 0x00A8 // dieresis
	standardEncoding[0xCA] = 0x02DA // ring
	standardEncoding[0xCB] = 0x00B8 // cedilla
	standardEncoding[0xCD] = 0x02DD // hungarumlaut
	standardEncoding[0xCE] = 0x02DB // ogonek
	standardEncoding[0xCF] = 0x02C7 // caron
	standardEncoding[0xD0] = 0x2014 // emdash
	standardEncoding[0xE1] = 0x00C6 // AE
	standardEncoding[0xE3] = 0x00AA // ordfeminine
	standardEncoding[0xE8] = 0x0141 // Lslash
	standardEncoding[0xE9] = 0x00D8 // Oslash
	standardEncoding[0xEA] = 0x0152 // OE
	standardEncoding[0xEB] = 0x00BA // ordmasculine
	standardEncoding[0xF1] = 0x00E6 // ae
	standardEncoding[0xF5] = 0x0131 // dotlessi
	standardEncoding[0xF8] = 0x0142 // lslash
	standardEncoding[0xF9] = 0x00F8 // oslash
	standardEncoding[0xFA] = 0x0153 // oe
	standardEncoding[0xFB] = 0x00DF // germandbls
}

// ────────────────────────────────────────────────────────────────────────────
// Adobe glyph name → Unicode mapping (common glyphs only)
//
// Used to resolve /Differences arrays where glyph names are used instead
// of Unicode code points.
//
// Full list: https://github.com/adobe-type-tools/agl-aglfn
// We include the ~300 most common names found in real-world PDFs.
// ────────────────────────────────────────────────────────────────────────────

var adobeGlyphNames = map[string]rune{
	// Basic Latin
	"space": ' ', "exclam": '!', "quotedbl": '"', "numbersign": '#',
	"dollar": '$', "percent": '%', "ampersand": '&', "quotesingle": '\'',
	"parenleft": '(', "parenright": ')', "asterisk": '*', "plus": '+',
	"comma": ',', "hyphen": '-', "period": '.', "slash": '/',
	"zero": '0', "one": '1', "two": '2', "three": '3',
	"four": '4', "five": '5', "six": '6', "seven": '7',
	"eight": '8', "nine": '9', "colon": ':', "semicolon": ';',
	"less": '<', "equal": '=', "greater": '>', "question": '?',
	"at": '@',
	"A": 'A', "B": 'B', "C": 'C', "D": 'D', "E": 'E', "F": 'F',
	"G": 'G', "H": 'H', "I": 'I', "J": 'J', "K": 'K', "L": 'L',
	"M": 'M', "N": 'N', "O": 'O', "P": 'P', "Q": 'Q', "R": 'R',
	"S": 'S', "T": 'T', "U": 'U', "V": 'V', "W": 'W', "X": 'X',
	"Y": 'Y', "Z": 'Z',
	"bracketleft": '[', "backslash": '\\', "bracketright": ']',
	"asciicircum": '^', "underscore": '_', "grave": '`',
	"a": 'a', "b": 'b', "c": 'c', "d": 'd', "e": 'e', "f": 'f',
	"g": 'g', "h": 'h', "i": 'i', "j": 'j', "k": 'k', "l": 'l',
	"m": 'm', "n": 'n', "o": 'o', "p": 'p', "q": 'q', "r": 'r',
	"s": 's', "t": 't', "u": 'u', "v": 'v', "w": 'w', "x": 'x',
	"y": 'y', "z": 'z',
	"braceleft": '{', "bar": '|', "braceright": '}', "asciitilde": '~',

	// Latin-1 Supplement
	"exclamdown": 0x00A1, "cent": 0x00A2, "sterling": 0x00A3,
	"currency": 0x00A4, "yen": 0x00A5, "brokenbar": 0x00A6,
	"section": 0x00A7, "dieresis": 0x00A8, "copyright": 0x00A9,
	"ordfeminine": 0x00AA, "guillemotleft": 0x00AB,
	"logicalnot": 0x00AC, "registered": 0x00AE,
	"macron": 0x00AF, "degree": 0x00B0, "plusminus": 0x00B1,
	"twosuperior": 0x00B2, "threesuperior": 0x00B3,
	"acute": 0x00B4, "mu": 0x00B5, "paragraph": 0x00B6,
	"periodcentered": 0x00B7, "cedilla": 0x00B8,
	"onesuperior": 0x00B9, "ordmasculine": 0x00BA,
	"guillemotright": 0x00BB, "onequarter": 0x00BC,
	"onehalf": 0x00BD, "threequarters": 0x00BE,
	"questiondown": 0x00BF,
	"Agrave": 0x00C0, "Aacute": 0x00C1, "Acircumflex": 0x00C2,
	"Atilde": 0x00C3, "Adieresis": 0x00C4, "Aring": 0x00C5,
	"AE": 0x00C6, "Ccedilla": 0x00C7,
	"Egrave": 0x00C8, "Eacute": 0x00C9, "Ecircumflex": 0x00CA,
	"Edieresis": 0x00CB,
	"Igrave": 0x00CC, "Iacute": 0x00CD, "Icircumflex": 0x00CE,
	"Idieresis": 0x00CF,
	"Eth": 0x00D0, "Ntilde": 0x00D1,
	"Ograve": 0x00D2, "Oacute": 0x00D3, "Ocircumflex": 0x00D4,
	"Otilde": 0x00D5, "Odieresis": 0x00D6, "multiply": 0x00D7,
	"Oslash": 0x00D8,
	"Ugrave": 0x00D9, "Uacute": 0x00DA, "Ucircumflex": 0x00DB,
	"Udieresis": 0x00DC, "Yacute": 0x00DD, "Thorn": 0x00DE,
	"germandbls": 0x00DF,
	"agrave": 0x00E0, "aacute": 0x00E1, "acircumflex": 0x00E2,
	"atilde": 0x00E3, "adieresis": 0x00E4, "aring": 0x00E5,
	"ae": 0x00E6, "ccedilla": 0x00E7,
	"egrave": 0x00E8, "eacute": 0x00E9, "ecircumflex": 0x00EA,
	"edieresis": 0x00EB,
	"igrave": 0x00EC, "iacute": 0x00ED, "icircumflex": 0x00EE,
	"idieresis": 0x00EF,
	"eth": 0x00F0, "ntilde": 0x00F1,
	"ograve": 0x00F2, "oacute": 0x00F3, "ocircumflex": 0x00F4,
	"otilde": 0x00F5, "odieresis": 0x00F6, "divide": 0x00F7,
	"oslash": 0x00F8,
	"ugrave": 0x00F9, "uacute": 0x00FA, "ucircumflex": 0x00FB,
	"udieresis": 0x00FC, "yacute": 0x00FD, "thorn": 0x00FE,
	"ydieresis": 0x00FF,

	// Extended Latin
	"Scaron": 0x0160, "scaron": 0x0161,
	"Zcaron": 0x017D, "zcaron": 0x017E,
	"OE": 0x0152, "oe": 0x0153,
	"Ydieresis": 0x0178,
	"Lslash": 0x0141, "lslash": 0x0142,
	"dotlessi": 0x0131, "florin": 0x0192,

	// General Punctuation
	"endash": 0x2013, "emdash": 0x2014,
	"quoteleft": 0x2018, "quoteright": 0x2019,
	"quotesinglbase": 0x201A,
	"quotedblleft": 0x201C, "quotedblright": 0x201D,
	"quotedblbase": 0x201E,
	"dagger": 0x2020, "daggerdbl": 0x2021,
	"bullet": 0x2022, "ellipsis": 0x2026,
	"perthousand": 0x2030,
	"guilsinglleft": 0x2039, "guilsinglright": 0x203A,
	"fraction": 0x2044,
	"Euro": 0x20AC,
	"trademark": 0x2122,
	"minus": 0x2212,

	// Spacing Modifier Letters
	"circumflex": 0x02C6, "caron": 0x02C7,
	"breve": 0x02D8, "dotaccent": 0x02D9,
	"ring": 0x02DA, "ogonek": 0x02DB,
	"tilde": 0x02DC, "hungarumlaut": 0x02DD,

	// Ligatures
	"fi": 0xFB01, "fl": 0xFB02,
	"ffi": 0xFB03, "ffl": 0xFB04,

	// Mathematical
	"infinity": 0x221E, "partialdiff": 0x2202,
	"summation": 0x2211, "product": 0x220F,
	"integral": 0x222B, "radical": 0x221A,
	"approxequal": 0x2248, "notequal": 0x2260,
	"lessequal": 0x2264, "greaterequal": 0x2265,
	"lozenge": 0x25CA, "Delta": 0x2206,
	"Omega": 0x03A9, "pi": 0x03C0,

	// Miscellaneous
	"nbspace": 0x00A0, "softhyphen": 0x00AD,
	".notdef": 0xFFFD,
}

// getEncoding returns the encoding table for a named PDF encoding.
func getEncoding(name string) *encodingTable {
	switch name {
	case "WinAnsiEncoding":
		return &winAnsiEncoding
	case "MacRomanEncoding":
		return &macRomanEncoding
	case "StandardEncoding":
		return &standardEncoding
	default:
		return nil
	}
}

// resolveGlyphName returns the Unicode rune for an Adobe glyph name.
// Returns 0xFFFD if the name is not recognized.
func resolveGlyphName(name string) rune {
	if r, ok := adobeGlyphNames[name]; ok {
		return r
	}
	return 0xFFFD
}
