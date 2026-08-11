package viewer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type Service struct {
	ctx context.Context

	mu          sync.Mutex
	sessionDir  string   // private scratch directory, created on first use
	workingFile []string // paths handed out, oldest first
	counter     int
	served      map[string]string // token → path the webview may read
}

func New() *Service { return &Service{} }

func (s *Service) SetContext(ctx context.Context) { s.ctx = ctx }

type PageInfo struct {
	Number int     `json:"number"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type DocumentInfo struct {
	Path      string     `json:"path"`
	PageCount int        `json:"pageCount"`
	Title     string     `json:"title"`
	Author    string     `json:"author"`
	Subject   string     `json:"subject"`
	Pages     []PageInfo `json:"pages"`
	Error     string     `json:"error,omitempty"`
}

type Result struct {
	OutputPath string `json:"outputPath"`
	Error      string `json:"error,omitempty"`
}

// --- Dialogs ---

func (s *Service) OpenFileDialog() (string, error) {
	return runtime.OpenFileDialog(s.ctx, runtime.OpenDialogOptions{
		Title: "Open PDF",
		Filters: []runtime.FileFilter{
			{DisplayName: "PDF Files (*.pdf)", Pattern: "*.pdf"},
		},
	})
}

func (s *Service) OpenMultipleFilesDialog() ([]string, error) {
	return runtime.OpenMultipleFilesDialog(s.ctx, runtime.OpenDialogOptions{
		Title: "Select PDFs",
		Filters: []runtime.FileFilter{
			{DisplayName: "PDF Files (*.pdf)", Pattern: "*.pdf"},
		},
	})
}

func (s *Service) OpenImageFilesDialog() ([]string, error) {
	return runtime.OpenMultipleFilesDialog(s.ctx, runtime.OpenDialogOptions{
		Title: "Select Images",
		Filters: []runtime.FileFilter{
			{DisplayName: "Images (*.jpg;*.jpeg;*.png)", Pattern: "*.jpg;*.jpeg;*.png"},
		},
	})
}

func (s *Service) OpenAnyFileDialog() (string, error) {
	return runtime.OpenFileDialog(s.ctx, runtime.OpenDialogOptions{
		Title: "Select File",
	})
}

// looksEncrypted reports whether an open error means a password is required.
//
// Deliberately does NOT treat "corrupt" as encrypted: a genuinely damaged file
// would then be reported to the user as password-protected, sending them to the
// Security panel to remove protection that was never there.
func looksEncrypted(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "password") ||
		strings.Contains(s, "encrypt") ||
		strings.Contains(s, "hex literal")
}

func (s *Service) IsEncrypted(filePath string) bool {
	return s.EncryptionStatus(filePath).Encrypted
}

type EncryptionStatus struct {
	Encrypted bool `json:"encrypted"`
	HasUserPW bool `json:"hasUserPW"` // true if a user (open) password is required
}

// EncryptionStatus reports the encryption state of a file.
//
// Encrypted means the trailer has an /Encrypt entry (§7.5.5); HasUserPW means
// the file cannot be opened at all without a password. An owner-password-only
// document is Encrypted but not HasUserPW — it opens freely, with permissions
// restricted.
//
// This reads the trailer rather than scanning the file for the bytes
// "/Encrypt", which matches inside content streams, text strings, and names
// like /Encryptor, and reports plain documents as protected.
func (s *Service) EncryptionStatus(filePath string) EncryptionStatus {
	ctx, err := api.ReadContextFile(filePath)
	if err != nil {
		// Unreadable without a password means a user password is set. Any
		// other failure (missing file, malformed PDF) is not an encryption
		// question and is reported elsewhere.
		if looksEncrypted(err) {
			return EncryptionStatus{Encrypted: true, HasUserPW: true}
		}
		return EncryptionStatus{}
	}
	return EncryptionStatus{Encrypted: ctx.XRefTable != nil && ctx.XRefTable.Encrypt != nil}
}

func (s *Service) CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func (s *Service) SaveFileDialog(title, defaultFilename string) (string, error) {
	return runtime.SaveFileDialog(s.ctx, runtime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: defaultFilename,
		Filters: []runtime.FileFilter{
			{DisplayName: "PDF Files (*.pdf)", Pattern: "*.pdf"},
		},
	})
}

func (s *Service) OpenDirectoryDialog(title string) (string, error) {
	return runtime.OpenDirectoryDialog(s.ctx, runtime.OpenDialogOptions{
		Title: title,
	})
}

// --- Document ---

func (s *Service) OpenDocument(filePath string) DocumentInfo {
	ctx, err := api.ReadContextFile(filePath)
	if err != nil {
		if looksEncrypted(err) {
			return DocumentInfo{Error: "encrypted: this PDF is password-protected — use Security → Remove Protection first"}
		}
		return DocumentInfo{Error: fmt.Sprintf("could not open PDF: %v", err)}
	}

	info := DocumentInfo{
		Path:      filePath,
		PageCount: ctx.PageCount,
	}

	f, err := os.Open(filePath)
	if err == nil {
		defer f.Close()
		conf := model.NewDefaultConfiguration()
		pdfInfo, err := api.PDFInfo(f, filePath, nil, false, conf)
		if err == nil && pdfInfo != nil {
			info.Title = pdfInfo.Title
			info.Author = pdfInfo.Author
			info.Subject = pdfInfo.Subject
		}
	}

	f2, err := os.Open(filePath)
	if err == nil {
		defer f2.Close()
		dims, err := api.PageDims(f2, model.NewDefaultConfiguration())
		if err == nil {
			for i, d := range dims {
				info.Pages = append(info.Pages, PageInfo{Number: i + 1, Width: d.Width, Height: d.Height})
			}
		}
	}

	if len(info.Pages) == 0 {
		for i := 1; i <= ctx.PageCount; i++ {
			info.Pages = append(info.Pages, PageInfo{Number: i, Width: 612, Height: 792})
		}
	}

	return info
}

func (s *Service) SaveDocument(inputPath, outputPath string) Result {
	// Read entire source into memory — safely handles inputPath == outputPath
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return Result{Error: fmt.Sprintf("cannot open source: %v", err)}
	}
	if len(data) == 0 {
		return Result{Error: "The source file is empty."}
	}

	// Resolve to absolute paths for reliable comparison
	absIn, err1 := filepath.Abs(inputPath)
	absOut, err2 := filepath.Abs(outputPath)
	if err1 == nil && err2 == nil && strings.EqualFold(absIn, absOut) {
		// Source and destination are the same file — nothing to copy
		return Result{OutputPath: outputPath}
	}

	// Write to temp, then rename — prevents empty output on failure
	tmpPath := outputPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		os.Remove(tmpPath)
		return Result{Error: fmt.Sprintf("cannot create output: %v", err)}
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		os.Remove(tmpPath)
		return Result{Error: fmt.Sprintf("cannot create output: %v", err)}
	}
	return Result{OutputPath: outputPath}
}

// maxWorkingFiles bounds how many intermediate files a session keeps. It sits
// just above the UI's 20-level undo stack so every reachable undo target still
// exists on disk.
const maxWorkingFiles = 24

// NewWorkingPath returns a fresh, unused path for the next edit in the chain.
//
// Every call returns a distinct file. That matters for three reasons the old
// two-slot scheme got wrong:
//
//   - Undo. Snapshots pointed at one of two alternating paths, so by the third
//     operation the file a snapshot named had already been overwritten and
//     undoing twice restored the newest content instead of the older state.
//   - Collisions. Paths were derived from the document's base name, so two
//     tabs holding same-named files from different folders shared one working
//     file and clobbered each other.
//   - Disclosure. Files sat directly in the shared temp directory under
//     predictable names with 0644 permissions, and nothing ever removed them —
//     so a decrypted copy of a protected document outlived the session,
//     world-readable. The session directory is created 0700 and removed on
//     shutdown.
//
// Only the base name of the argument is used, so callers may pass a full path.
func (s *Service) NewWorkingPath(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessionDir == "" {
		dir, err := os.MkdirTemp("", "veruspdf-session-")
		if err != nil {
			return "", fmt.Errorf("could not create a working directory: %w", err)
		}
		s.sessionDir = dir
	}

	s.counter++
	base := filepath.Base(name)
	if base == "." || base == string(filepath.Separator) {
		base = "document.pdf"
	}
	path := filepath.Join(s.sessionDir, fmt.Sprintf("%04d-%s", s.counter, base))

	s.workingFile = append(s.workingFile, path)
	for len(s.workingFile) > maxWorkingFiles {
		os.Remove(s.workingFile[0])
		s.revokeFile(s.workingFile[0])
		s.workingFile = s.workingFile[1:]
	}
	return path, nil
}

// Cleanup removes the session's working directory and everything in it.
// Called on application shutdown.
func (s *Service) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Revoke the webview's file grants first — they are independent of the
	// session directory, since a grant can name a document anywhere on disk.
	s.served = nil

	if s.sessionDir == "" {
		return
	}
	os.RemoveAll(s.sessionDir)
	s.sessionDir = ""
	s.workingFile = nil
}
