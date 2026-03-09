package viewer

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type Service struct {
	ctx context.Context
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

// looksEncrypted returns true if the error looks like a failed decrypt attempt.
func looksEncrypted(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "password") ||
		strings.Contains(s, "encrypt") ||
		strings.Contains(s, "hex literal") ||
		strings.Contains(s, "corrupt")
}

func (s *Service) IsEncrypted(filePath string) bool {
	// Primary: scan raw PDF bytes for /Encrypt in the trailer.
	// Every encrypted PDF has this entry regardless of whether a user password is set.
	// An owner-password-only PDF opens fine without a password but still has /Encrypt.
	if b, err := os.ReadFile(filePath); err == nil {
		if bytes.Contains(b, []byte("/Encrypt")) {
			return true
		}
	}
	// Fallback: if reading fails with a password/corrupt error, it's encrypted.
	_, err := api.ReadContextFile(filePath)
	return looksEncrypted(err)
}

type EncryptionStatus struct {
	Encrypted    bool `json:"encrypted"`
	HasUserPW    bool `json:"hasUserPW"`    // true if a user (open) password is required
}

// EncryptionStatus returns encryption state for a file.
// Encrypted=true means /Encrypt is present (owner or user password).
// HasUserPW=true means the file can't be opened without a password.
func (s *Service) EncryptionStatus(filePath string) EncryptionStatus {
	b, err := os.ReadFile(filePath)
	if err != nil {
		return EncryptionStatus{}
	}
	encrypted := bytes.Contains(b, []byte("/Encrypt"))
	// If ReadContextFile fails, a user (open) password is required.
	_, openErr := api.ReadContextFile(filePath)
	hasUserPW := looksEncrypted(openErr)
	return EncryptionStatus{Encrypted: encrypted, HasUserPW: hasUserPW}
}

func (s *Service) CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil { return err }
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil { return err }
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

func (s *Service) ReadFileBytes(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("could not read file: %v", err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// TempPath returns a writable temp file path for the given name.
// It always uses only the base filename, so callers may pass a full path safely.
func (s *Service) TempPath(name string) string {
	return filepath.Join(os.TempDir(), "veruspdf_"+filepath.Base(name))
}

// TempPathB is the second temp slot (read A / write B alternation).
func (s *Service) TempPathB(name string) string {
	return filepath.Join(os.TempDir(), "veruspdf_b_"+filepath.Base(name))
}
