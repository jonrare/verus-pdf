// Package pdftest builds real PDF files on disk for tests.
//
// Everything in here produces a genuine, parseable PDF rather than a fixture
// blob, so tests exercise the same pdfcpu read path the application uses.
// Test-only: nothing outside _test.go files should import this.
package pdftest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpupkg "github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Write serialises an xref table to a PDF file under t.TempDir() and returns
// its path.
func Write(t *testing.T, xref *model.XRefTable, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	conf := model.NewDefaultConfiguration()
	if err := api.CreatePDFFile(xref, path, conf); err != nil {
		t.Fatalf("pdftest.Write(%s): %v", name, err)
	}
	return path
}

// Form returns the path to a document containing an AcroForm with text fields,
// checkboxes, radio buttons and choice fields.
func Form(t *testing.T, name string) string {
	t.Helper()
	xref, err := pdfcpupkg.CreateFormDemoXRef()
	if err != nil {
		t.Fatalf("CreateFormDemoXRef: %v", err)
	}
	return Write(t, xref, name)
}

// TextPage builds a single-page PDF whose content stream is exactly the bytes
// given, with one Helvetica resource registered as /F1.
//
// This is the workhorse for decoder tests: the content stream is known
// byte-for-byte, so byte offsets reported by the decoder can be asserted
// against it directly.
//
// The font carries a real /Widths array and font descriptor, so it is valid
// under strict validation and the decoder can resolve true glyph advances.
// Tests that need the unknown-width code paths should drive mergeAdjacentSpans
// directly rather than going through a fixture.
func TextPage(t *testing.T, name, contentStream string) string {
	t.Helper()
	return Pages(t, name, contentStream)
}

// Pages builds a PDF with one page per content stream given.
func Pages(t *testing.T, name string, contentStreams ...string) string {
	t.Helper()

	if len(contentStreams) == 0 {
		t.Fatal("pdftest.Pages: need at least one content stream")
	}

	xref, err := pdfcpupkg.CreateXRefTableWithRootDict()
	if err != nil {
		t.Fatalf("CreateXRefTableWithRootDict: %v", err)
	}

	// Font descriptor — ISO 32000-1:2008, Table 122. Strict validation requires
	// it even for a standard-14 face. Values are Helvetica's real metrics.
	descRef, err := xref.IndRefForNewObject(types.Dict(map[string]types.Object{
		"Type":        types.Name("FontDescriptor"),
		"FontName":    types.Name("Helvetica"),
		"FontFamily":  types.StringLiteral("Helvetica"),
		"FontStretch": types.Name("Normal"),
		"FontWeight":  types.Integer(400),
		"Flags":       types.Integer(32), // nonsymbolic
		"FontBBox":    types.NewNumberArray(-166, -225, 1000, 931),
		"ItalicAngle": types.Integer(0),
		"Ascent":      types.Integer(718),
		"Descent":     types.Integer(-207),
		"CapHeight":   types.Integer(718),
		"StemV":       types.Integer(88),
	}))
	if err != nil {
		t.Fatalf("font descriptor: %v", err)
	}

	widths := make(types.Array, 0, len(HelveticaWidths))
	for _, w := range HelveticaWidths {
		widths = append(widths, types.Integer(w))
	}
	fontRef, err := xref.IndRefForNewObject(types.Dict(map[string]types.Object{
		"Type":           types.Name("Font"),
		"Subtype":        types.Name("Type1"),
		"BaseFont":       types.Name("Helvetica"),
		"Encoding":       types.Name("WinAnsiEncoding"),
		"FirstChar":      types.Integer(FirstChar),
		"FontDescriptor": *descRef,
		"LastChar":       types.Integer(FirstChar + len(HelveticaWidths) - 1),
		"Widths":         widths,
	}))
	if err != nil {
		t.Fatalf("font object: %v", err)
	}

	pagesRef, err := xref.IndRefForNewObject(types.Dict(map[string]types.Object{
		"Type": types.Name("Pages"),
	}))
	if err != nil {
		t.Fatalf("pages object: %v", err)
	}

	kids := make(types.Array, 0, len(contentStreams))
	for i, cs := range contentStreams {
		sd, err := xref.NewStreamDictForBuf([]byte(cs))
		if err != nil {
			t.Fatalf("content stream %d: %v", i, err)
		}
		if err := sd.Encode(); err != nil {
			t.Fatalf("encode content stream %d: %v", i, err)
		}
		contentRef, err := xref.IndRefForNewObject(*sd)
		if err != nil {
			t.Fatalf("content object %d: %v", i, err)
		}

		pageRef, err := xref.IndRefForNewObject(types.Dict(map[string]types.Object{
			"Type":     types.Name("Page"),
			"Parent":   *pagesRef,
			"MediaBox": types.NewNumberArray(0, 0, 612, 792),
			"Contents": *contentRef,
			"Resources": types.Dict(map[string]types.Object{
				"Font": types.Dict(map[string]types.Object{"F1": *fontRef}),
			}),
		}))
		if err != nil {
			t.Fatalf("page object %d: %v", i, err)
		}
		kids = append(kids, *pageRef)
	}

	pagesDict := types.Dict(map[string]types.Object{
		"Type":  types.Name("Pages"),
		"Count": types.Integer(len(contentStreams)),
		"Kids":  kids,
	})
	if err := xref.SetValid(*pagesRef); err != nil {
		t.Fatalf("SetValid: %v", err)
	}
	entry, ok := xref.FindTableEntry(pagesRef.ObjectNumber.Value(), pagesRef.GenerationNumber.Value())
	if !ok {
		t.Fatal("pages xref entry missing")
	}
	entry.Object = pagesDict

	rootDict, err := xref.Catalog()
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	rootDict["Pages"] = *pagesRef

	return Write(t, xref, name)
}

// ReadFile is os.ReadFile with a t.Fatal on failure.
func ReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// FirstChar is the character code the HelveticaWidths table starts at.
const FirstChar = 32

// HelveticaWidths holds the Adobe Font Metrics advance widths for Helvetica,
// character codes 32 (space) through 126 (~), in 1/1000 text-space units.
//
// Including a real /Widths array in the fixture matters for two reasons: a
// Type1 font dictionary without FirstChar/LastChar/Widths fails strict
// validation, and without it the decoder cannot resolve glyph widths and falls
// back to estimating, so the accurate-width code paths would never be tested.
var HelveticaWidths = [...]int{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, // 32-41   space ! " # $ % & ' ( )
	389, 584, 278, 333, 278, 278, 556, 556, 556, 556, // 42-51   * + , - . / 0 1 2 3
	556, 556, 556, 556, 556, 556, 278, 278, 584, 584, // 52-61   4 5 6 7 8 9 : ; < =
	584, 556, 1015, 667, 667, 722, 722, 667, 611, 778, // 62-71  > ? @ A B C D E F G
	722, 278, 500, 667, 556, 833, 722, 778, 667, 778, // 72-81   H I J K L M N O P Q
	722, 667, 611, 722, 667, 944, 667, 667, 611, 278, // 82-91   R S T U V W X Y Z [
	278, 278, 469, 556, 333, 556, 556, 500, 556, 556, // 92-101  \ ] ^ _ ` a b c d e
	278, 556, 556, 222, 222, 500, 222, 833, 556, 556, // 102-111 f g h i j k l m n o
	556, 556, 333, 500, 278, 556, 500, 722, 500, 500, // 112-121 p q r s t u v w x y
	500, 334, 260, 334, 584, // 122-126  z { | } ~
}
