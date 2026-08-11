package viewer

// Serving documents to the webview.
//
// The viewer needs the raw PDF bytes. Handing them over the Wails bridge meant
// base64-encoding the whole file into a JSON string and rebuilding it in JS one
// character at a time — for a 100 MB document, a ~133 MB string across the
// bridge and a 100-million-iteration loop on the UI thread before rendering
// could even start.
//
// Instead the file is served over the asset server. pdf.js fetches it like any
// other URL, the bytes never become text, and because the handler supports
// range requests pdf.js can render the first page without waiting for the rest.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// filePrefix is the URL space the document handler owns.
const filePrefix = "/_file/"

// fileToken derives the opaque id a path is served under.
//
// Serving by token rather than by path keeps filesystem paths out of URLs and
// makes the handler incapable of path traversal: it can only ever return a file
// that some earlier call explicitly granted.
func fileToken(absPath string) string {
	sum := sha256.Sum256([]byte(absPath))
	return hex.EncodeToString(sum[:16])
}

// FileURL grants the webview read access to one file and returns the URL that
// serves it. The URL is relative, so it resolves against whichever origin the
// webview is using on this platform.
func (s *Service) FileURL(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("could not resolve %s: %w", filepath.Base(path), err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("could not open %s: %w", filepath.Base(path), err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a file", filepath.Base(path))
	}

	token := fileToken(abs)

	s.mu.Lock()
	if s.served == nil {
		s.served = make(map[string]string)
	}
	s.served[token] = abs
	s.mu.Unlock()

	return filePrefix + token, nil
}

// FileHandler serves the files granted by FileURL. Wails calls it for any GET
// the embedded assets do not satisfy.
func (s *Service) FileHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, filePrefix) {
			http.NotFound(w, r)
			return
		}
		token := strings.TrimPrefix(r.URL.Path, filePrefix)
		if token == "" || strings.Contains(token, "/") {
			http.NotFound(w, r)
			return
		}

		s.mu.Lock()
		path, granted := s.served[token]
		s.mu.Unlock()
		if !granted {
			http.NotFound(w, r)
			return
		}

		f, err := os.Open(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/pdf")
		// A token is derived from the path, and an edit chain reuses paths as
		// the working-file window wraps around, so a cached response could show
		// a stale document.
		w.Header().Set("Cache-Control", "no-store")

		// Drop conditional headers so ServeContent can only ever answer 200 or
		// 206. It would otherwise answer 304 to a conditional request, and the
		// legacy WebKitGTK path (a Linux build without the webkit2_41 tag)
		// rejects every status other than 200 outright — turning a harmless
		// cache revalidation into a failed load. Revalidation is meaningless
		// here anyway: responses are no-store.
		r.Header.Del("If-Modified-Since")
		r.Header.Del("If-None-Match")
		r.Header.Del("If-Range")

		// ServeContent handles range requests, which is what lets pdf.js render
		// the first page before the whole file has arrived. See
		// docs/asset-server.md for what each platform actually forwards.
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	})
}

// revokeFile drops the grant for a path. Called when a working file is pruned.
func (s *Service) revokeFile(absPath string) {
	delete(s.served, fileToken(absPath))
}
