package merge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"veruspdf/backend/internal/pdftest"
)

// ── Page selection parsing ───────────────────────────────────────────────────

func TestNormalizePageSelection(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"single page", "1", "1"},
		{"comma separated", "1,2,3", "1,2,3"},
		{"comma with spaces", "1, 2, 3", "1,2,3"},
		{"space separated", "1 2 3", "1,2,3"},
		{"range", "1-3", "1-3"},
		{"mixed", "1-3, 5, 7-9", "1-3,5,7-9"},
		{"semicolons", "1;2;3", "1,2,3"},
		{"leading and trailing commas", ",1,2,", "1,2"},
		{"repeated separators", "1,,2", "1,2"},
		{"tabs", "1\t2", "1,2"},
		{"surrounding whitespace", "  1-3  ", "1-3"},
		{"mixed separators", "1, 2 3;4-6", "1,2,3,4-6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizePageSelection(tt.in); got != tt.want {
				t.Errorf("normalizePageSelection(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Whatever normalize produces has to be something pdfcpu will accept, or the
// user sees a parse error from deep inside the library.
func TestNormalizePageSelection_OutputIsParseable(t *testing.T) {
	for _, in := range []string{"1", "1,2,3", "1, 2, 3", "1 2 3", "1-3", "1-3, 5, 7-9", "1;2", "  4  "} {
		normalized := normalizePageSelection(in)
		if normalized == "" {
			continue
		}
		if _, err := api.ParsePageSelection(normalized); err != nil {
			t.Errorf("normalizePageSelection(%q) = %q, which pdfcpu rejects: %v", in, normalized, err)
		}
	}
}

func TestNormalizePageSelection_Idempotent(t *testing.T) {
	for _, in := range []string{"1,2,3", "1-3,5", "1 2 3", "1, 2"} {
		once := normalizePageSelection(in)
		if twice := normalizePageSelection(once); twice != once {
			t.Errorf("not idempotent for %q: %q then %q", in, once, twice)
		}
	}
}

// ── Page extraction ──────────────────────────────────────────────────────────

func fourPager(t *testing.T) string {
	t.Helper()
	return pdftest.Pages(t, "src.pdf",
		"BT /F1 12 Tf 72 700 Td (One) Tj ET",
		"BT /F1 12 Tf 72 700 Td (Two) Tj ET",
		"BT /F1 12 Tf 72 700 Td (Three) Tj ET",
		"BT /F1 12 Tf 72 700 Td (Four) Tj ET")
}

func TestExtractPages(t *testing.T) {
	tests := []struct {
		name      string
		selection string
		wantPages int
	}{
		{"single", "2", 1},
		{"list", "1,3", 2},
		{"range", "2-4", 3},
		{"mixed", "1,3-4", 3},
		{"spaces", "1, 3", 2},
		{"all via empty selection", "", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := fourPager(t)
			out := filepath.Join(t.TempDir(), "out.pdf")

			res := New().ExtractPages(src, out, tt.selection)
			if res.Error != "" {
				t.Fatalf("ExtractPages(%q): %s", tt.selection, res.Error)
			}
			if res.PageCount != tt.wantPages {
				t.Errorf("PageCount = %d, want %d", res.PageCount, tt.wantPages)
			}
			if _, err := os.Stat(out); err != nil {
				t.Errorf("output not written: %v", err)
			}
		})
	}
}

func TestExtractPages_KeepsTheRightPage(t *testing.T) {
	src := fourPager(t)
	out := filepath.Join(t.TempDir(), "out.pdf")

	if res := New().ExtractPages(src, out, "3"); res.Error != "" {
		t.Fatalf("ExtractPages: %s", res.Error)
	}

	ctx, err := api.ReadContextFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if ctx.PageCount != 1 {
		t.Fatalf("PageCount = %d, want 1", ctx.PageCount)
	}
}

func TestExtractPages_InvalidSelection(t *testing.T) {
	src := fourPager(t)
	out := filepath.Join(t.TempDir(), "out.pdf")

	if res := New().ExtractPages(src, out, "not-a-page"); res.Error == "" {
		t.Error("expected an error for an unparseable selection")
	}
}

func TestExtractPages_MissingInput(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.pdf")
	if res := New().ExtractPages("/nonexistent/nope.pdf", out, "1"); res.Error == "" {
		t.Error("expected an error for a missing input file")
	}
}

// ── Rotation ─────────────────────────────────────────────────────────────────

func TestRotatePages(t *testing.T) {
	src := fourPager(t)
	out := filepath.Join(t.TempDir(), "out.pdf")

	if res := New().RotatePages(src, out, 90, "1"); res.Error != "" {
		t.Fatalf("RotatePages: %s", res.Error)
	}

	ctx, err := api.ReadContextFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if ctx.PageCount != 4 {
		t.Errorf("PageCount = %d, want 4 — rotation must not drop pages", ctx.PageCount)
	}
}

func TestRotatePages_AllPagesWhenSelectionEmpty(t *testing.T) {
	src := fourPager(t)
	out := filepath.Join(t.TempDir(), "out.pdf")

	if res := New().RotatePages(src, out, 180, ""); res.Error != "" {
		t.Fatalf("RotatePages: %s", res.Error)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("output not written: %v", err)
	}
}

func TestRotatePages_InvalidSelection(t *testing.T) {
	src := fourPager(t)
	out := filepath.Join(t.TempDir(), "out.pdf")
	if res := New().RotatePages(src, out, 90, "abc"); res.Error == "" {
		t.Error("expected an error for an unparseable selection")
	}
}

// ── Merge ────────────────────────────────────────────────────────────────────

func TestMergeFiles(t *testing.T) {
	a := pdftest.Pages(t, "a.pdf", "BT /F1 12 Tf 72 700 Td (A) Tj ET")
	b := pdftest.Pages(t, "b.pdf",
		"BT /F1 12 Tf 72 700 Td (B1) Tj ET",
		"BT /F1 12 Tf 72 700 Td (B2) Tj ET")
	out := filepath.Join(t.TempDir(), "merged.pdf")

	res := New().MergeFiles([]string{a, b}, out)
	if res.Error != "" {
		t.Fatalf("MergeFiles: %s", res.Error)
	}
	if res.PageCount != 3 {
		t.Errorf("PageCount = %d, want 3", res.PageCount)
	}
}

func TestMergeFiles_NeedsAtLeastTwo(t *testing.T) {
	a := pdftest.Pages(t, "a.pdf", "BT /F1 12 Tf 72 700 Td (A) Tj ET")
	out := filepath.Join(t.TempDir(), "merged.pdf")

	for _, inputs := range [][]string{nil, {}, {a}} {
		if res := New().MergeFiles(inputs, out); res.Error == "" {
			t.Errorf("%d input(s) were accepted", len(inputs))
		}
	}
}

// ── Splitting ────────────────────────────────────────────────────────────────

func TestSplitEveryNPages(t *testing.T) {
	src := fourPager(t)
	dir := t.TempDir()

	res := New().SplitEveryNPages(src, 2, dir)
	if res.Error != "" {
		t.Fatalf("SplitEveryNPages: %s", res.Error)
	}
	if len(res.Files) != 2 {
		t.Errorf("got %d files, want 2: %v", len(res.Files), res.Files)
	}
}

func TestSplitEveryNPages_RejectsNonPositiveN(t *testing.T) {
	src := fourPager(t)
	dir := t.TempDir()
	for _, n := range []int{0, -1} {
		if res := New().SplitEveryNPages(src, n, dir); res.Error == "" {
			t.Errorf("n = %d was accepted", n)
		}
	}
}
