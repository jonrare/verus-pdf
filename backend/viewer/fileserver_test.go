package viewer

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"veruspdf/backend/internal/pdftest"
)

// The document reaches the viewer over the asset server. These tests drive the
// handler directly, so the serving contract is pinned even though the Wails
// wiring itself can only be exercised by running the app.

func grantedFile(t *testing.T) (*Service, string, string) {
	t.Helper()
	svc := New()
	t.Cleanup(svc.Cleanup)

	path := pdftest.TextPage(t, "doc.pdf", "BT /F1 12 Tf 72 700 Td (Hello) Tj ET")
	url, err := svc.FileURL(path)
	if err != nil {
		t.Fatalf("FileURL: %v", err)
	}
	return svc, path, url
}

func TestFileURL_ReturnsARelativeURLUnderThePrefix(t *testing.T) {
	_, _, url := grantedFile(t)

	if got := url[:len(filePrefix)]; got != filePrefix {
		t.Errorf("URL %q does not start with %q", url, filePrefix)
	}
	if len(url) <= len(filePrefix) {
		t.Errorf("URL %q carries no token", url)
	}
}

// The URL must not leak the filesystem path.
func TestFileURL_DoesNotContainThePath(t *testing.T) {
	_, path, url := grantedFile(t)

	for _, part := range []string{filepath.Dir(path), "doc.pdf", ".."} {
		if part != "" && strings.Contains(url, part) {
			t.Errorf("URL %q leaks %q", url, part)
		}
	}
}

func TestFileURL_RejectsMissingAndNonRegularFiles(t *testing.T) {
	svc := New()
	t.Cleanup(svc.Cleanup)

	if _, err := svc.FileURL("/nonexistent/nope.pdf"); err == nil {
		t.Error("a missing file was granted")
	}
	if _, err := svc.FileURL(t.TempDir()); err == nil {
		t.Error("a directory was granted")
	}
}

func TestFileHandler_ServesTheGrantedFile(t *testing.T) {
	svc, path, url := grantedFile(t)

	rec := httptest.NewRecorder()
	svc.FileHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/pdf" {
		t.Errorf("Content-Type %q, want application/pdf", got)
	}
	want := pdftest.ReadFile(t, path)
	if got := rec.Body.Bytes(); len(got) != len(want) {
		t.Errorf("served %d bytes, want %d", len(got), len(want))
	}
	if got := rec.Body.String(); len(got) < 5 || got[:5] != "%PDF-" {
		t.Errorf("body does not start with a PDF header")
	}
}

// Nothing may be served that was not explicitly granted — the handler cannot be
// talked into reading an arbitrary path.
func TestFileHandler_RefusesUngrantedTokens(t *testing.T) {
	svc, _, _ := grantedFile(t)

	for _, target := range []string{
		filePrefix + "deadbeef",
		filePrefix,
		filePrefix + "../../etc/passwd",
		"/etc/passwd",
		"/",
		"/index.html",
	} {
		t.Run(target, func(t *testing.T) {
			rec := httptest.NewRecorder()
			svc.FileHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
			if rec.Code != http.StatusNotFound {
				t.Errorf("status %d for %q, want 404", rec.Code, target)
			}
		})
	}
}

// Range support is what lets pdf.js render page one without downloading the
// whole document.
func TestFileHandler_SupportsRangeRequests(t *testing.T) {
	svc, path, url := grantedFile(t)
	full := pdftest.ReadFile(t, path)

	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Range", "bytes=0-15")

	rec := httptest.NewRecorder()
	svc.FileHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status %d, want 206 — without ranges pdf.js must buffer the whole file", rec.Code)
	}
	if got, want := rec.Body.Len(), 16; got != want {
		t.Errorf("served %d bytes, want %d", got, want)
	}
	if got, want := rec.Body.String(), string(full[:16]); got != want {
		t.Errorf("served %q, want %q", got, want)
	}
	if got, want := rec.Header().Get("Content-Range"), fmt.Sprintf("bytes 0-15/%d", len(full)); got != want {
		t.Errorf("Content-Range %q, want %q", got, want)
	}
}

// Working-file paths are reused as the retention window wraps, so a cached
// response would show a stale document.
func TestFileHandler_ForbidsCaching(t *testing.T) {
	svc, _, url := grantedFile(t)

	rec := httptest.NewRecorder()
	svc.FileHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", got)
	}
}

// A grant follows the file: once a working file is pruned, its URL stops
// serving.
func TestFileHandler_GrantIsRevokedWhenTheWorkingFileIsPruned(t *testing.T) {
	svc := New()
	t.Cleanup(svc.Cleanup)

	src := pdftest.TextPage(t, "doc.pdf", "BT /F1 12 Tf 72 700 Td (Hello) Tj ET")
	data := pdftest.ReadFile(t, src)

	first, err := svc.NewWorkingPath("doc.pdf")
	if err != nil {
		t.Fatalf("NewWorkingPath: %v", err)
	}
	writeFile(t, first, data)

	url, err := svc.FileURL(first)
	if err != nil {
		t.Fatalf("FileURL: %v", err)
	}

	rec := httptest.NewRecorder()
	svc.FileHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d before pruning, want 200", rec.Code)
	}

	// Push the first file out of the retention window.
	for i := 0; i < maxWorkingFiles+1; i++ {
		p, err := svc.NewWorkingPath("doc.pdf")
		if err != nil {
			t.Fatalf("NewWorkingPath: %v", err)
		}
		writeFile(t, p, data)
	}

	rec = httptest.NewRecorder()
	svc.FileHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d after pruning, want 404 — the grant outlived the file", rec.Code)
	}
}

func TestCleanup_RevokesAllGrants(t *testing.T) {
	svc, _, url := grantedFile(t)
	svc.Cleanup()

	rec := httptest.NewRecorder()
	svc.FileHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d after cleanup, want 404", rec.Code)
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
