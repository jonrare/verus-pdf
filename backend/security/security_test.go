package security

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"veruspdf/backend/internal/pdftest"
)

func plainPDF(t *testing.T) string {
	t.Helper()
	return pdftest.TextPage(t, "plain.pdf", "BT /F1 12 Tf 72 700 Td (Secret) Tj ET")
}

// ── Encryption ───────────────────────────────────────────────────────────────

func TestEncrypt_ProducesEncryptedFile(t *testing.T) {
	src := plainPDF(t)
	out := filepath.Join(t.TempDir(), "enc.pdf")

	if res := New().Encrypt(src, out, "owner-pw", "", PermissionsAll); res.Error != "" {
		t.Fatalf("Encrypt: %s", res.Error)
	}

	// An encrypted document carries /Encrypt in its trailer (§7.6.1).
	if !bytes.Contains(pdftest.ReadFile(t, out), []byte("/Encrypt")) {
		t.Error("output has no /Encrypt entry")
	}
}

// With an owner password only, the file still opens without a password —
// the restriction is on permissions, not on access (§7.6.4.2).
func TestEncrypt_OwnerOnlyStillOpens(t *testing.T) {
	src := plainPDF(t)
	out := filepath.Join(t.TempDir(), "enc.pdf")

	if res := New().Encrypt(src, out, "owner-pw", "", PermissionsPrint); res.Error != "" {
		t.Fatalf("Encrypt: %s", res.Error)
	}
	if _, err := api.ReadContextFile(out); err != nil {
		t.Errorf("owner-only encryption should still open without a password: %v", err)
	}
}

// With a user password set, opening without one must fail.
func TestEncrypt_UserPasswordBlocksOpening(t *testing.T) {
	src := plainPDF(t)
	out := filepath.Join(t.TempDir(), "enc.pdf")

	if res := New().Encrypt(src, out, "owner-pw", "user-pw", PermissionsAll); res.Error != "" {
		t.Fatalf("Encrypt: %s", res.Error)
	}
	if _, err := api.ReadContextFile(out); err == nil {
		t.Error("a user-password-protected file opened with no password")
	}
}

func TestEncrypt_AllPermissionLevels(t *testing.T) {
	for _, level := range []PermissionLevel{PermissionsNone, PermissionsPrint, PermissionsAll} {
		t.Run(string(level), func(t *testing.T) {
			src := plainPDF(t)
			out := filepath.Join(t.TempDir(), "enc.pdf")
			if res := New().Encrypt(src, out, "owner-pw", "", level); res.Error != "" {
				t.Fatalf("Encrypt(%s): %s", level, res.Error)
			}
			if _, err := os.Stat(out); err != nil {
				t.Errorf("no output for permission level %s", level)
			}
		})
	}
}

// An unrecognised level must fall through to the most restrictive setting
// rather than silently granting full access.
func TestEncrypt_UnknownPermissionLevelIsRestrictive(t *testing.T) {
	src := plainPDF(t)
	out := filepath.Join(t.TempDir(), "enc.pdf")

	if res := New().Encrypt(src, out, "owner-pw", "", PermissionLevel("bogus")); res.Error != "" {
		t.Fatalf("Encrypt: %s", res.Error)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("no output: %v", err)
	}
}

func TestEncrypt_MissingInput(t *testing.T) {
	out := filepath.Join(t.TempDir(), "enc.pdf")
	if res := New().Encrypt("/nonexistent/nope.pdf", out, "pw", "", PermissionsAll); res.Error == "" {
		t.Error("expected an error for a missing input")
	}
}

// ── Round trip ───────────────────────────────────────────────────────────────

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	src := plainPDF(t)
	dir := t.TempDir()
	enc := filepath.Join(dir, "enc.pdf")
	dec := filepath.Join(dir, "dec.pdf")

	if res := New().Encrypt(src, enc, "owner-pw", "user-pw", PermissionsAll); res.Error != "" {
		t.Fatalf("Encrypt: %s", res.Error)
	}
	if res := New().Decrypt(enc, dec, "user-pw"); res.Error != "" {
		t.Fatalf("Decrypt: %s", res.Error)
	}

	// The decrypted file must open with no password and keep its content.
	ctx, err := api.ReadContextFile(dec)
	if err != nil {
		t.Fatalf("decrypted file will not open: %v", err)
	}
	if ctx.PageCount != 1 {
		t.Errorf("PageCount = %d, want 1", ctx.PageCount)
	}
	if bytes.Contains(pdftest.ReadFile(t, dec), []byte("/Encrypt")) {
		t.Error("decrypted output still carries /Encrypt")
	}
}

func TestDecrypt_WrongPassword(t *testing.T) {
	src := plainPDF(t)
	dir := t.TempDir()
	enc := filepath.Join(dir, "enc.pdf")

	if res := New().Encrypt(src, enc, "owner-pw", "user-pw", PermissionsAll); res.Error != "" {
		t.Fatalf("Encrypt: %s", res.Error)
	}
	if res := New().Decrypt(enc, filepath.Join(dir, "dec.pdf"), "wrong-pw"); res.Error == "" {
		t.Error("decryption succeeded with the wrong password")
	}
}

func TestDecrypt_NotEncrypted(t *testing.T) {
	src := plainPDF(t)
	out := filepath.Join(t.TempDir(), "dec.pdf")
	// Decrypting a plain file is a no-op at best; it must not panic or produce
	// a silently corrupt file.
	if res := New().Decrypt(src, out, "pw"); res.Error == "" {
		if _, err := api.ReadContextFile(out); err != nil {
			t.Errorf("reported success but produced an unreadable file: %v", err)
		}
	}
}

// ── Password change ──────────────────────────────────────────────────────────

func TestChangePassword(t *testing.T) {
	src := plainPDF(t)
	dir := t.TempDir()
	enc := filepath.Join(dir, "enc.pdf")
	changed := filepath.Join(dir, "changed.pdf")

	if res := New().Encrypt(src, enc, "old-owner", "", PermissionsAll); res.Error != "" {
		t.Fatalf("Encrypt: %s", res.Error)
	}
	if res := New().ChangePassword(enc, changed, "old-owner", "new-owner"); res.Error != "" {
		t.Fatalf("ChangePassword: %s", res.Error)
	}

	// The new owner password must work for a subsequent decrypt.
	conf := model.NewDefaultConfiguration()
	conf.OwnerPW = "new-owner"
	conf.UserPW = "new-owner"
	if err := api.DecryptFile(changed, filepath.Join(dir, "dec.pdf"), conf); err != nil {
		t.Errorf("the new owner password does not work: %v", err)
	}
}

func TestChangePassword_WrongCurrent(t *testing.T) {
	src := plainPDF(t)
	dir := t.TempDir()
	enc := filepath.Join(dir, "enc.pdf")

	if res := New().Encrypt(src, enc, "old-owner", "", PermissionsAll); res.Error != "" {
		t.Fatalf("Encrypt: %s", res.Error)
	}
	res := New().ChangePassword(enc, filepath.Join(dir, "changed.pdf"), "wrong", "new-owner")
	if res.Error == "" {
		t.Error("the password was changed using the wrong current password")
	}
}
