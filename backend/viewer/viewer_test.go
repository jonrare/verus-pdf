package viewer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

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
		// A damaged file is not a protected one. Reporting it as encrypted
		// sends the user to remove protection that was never there.
		{"corrupt is not encrypted", errors.New("corrupt xref table"), false},
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

// Encryption is decided from the trailer, not by scanning the file for the
// bytes "/Encrypt". A document that merely draws that text, or names a field
// after it, is not protected.
func TestEncryptionStatus_IgnoresTheLiteralBytesInContent(t *testing.T) {
	path := pdftest.TextPage(t, "mentions.pdf",
		"BT /F1 12 Tf 72 700 Td (see /Encrypt for details) Tj ET")

	if got := New().EncryptionStatus(path); got.Encrypted {
		t.Error("a plain document containing the text \"/Encrypt\" was reported as encrypted")
	}
}

// An owner-password-only document opens without a password but is encrypted:
// Encrypted true, HasUserPW false. Conflating the two hides the Remove
// Protection panel for exactly the files that need it.
func TestEncryptionStatus_OwnerPasswordOnly(t *testing.T) {
	src := pdftest.TextPage(t, "plain.pdf", "BT /F1 12 Tf 72 700 Td (Hello) Tj ET")
	enc := filepath.Join(t.TempDir(), "enc.pdf")

	conf := model.NewDefaultConfiguration()
	conf.EncryptUsingAES = true
	conf.EncryptKeyLength = 256
	conf.OwnerPW = "owner-pw"
	conf.UserPW = ""
	if err := api.EncryptFile(src, enc, conf); err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}

	got := New().EncryptionStatus(enc)
	if !got.Encrypted {
		t.Error("an owner-password-protected file was not reported as encrypted")
	}
	if got.HasUserPW {
		t.Error("HasUserPW is set, but the file opens without a password")
	}
}

// A user password means the file cannot be opened at all without one.
func TestEncryptionStatus_UserPassword(t *testing.T) {
	src := pdftest.TextPage(t, "plain.pdf", "BT /F1 12 Tf 72 700 Td (Hello) Tj ET")
	enc := filepath.Join(t.TempDir(), "enc.pdf")

	conf := model.NewDefaultConfiguration()
	conf.EncryptUsingAES = true
	conf.EncryptKeyLength = 256
	conf.OwnerPW = "owner-pw"
	conf.UserPW = "user-pw"
	if err := api.EncryptFile(src, enc, conf); err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}

	got := New().EncryptionStatus(enc)
	if !got.Encrypted || !got.HasUserPW {
		t.Errorf("got %+v, want both Encrypted and HasUserPW", got)
	}
}

// A damaged file is not encrypted; it is broken, and is reported elsewhere.
func TestEncryptionStatus_GarbageIsNotEncrypted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.pdf")
	if err := os.WriteFile(path, []byte("definitely not a PDF"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := New().EncryptionStatus(path); got.Encrypted || got.HasUserPW {
		t.Errorf("got %+v, want a zero value for a damaged file", got)
	}
}

// ── Working files ────────────────────────────────────────────────────────────

// Every call must hand back a distinct path. The old two-slot scheme alternated
// between exactly two files, so an undo stack deeper than one step pointed at
// content that had already been overwritten.
func TestNewWorkingPath_AlwaysUnique(t *testing.T) {
	svc := New()
	t.Cleanup(svc.Cleanup)

	seen := map[string]bool{}
	for i := 0; i < 10; i++ {
		p, err := svc.NewWorkingPath("report.pdf")
		if err != nil {
			t.Fatalf("NewWorkingPath: %v", err)
		}
		if seen[p] {
			t.Fatalf("path %q handed out twice after %d calls", p, i+1)
		}
		seen[p] = true
	}
}

// Two documents with the same base name from different folders must not share
// a working file.
func TestNewWorkingPath_NoCollisionAcrossSameBaseName(t *testing.T) {
	svc := New()
	t.Cleanup(svc.Cleanup)

	a, err := svc.NewWorkingPath("/home/user/quarterly/report.pdf")
	if err != nil {
		t.Fatalf("NewWorkingPath: %v", err)
	}
	b, err := svc.NewWorkingPath("/home/user/annual/report.pdf")
	if err != nil {
		t.Fatalf("NewWorkingPath: %v", err)
	}
	if a == b {
		t.Errorf("both documents resolved to %q", a)
	}
}

// Only the base name is used, so a caller cannot direct writes out of the
// session directory.
func TestNewWorkingPath_StaysInsideSessionDirectory(t *testing.T) {
	svc := New()
	t.Cleanup(svc.Cleanup)

	first, err := svc.NewWorkingPath("doc.pdf")
	if err != nil {
		t.Fatalf("NewWorkingPath: %v", err)
	}
	dir := filepath.Dir(first)

	for _, in := range []string{"../../etc/passwd", "/etc/passwd", "a/b/c.pdf", ".", "/"} {
		got, err := svc.NewWorkingPath(in)
		if err != nil {
			t.Fatalf("NewWorkingPath(%q): %v", in, err)
		}
		if filepath.Dir(got) != dir {
			t.Errorf("NewWorkingPath(%q) = %q, which escapes %q", in, got, dir)
		}
	}
}

// The session directory holds decrypted copies of protected documents, so it
// must not be readable by other users.
func TestNewWorkingPath_SessionDirectoryIsPrivate(t *testing.T) {
	svc := New()
	t.Cleanup(svc.Cleanup)

	p, err := svc.NewWorkingPath("doc.pdf")
	if err != nil {
		t.Fatalf("NewWorkingPath: %v", err)
	}

	info, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("session directory mode is %#o, want no group or other access", perm)
	}
}

// Old working files are pruned so a long session cannot fill the disk, but the
// window must stay deep enough to cover the UI's 20-level undo stack.
func TestNewWorkingPath_PrunesOldestBeyondTheWindow(t *testing.T) {
	svc := New()
	t.Cleanup(svc.Cleanup)

	var paths []string
	for i := 0; i < maxWorkingFiles+5; i++ {
		p, err := svc.NewWorkingPath("doc.pdf")
		if err != nil {
			t.Fatalf("NewWorkingPath: %v", err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}

	if maxWorkingFiles < 20 {
		t.Errorf("maxWorkingFiles is %d, below the 20-level undo stack", maxWorkingFiles)
	}
	for _, p := range paths[:5] {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%q should have been pruned", filepath.Base(p))
		}
	}
	for _, p := range paths[5:] {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%q should still exist: %v", filepath.Base(p), err)
		}
	}
}

func TestCleanup_RemovesEverything(t *testing.T) {
	svc := New()

	p, err := svc.NewWorkingPath("doc.pdf")
	if err != nil {
		t.Fatalf("NewWorkingPath: %v", err)
	}
	if err := os.WriteFile(p, []byte("sensitive"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(p)

	svc.Cleanup()

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("session directory %q survived cleanup", dir)
	}
}

func TestCleanup_IsSafeToCallTwiceAndWhenUnused(t *testing.T) {
	New().Cleanup() // never allocated a directory

	svc := New()
	if _, err := svc.NewWorkingPath("doc.pdf"); err != nil {
		t.Fatalf("NewWorkingPath: %v", err)
	}
	svc.Cleanup()
	svc.Cleanup()
}

// After cleanup the service must still work — a new directory is allocated.
func TestNewWorkingPath_WorksAfterCleanup(t *testing.T) {
	svc := New()
	if _, err := svc.NewWorkingPath("doc.pdf"); err != nil {
		t.Fatalf("NewWorkingPath: %v", err)
	}
	svc.Cleanup()

	p, err := svc.NewWorkingPath("doc.pdf")
	if err != nil {
		t.Fatalf("NewWorkingPath after Cleanup: %v", err)
	}
	t.Cleanup(svc.Cleanup)
	if _, err := os.Stat(filepath.Dir(p)); err != nil {
		t.Errorf("a fresh session directory was not created: %v", err)
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
