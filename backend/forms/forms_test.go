package forms

import (
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"veruspdf/backend/internal/pdftest"
)

// ── Field value typing — ISO 32000-1:2008, §12.7.4.2 ─────────────────────────

// Button fields take a name object; everything else takes a text string.
// Writing a string into a checkbox leaves it rendering as off.
func TestFieldValue_ButtonsGetNameObjects(t *testing.T) {
	got := fieldValue("Btn", "Yes")
	name, ok := got.(types.Name)
	if !ok {
		t.Fatalf("got %T, want types.Name", got)
	}
	if name.Value() != "Yes" {
		t.Errorf("name = %q, want %q", name.Value(), "Yes")
	}
}

func TestFieldValue_OffIsAlsoAName(t *testing.T) {
	if _, ok := fieldValue("Btn", "Off").(types.Name); !ok {
		t.Error("the off state must also be a name object")
	}
}

func TestFieldValue_NonButtonsGetStrings(t *testing.T) {
	for _, ft := range []string{"Tx", "Ch", "Sig", ""} {
		t.Run(ft, func(t *testing.T) {
			if _, ok := fieldValue(ft, "hello").(types.StringLiteral); !ok {
				t.Errorf("field type %q produced %T, want types.StringLiteral", ft, fieldValue(ft, "hello"))
			}
		})
	}
}

// Delimiters in user text must be escaped or the string terminates early and
// corrupts every object after it.
func TestFieldValue_EscapesStringDelimiters(t *testing.T) {
	got, ok := fieldValue("Tx", `a(b)c\d`).(types.StringLiteral)
	if !ok {
		t.Fatalf("got %T, want types.StringLiteral", got)
	}
	if got.Value() == `a(b)c\d` {
		t.Errorf("value %q was stored unescaped", got.Value())
	}
}

// ── /DA parsing — §12.7.3.3 ──────────────────────────────────────────────────

func TestParseDAFontSizeFromString(t *testing.T) {
	// parseDAFontSize needs a context to dereference, so exercise the parsing
	// rule directly through the same "number before Tf" logic it applies.
	tests := []struct {
		name string
		da   string
		want float64
	}{
		{"simple", "/Helv 9 Tf 0 g", 9},
		{"decimal", "/Helv 10.5 Tf 0 g", 10.5},
		{"leading ops", "0 g /Helv 12 Tf", 12},
		{"auto size", "/Helv 0 Tf 0 g", 0},
		{"no Tf", "/Helv 9", 0},
		{"empty", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := daFontSize(tt.da); got != tt.want {
				t.Errorf("daFontSize(%q) = %v, want %v", tt.da, got, tt.want)
			}
		})
	}
}

// ── End to end against a real AcroForm ───────────────────────────────────────

func TestGetFormFields(t *testing.T) {
	path := pdftest.Form(t, "form.pdf")

	fields, err := New().GetFormFields(path)
	if err != nil {
		t.Fatalf("GetFormFields: %v", err)
	}
	if len(fields) == 0 {
		t.Fatal("no fields found in a document that has an AcroForm")
	}

	for _, f := range fields {
		if f.ID == "" {
			t.Errorf("field %q has no ID", f.Name)
		}
		if f.PageNum < 1 {
			t.Errorf("field %q reports page %d", f.Name, f.PageNum)
		}
		if f.Type == "" {
			t.Errorf("field %q has no type", f.Name)
		}
		// Geometry is normalised so width and height are never negative.
		if f.Width < 0 || f.Height < 0 {
			t.Errorf("field %q has negative geometry %vx%v", f.Name, f.Width, f.Height)
		}
	}
}

func TestGetFormFields_TypesAreRecognised(t *testing.T) {
	path := pdftest.Form(t, "form.pdf")

	fields, err := New().GetFormFields(path)
	if err != nil {
		t.Fatalf("GetFormFields: %v", err)
	}

	known := map[string]bool{
		"text": true, "checkbox": true, "radio": true, "button": true,
		"dropdown": true, "listbox": true, "signature": true, "unknown": true,
	}
	seen := map[string]int{}
	for _, f := range fields {
		if !known[f.Type] {
			t.Errorf("field %q has unrecognised type %q", f.Name, f.Type)
		}
		seen[f.Type]++
	}
	if seen["unknown"] == len(fields) {
		t.Error("every field came back as unknown — field type decoding is not working")
	}
}

func TestGetFormFields_NoDuplicateIDs(t *testing.T) {
	path := pdftest.Form(t, "form.pdf")

	fields, err := New().GetFormFields(path)
	if err != nil {
		t.Fatalf("GetFormFields: %v", err)
	}

	seen := map[string]bool{}
	for _, f := range fields {
		if seen[f.ID] {
			t.Errorf("duplicate field ID %q — the /Fields walk and the /Annots sweep both emitted it", f.ID)
		}
		seen[f.ID] = true
	}
}

func TestGetFormFields_MissingFile(t *testing.T) {
	if _, err := New().GetFormFields("/nonexistent/nope.pdf"); err == nil {
		t.Error("expected an error for a missing file")
	}
}

func TestGetFormFields_NoAcroForm(t *testing.T) {
	path := pdftest.TextPage(t, "plain.pdf", "BT /F1 12 Tf 72 700 Td (Hello) Tj ET")

	fields, err := New().GetFormFields(path)
	if err != nil {
		t.Fatalf("GetFormFields on a plain PDF should not error: %v", err)
	}
	if len(fields) != 0 {
		t.Errorf("got %d fields from a document with no form", len(fields))
	}
}

// ── Filling ──────────────────────────────────────────────────────────────────

func TestFillFormFields_PersistsTextValues(t *testing.T) {
	path := pdftest.Form(t, "form.pdf")
	svc := New()

	fields, err := svc.GetFormFields(path)
	if err != nil {
		t.Fatalf("GetFormFields: %v", err)
	}

	var target *FormField
	for i := range fields {
		if fields[i].Type == "text" && !fields[i].ReadOnly && fields[i].Name != "" {
			target = &fields[i]
			break
		}
	}
	if target == nil {
		t.Skip("fixture has no writable text field")
	}

	out := filepath.Join(t.TempDir(), "filled.pdf")
	if res := svc.FillFormFields(path, out, map[string]string{target.Name: "Ada Lovelace"}); res.Error != "" {
		t.Fatalf("FillFormFields: %s", res.Error)
	}

	after, err := svc.GetFormFields(out)
	if err != nil {
		t.Fatalf("GetFormFields(out): %v", err)
	}
	for _, f := range after {
		if f.Name == target.Name {
			if f.Value != "Ada Lovelace" {
				t.Errorf("field %q reads back as %q, want %q", f.Name, f.Value, "Ada Lovelace")
			}
			return
		}
	}
	t.Errorf("field %q disappeared after filling", target.Name)
}

// Text with parentheses must survive a fill round trip — unescaped delimiters
// would terminate the string object early.
func TestFillFormFields_HandlesDelimitersInValues(t *testing.T) {
	path := pdftest.Form(t, "form.pdf")
	svc := New()

	fields, err := svc.GetFormFields(path)
	if err != nil {
		t.Fatalf("GetFormFields: %v", err)
	}
	var name string
	for _, f := range fields {
		if f.Type == "text" && !f.ReadOnly && f.Name != "" {
			name = f.Name
			break
		}
	}
	if name == "" {
		t.Skip("fixture has no writable text field")
	}

	const value = `Smith (Jr.) \ Co.`
	out := filepath.Join(t.TempDir(), "filled.pdf")
	if res := svc.FillFormFields(path, out, map[string]string{name: value}); res.Error != "" {
		t.Fatalf("FillFormFields: %s", res.Error)
	}

	// The output must still parse — that is the real assertion here.
	after, err := svc.GetFormFields(out)
	if err != nil {
		t.Fatalf("output is unparseable, the value corrupted the file: %v", err)
	}
	for _, f := range after {
		if f.Name == name && f.Value != value {
			t.Errorf("field %q reads back as %q, want %q", name, f.Value, value)
		}
	}
}

func TestFillFormFields_MissingInput(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.pdf")
	if res := New().FillFormFields("/nonexistent/nope.pdf", out, nil); res.Error == "" {
		t.Error("expected an error for a missing input")
	}
}

func TestFillFormFields_UnknownFieldNamesAreIgnored(t *testing.T) {
	path := pdftest.Form(t, "form.pdf")
	out := filepath.Join(t.TempDir(), "out.pdf")

	res := New().FillFormFields(path, out, map[string]string{"no-such-field": "x"})
	if res.Error != "" {
		t.Fatalf("FillFormFields: %s", res.Error)
	}
	if _, err := New().GetFormFields(out); err != nil {
		t.Errorf("output is unparseable: %v", err)
	}
}

// ── Reset ────────────────────────────────────────────────────────────────────

func TestResetForm_ClearsValues(t *testing.T) {
	path := pdftest.Form(t, "form.pdf")
	svc := New()

	fields, err := svc.GetFormFields(path)
	if err != nil {
		t.Fatalf("GetFormFields: %v", err)
	}
	var name string
	for _, f := range fields {
		if f.Type == "text" && !f.ReadOnly && f.Name != "" {
			name = f.Name
			break
		}
	}
	if name == "" {
		t.Skip("fixture has no writable text field")
	}

	dir := t.TempDir()
	filled := filepath.Join(dir, "filled.pdf")
	if res := svc.FillFormFields(path, filled, map[string]string{name: "value"}); res.Error != "" {
		t.Fatalf("FillFormFields: %s", res.Error)
	}

	reset := filepath.Join(dir, "reset.pdf")
	if res := svc.ResetForm(filled, reset); res.Error != "" {
		t.Fatalf("ResetForm: %s", res.Error)
	}

	after, err := svc.GetFormFields(reset)
	if err != nil {
		t.Fatalf("GetFormFields(reset): %v", err)
	}
	for _, f := range after {
		if f.Name == name && f.Value != "" {
			t.Errorf("field %q still holds %q after reset", name, f.Value)
		}
	}
}

func TestResetForm_NoAcroForm(t *testing.T) {
	path := pdftest.TextPage(t, "plain.pdf", "BT /F1 12 Tf 72 700 Td (Hello) Tj ET")
	out := filepath.Join(t.TempDir(), "out.pdf")
	if res := New().ResetForm(path, out); res.Error == "" {
		t.Error("expected an error resetting a document with no form")
	}
}

// LockForm is a stub. This pins that fact so the test starts failing the day
// it is implemented and needs real coverage.
func TestLockForm_NotImplemented(t *testing.T) {
	path := pdftest.Form(t, "form.pdf")
	res := New().LockForm(path, filepath.Join(t.TempDir(), "out.pdf"))
	if res.Error == "" {
		t.Error("LockForm reports success — if it is now implemented, replace this test")
	}
}
