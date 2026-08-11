package optimize

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"veruspdf/backend/internal/pdftest"
)

func samplePDF(t *testing.T) string {
	t.Helper()
	return pdftest.Pages(t, "src.pdf",
		"BT /F1 12 Tf 72 700 Td (One) Tj ET",
		"BT /F1 12 Tf 72 700 Td (Two) Tj ET",
		"BT /F1 12 Tf 72 700 Td (Three) Tj ET")
}

// ── Optimize ─────────────────────────────────────────────────────────────────

func TestOptimize_ProducesReadableOutput(t *testing.T) {
	src := samplePDF(t)
	out := filepath.Join(t.TempDir(), "out.pdf")

	res := New().Optimize(src, out)
	if res.Error != "" {
		t.Fatalf("Optimize: %s", res.Error)
	}

	ctx, err := api.ReadContextFile(out)
	if err != nil {
		t.Fatalf("optimised file will not open: %v", err)
	}
	if ctx.PageCount != 3 {
		t.Errorf("PageCount = %d, want 3 — optimisation must not drop pages", ctx.PageCount)
	}
}

func TestOptimize_ReportsSizes(t *testing.T) {
	src := samplePDF(t)
	out := filepath.Join(t.TempDir(), "out.pdf")

	res := New().Optimize(src, out)
	if res.Error != "" {
		t.Fatalf("Optimize: %s", res.Error)
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	if res.OriginalBytes != srcInfo.Size() {
		t.Errorf("OriginalBytes = %d, want %d", res.OriginalBytes, srcInfo.Size())
	}
	if res.OptimizedBytes <= 0 {
		t.Errorf("OptimizedBytes = %d, want a positive size", res.OptimizedBytes)
	}
	if res.OutputPath != out {
		t.Errorf("OutputPath = %q, want %q", res.OutputPath, out)
	}
}

// The reduction percentage has to be consistent with the byte counts it is
// derived from, including when the file grows.
func TestOptimize_ReductionMatchesByteCounts(t *testing.T) {
	src := samplePDF(t)
	out := filepath.Join(t.TempDir(), "out.pdf")

	res := New().Optimize(src, out)
	if res.Error != "" {
		t.Fatalf("Optimize: %s", res.Error)
	}

	want := float64(res.OriginalBytes-res.OptimizedBytes) / float64(res.OriginalBytes) * 100
	if diff := res.ReductionPct - want; diff > 0.001 || diff < -0.001 {
		t.Errorf("ReductionPct = %v, want %v", res.ReductionPct, want)
	}
	if res.OptimizedBytes > res.OriginalBytes && res.ReductionPct >= 0 {
		t.Errorf("file grew but ReductionPct is %v, want a negative value", res.ReductionPct)
	}
}

func TestOptimize_MissingInput(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.pdf")
	if res := New().Optimize("/nonexistent/nope.pdf", out); res.Error == "" {
		t.Error("expected an error for a missing input")
	}
}

func TestOptimize_NotAPDF(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "notapdf.pdf")
	if err := os.WriteFile(src, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := New().Optimize(src, filepath.Join(dir, "out.pdf")); res.Error == "" {
		t.Error("expected an error for a non-PDF input")
	}
}

// ── Metadata ─────────────────────────────────────────────────────────────────

func TestRemoveMetadata(t *testing.T) {
	src := samplePDF(t)
	out := filepath.Join(t.TempDir(), "out.pdf")

	res := New().RemoveMetadata(src, out)
	if res.Error != "" {
		t.Fatalf("RemoveMetadata: %s", res.Error)
	}

	ctx, err := api.ReadContextFile(out)
	if err != nil {
		t.Fatalf("output will not open: %v", err)
	}
	if ctx.PageCount != 3 {
		t.Errorf("PageCount = %d, want 3", ctx.PageCount)
	}
}

func TestRemoveMetadata_MissingInput(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.pdf")
	if res := New().RemoveMetadata("/nonexistent/nope.pdf", out); res.Error == "" {
		t.Error("expected an error for a missing input")
	}
}

// ── Validation ───────────────────────────────────────────────────────────────

func TestValidate_AcceptsWellFormedPDF(t *testing.T) {
	ok, msg := New().Validate(samplePDF(t))
	if !ok {
		t.Errorf("a well-formed PDF failed validation: %s", msg)
	}
	if msg != "" {
		t.Errorf("got message %q alongside a pass, want empty", msg)
	}
}

func TestValidate_RejectsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.pdf")
	if err := os.WriteFile(path, []byte("definitely not a PDF"), 0o644); err != nil {
		t.Fatal(err)
	}

	ok, msg := New().Validate(path)
	if ok {
		t.Error("garbage passed validation")
	}
	if msg == "" {
		t.Error("validation failed with no explanation")
	}
}

func TestValidate_MissingFile(t *testing.T) {
	if ok, _ := New().Validate("/nonexistent/nope.pdf"); ok {
		t.Error("a missing file passed validation")
	}
}
