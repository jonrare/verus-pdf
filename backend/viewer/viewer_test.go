package viewer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"veruspdf/backend/internal/pdftest"
)

// ── Encryption heuristics ────────────────────────────────────────────────────

func TestLooksEncrypted(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"password required", errors.New("please provide the correct password"), true},
		{"encrypted", errors.New("this file is encrypted"), true},
		{"mixed case", errors.New("PASSWORD required"), true},
		{"hex literal", errors.New("invalid hex literal"), true},
		{"unrelated", errors.New("no such file or directory"), false},
		{"empty", errors.New(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksEncrypted(tt.err); got != tt.want {
				t.Errorf("looksEncrypted(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestEncryptionStatus_PlainPDF(t *testing.T) {
	path := pdftest.TextPage(t, "plain.pdf", "BT /F1 12 Tf 72 700 Td (Hello) Tj ET")

	got := New().EncryptionStatus(path)
	if got.Encrypted {
		t.Error("a plain PDF was reported as encrypted")
	}
	if got.HasUserPW {
		t.Error("a plain PDF was reported as needing a password")
	}
}

func TestEncryptionStatus_MissingFile(t *testing.T) {
	got := New().EncryptionStatus("/nonexistent/nope.pdf")
	if got.Encrypted || got.HasUserPW {
		t.Errorf("got %+v, want a zero value for an unreadable file", got)
	}
}

func TestIsEncrypted_PlainPDF(t *testing.T) {
	path := pdftest.TextPage(t, "plain.pdf", "BT /F1 12 Tf 72 700 Td (Hello) Tj ET")
	if New().IsEncrypted(path) {
		t.Error("a plain PDF was reported as encrypted")
	}
}

// ── Temp paths ───────────────────────────────────────────────────────────────

// TempPath must reduce whatever it is given to a base name, so a caller
// passing a full path cannot direct writes outside the temp directory.
func TestTempPath_StripsDirectoryComponents(t *testing.T) {
	svc := New()
	tmp := os.TempDir()

	for _, in := range []string{
		"report.pdf",
		"/home/user/documents/report.pdf",
		"../../etc/passwd",
		"/etc/passwd",
	} {
		got := svc.TempPath(in)
		if filepath.Dir(got) != filepath.Clean(tmp) {
			t.Errorf("TempPath(%q) = %q, which is outside %q", in, got, tmp)
		}
		if strings.Contains(filepath.Base(got), "/") || strings.Contains(filepath.Base(got), `\`) {
			t.Errorf("TempPath(%q) = %q, whose base still has separators", in, got)
		}
	}
}

// The two slots exist so an operation never reads and writes the same file.
func TestTempPath_SlotsAreDistinct(t *testing.T) {
	svc := New()
	if a, b := svc.TempPath("doc.pdf"), svc.TempPathB("doc.pdf"); a == b {
		t.Errorf("both slots resolved to %q", a)
	}
}

func TestTempPath_Deterministic(t *testing.T) {
	svc := New()
	if a, b := svc.TempPath("doc.pdf"), svc.TempPath("doc.pdf"); a != b {
		t.Errorf("TempPath is not deterministic: %q then %q", a, b)
	}
}

// ── Save ─────────────────────────────────────────────────────────────────────

func TestSaveDocument_CopiesContent(t *testing.T) {
	src := pdftest.TextPage(t, "src.pdf", "BT /F1 12 Tf 72 700 Td (Hello) Tj ET")
	dst := filepath.Join(t.TempDir(), "dst.pdf")

	if res := New().SaveDocument(src, dst); res.Error != "" {
		t.Fatalf("SaveDocument: %s", res.Error)
	}

	want := pdftest.ReadFile(t, src)
	got := pdftest.ReadFile(t, dst)
	if len(got) != len(want) {
		t.Errorf("copied %d bytes, want %d", len(got), len(want))
	}
}

// Saving over the source must be a no-op rather than truncating the file it is
// about to read.
func TestSaveDocument_SameFileIsSafe(t *testing.T) {
	src := pdftest.TextPage(t, "src.pdf", "BT /F1 12 Tf 72 700 Td (Hello) Tj ET")
	before := pdftest.ReadFile(t, src)

	if res := New().SaveDocument(src, src); res.Error != "" {
		t.Fatalf("SaveDocument: %s", res.Error)
	}

	after := pdftest.ReadFile(t, src)
	if len(after) != len(before) {
		t.Fatalf("file changed size: %d -> %d", len(before), len(after))
	}
}

func TestSaveDocument_MissingSource(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "dst.pdf")
	if res := New().SaveDocument("/nonexistent/nope.pdf", dst); res.Error == "" {
		t.Error("expected an error for a missing source")
	}
}

func TestSaveDocument_EmptySource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "empty.pdf")
	if err := os.WriteFile(src, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if res := New().SaveDocument(src, filepath.Join(dir, "dst.pdf")); res.Error == "" {
		t.Error("expected an error for an empty source")
	}
}

// A failed write must not leave a stray .tmp file behind.
func TestSaveDocument_NoTempLeftOnFailure(t *testing.T) {
	src := pdftest.TextPage(t, "src.pdf", "BT /F1 12 Tf 72 700 Td (Hello) Tj ET")
	dst := filepath.Join(t.TempDir(), "no-such-dir", "dst.pdf")

	if res := New().SaveDocument(src, dst); res.Error == "" {
		t.Fatal("expected an error writing into a missing directory")
	}
	if _, err := os.Stat(dst + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("a .tmp file was left behind at %s", dst+".tmp")
	}
}

// ── Document info ────────────────────────────────────────────────────────────

func TestOpenDocument(t *testing.T) {
	path := pdftest.Pages(t, "doc.pdf",
		"BT /F1 12 Tf 72 700 Td (One) Tj ET",
		"BT /F1 12 Tf 72 700 Td (Two) Tj ET")

	got := New().OpenDocument(path)
	if got.Error != "" {
		t.Fatalf("OpenDocument: %s", got.Error)
	}
	if got.PageCount != 2 {
		t.Errorf("PageCount = %d, want 2", got.PageCount)
	}
	if got.Path != path {
		t.Errorf("Path = %q, want %q", got.Path, path)
	}
	if len(got.Pages) != 2 {
		t.Fatalf("got %d page entries, want 2", len(got.Pages))
	}
	for i, p := range got.Pages {
		if p.Number != i+1 {
			t.Errorf("page entry %d reports number %d", i, p.Number)
		}
		if p.Width <= 0 || p.Height <= 0 {
			t.Errorf("page %d has non-positive dimensions %vx%v", p.Number, p.Width, p.Height)
		}
	}
}

func TestOpenDocument_MissingFile(t *testing.T) {
	if got := New().OpenDocument("/nonexistent/nope.pdf"); got.Error == "" {
		t.Error("expected an error for a missing file")
	}
}

func TestOpenDocument_NotAPDF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notapdf.pdf")
	if err := os.WriteFile(path, []byte("this is not a PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := New().OpenDocument(path); got.Error == "" {
		t.Error("expected an error for a non-PDF file")
	}
}

// ── Byte reading ─────────────────────────────────────────────────────────────

func TestReadFileBytes_RoundTrips(t *testing.T) {
	path := pdftest.TextPage(t, "doc.pdf", "BT /F1 12 Tf 72 700 Td (Hello) Tj ET")

	encoded, err := New().ReadFileBytes(path)
	if err != nil {
		t.Fatalf("ReadFileBytes: %v", err)
	}
	if encoded == "" {
		t.Fatal("got an empty string")
	}
	// Base64 of a PDF always begins with the encoding of "%PDF".
	if !strings.HasPrefix(encoded, "JVBERi") {
		t.Errorf("got %q…, which does not decode to a PDF header", encoded[:min(12, len(encoded))])
	}
}

func TestReadFileBytes_MissingFile(t *testing.T) {
	if _, err := New().ReadFileBytes("/nonexistent/nope.pdf"); err == nil {
		t.Error("expected an error for a missing file")
	}
}

// ── Copy ─────────────────────────────────────────────────────────────────────

func TestCopyFile(t *testing.T) {
	src := pdftest.TextPage(t, "src.pdf", "BT /F1 12 Tf 72 700 Td (Hello) Tj ET")
	dst := filepath.Join(t.TempDir(), "dst.pdf")

	if err := New().CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	if len(pdftest.ReadFile(t, dst)) != len(pdftest.ReadFile(t, src)) {
		t.Error("copied file differs in size")
	}
}

func TestCopyFile_MissingSource(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "dst.pdf")
	if err := New().CopyFile("/nonexistent/nope.pdf", dst); err == nil {
		t.Error("expected an error for a missing source")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
