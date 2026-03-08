package bookmarks

import (
	"fmt"
	"io"
	"os"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

type Service struct{}

// Bookmark is a frontend-safe version of pdfcpu.Bookmark (no circular Parent ptr).
type Bookmark struct {
	Title  string     `json:"title"`
	Page   int        `json:"page"`
	Bold   bool       `json:"bold,omitempty"`
	Italic bool       `json:"italic,omitempty"`
	Kids   []Bookmark `json:"kids,omitempty"`
}

type Result struct {
	Error string `json:"error,omitempty"`
}

func convert(bms []pdfcpu.Bookmark) []Bookmark {
	out := make([]Bookmark, 0, len(bms))
	for _, b := range bms {
		out = append(out, Bookmark{
			Title:  b.Title,
			Page:   b.PageFrom,
			Bold:   b.Bold,
			Italic: b.Italic,
			Kids:   convert(b.Kids),
		})
	}
	return out
}

// sanitizeBookmarks creates clean pdfcpu.Bookmark structs from ones read
// by api.Bookmarks. The read path populates internal fields (Parent pointers,
// PageThru=0, AbsPos, Color) that can fail validation when passed back into
// AddBookmarksFile. We only copy the fields we actually need.
func sanitizeBookmarks(bms []pdfcpu.Bookmark) []pdfcpu.Bookmark {
	out := make([]pdfcpu.Bookmark, 0, len(bms))
	for _, b := range bms {
		clean := pdfcpu.Bookmark{
			Title:    b.Title,
			PageFrom: b.PageFrom,
			Bold:     b.Bold,
			Italic:   b.Italic,
			Kids:     sanitizeBookmarks(b.Kids),
		}
		out = append(out, clean)
	}
	return out
}

// ListBookmarks returns all bookmarks (outline) for the PDF.
func (s *Service) ListBookmarks(filePath string) ([]Bookmark, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	bms, err := api.Bookmarks(f, conf)
	if err != nil {
		// File simply has no bookmarks — not an error.
		return []Bookmark{}, nil
	}
	return convert(bms), nil
}

// AddBookmark appends a new top-level bookmark for the given page, preserving existing ones.
type AddBookmarkResult struct {
	Error    string `json:"error,omitempty"`
	Debug    string `json:"debug,omitempty"`
}

func (s *Service) AddBookmark(inputPath, outputPath, title string, page int) AddBookmarkResult {
	if title == "" {
		return AddBookmarkResult{Error: "title cannot be empty"}
	}
	if page < 1 {
		return AddBookmarkResult{Error: fmt.Sprintf("invalid page number: %d", page)}
	}

	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed

	// Read existing bookmarks so we can append without losing them.
	f, err := os.Open(inputPath)
	if err != nil {
		return AddBookmarkResult{Error: fmt.Sprintf("open input: %v", err)}
	}
	existing, _ := api.Bookmarks(f, conf)
	f.Close()

	// Sanitize existing bookmarks — the structs returned by api.Bookmarks
	// carry internal state (Parent pointers, PageThru=0, AbsPos, etc.)
	// that can fail validation when fed back into AddBookmarksFile.
	cleaned := sanitizeBookmarks(existing)
	cleaned = append(cleaned, pdfcpu.Bookmark{Title: title, PageFrom: page})

	debug := fmt.Sprintf("in=%s out=%s title=%q page=%d existing=%d", inputPath, outputPath, title, page, len(existing))

	if err := api.AddBookmarksFile(inputPath, outputPath, cleaned, true, conf); err != nil {
		return AddBookmarkResult{Error: fmt.Sprintf("add bookmark: %v | %s", err, debug)}
	}

	// Verify the output file was actually written
	if _, err := os.Stat(outputPath); err != nil {
		return AddBookmarkResult{Error: fmt.Sprintf("output not written: %v | %s", err, debug)}
	}

	return AddBookmarkResult{Debug: debug}
}

// RemoveBookmark removes the first bookmark matching title+page.
// Strategy: strip all bookmarks with RemoveBookmarksFile, then re-add the survivors.
func (s *Service) RemoveBookmark(inputPath, outputPath, title string, page int) Result {
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed

	// Read existing bookmarks first.
	f, err := os.Open(inputPath)
	if err != nil {
		return Result{Error: err.Error()}
	}
	existing, err := api.Bookmarks(f, conf)
	f.Close()
	if err != nil {
		return Result{Error: fmt.Sprintf("read bookmarks: %v", err)}
	}

	// Build the list to keep (remove first matching entry).
	kept := make([]pdfcpu.Bookmark, 0, len(existing))
	removed := false
	for _, b := range existing {
		if !removed && b.Title == title && b.PageFrom == page {
			removed = true
			continue
		}
		kept = append(kept, b)
	}

	if !removed {
		return Result{Error: fmt.Sprintf("bookmark %q (page %d) not found", title, page)}
	}

	// Strip all outlines to a temp file, then re-add survivors to the final output.
	// Never read and write the same file path simultaneously — it causes corruption.
	stripped := outputPath + ".stripped.pdf"
	defer os.Remove(stripped)

	if err := api.RemoveBookmarksFile(inputPath, stripped, conf); err != nil {
		return Result{Error: fmt.Sprintf("remove all bookmarks: %v", err)}
	}

	if len(kept) > 0 {
		cleaned := sanitizeBookmarks(kept)
		if err := api.AddBookmarksFile(stripped, outputPath, cleaned, true, conf); err != nil {
			return Result{Error: fmt.Sprintf("re-add bookmarks: %v", err)}
		}
	} else {
		// No survivors — stripped file is the final output, just rename it.
		if err := os.Rename(stripped, outputPath); err != nil {
			// Rename failed (e.g. cross-device) — fall back to copy.
			if err2 := copyFile(stripped, outputPath); err2 != nil {
				return Result{Error: fmt.Sprintf("finalize output: %v", err2)}
			}
		}
	}

	return Result{}
}

// copyFile copies src to dst.
func copyFile(src, dst string) error {
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
