package merge

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

type Service struct{}

func New() *Service { return &Service{} }

type MergeResult struct {
	OutputPath string `json:"outputPath"`
	PageCount  int    `json:"pageCount"`
	Error      string `json:"error,omitempty"`
}

type SplitResult struct {
	Files []string `json:"files"`
	Error string   `json:"error,omitempty"`
}

func (s *Service) MergeFiles(inputPaths []string, outputPath string) MergeResult {
	if len(inputPaths) < 2 {
		return MergeResult{Error: "at least 2 files required to merge"}
	}
	conf := model.NewDefaultConfiguration()
	if err := api.MergeCreateFile(inputPaths, outputPath, false, conf); err != nil {
		return MergeResult{Error: fmt.Sprintf("merge failed: %v", err)}
	}
	ctx, err := api.ReadContextFile(outputPath)
	if err != nil {
		return MergeResult{OutputPath: outputPath, PageCount: -1}
	}
	return MergeResult{OutputPath: outputPath, PageCount: ctx.PageCount}
}

func (s *Service) SplitByBookmarks(inputPath string, outputDir string) SplitResult {
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	if err := api.SplitFile(inputPath, outputDir, 0, conf); err != nil {
		return SplitResult{Error: fmt.Sprintf("split failed: %v", err)}
	}
	matches, _ := filepath.Glob(filepath.Join(outputDir, "*.pdf"))
	return SplitResult{Files: matches}
}

func (s *Service) SplitEveryNPages(inputPath string, n int, outputDir string) SplitResult {
	if n < 1 {
		return SplitResult{Error: "n must be >= 1"}
	}
	conf := model.NewDefaultConfiguration()
	if err := api.SplitFile(inputPath, outputDir, n, conf); err != nil {
		return SplitResult{Error: fmt.Sprintf("split failed: %v", err)}
	}
	matches, _ := filepath.Glob(filepath.Join(outputDir, "*.pdf"))
	return SplitResult{Files: matches}
}

// normalizePageSelection converts flexible user input into pdfcpu's expected
// page selection format. Accepts: "1,2,3", "1, 2, 3", "1-3", "1-3, 5, 7-9",
// "1 2 3", or any mix. Output: "1-3, 5, 7-9" style ranges.
func normalizePageSelection(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	// Split on commas, spaces, or semicolons
	parts := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ';'
	})

	var normalized []string
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		// If it already has a dash, keep it as-is (it's a range like "1-3")
		if strings.Contains(p, "-") {
			normalized = append(normalized, p)
			continue
		}
		// Otherwise it might be a bare number or space-separated numbers
		nums := strings.Fields(p)
		for _, n := range nums {
			n = strings.TrimSpace(n)
			if n != "" {
				normalized = append(normalized, n)
			}
		}
	}

	return strings.Join(normalized, ",")
}

func (s *Service) ExtractPages(inputPath, outputPath, pageSelection string) MergeResult {
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed

	pageSelection = normalizePageSelection(pageSelection)

	// Empty selection means "keep all pages" — just copy the file.
	if pageSelection == "" {
		if err := copyFile(inputPath, outputPath); err != nil {
			return MergeResult{Error: fmt.Sprintf("copy failed: %v", err)}
		}
		ctx, _ := api.ReadContextFile(outputPath)
		pc := 0
		if ctx != nil {
			pc = ctx.PageCount
		}
		return MergeResult{OutputPath: outputPath, PageCount: pc}
	}

	pages, err := api.ParsePageSelection(pageSelection)
	if err != nil {
		return MergeResult{Error: fmt.Sprintf("invalid page selection %q: %v", pageSelection, err)}
	}
	if err := api.TrimFile(inputPath, outputPath, pages, conf); err != nil {
		return MergeResult{Error: fmt.Sprintf("extract failed: %v", err)}
	}
	ctx, _ := api.ReadContextFile(outputPath)
	pc := 0
	if ctx != nil {
		pc = ctx.PageCount
	}
	return MergeResult{OutputPath: outputPath, PageCount: pc}
}

func (s *Service) RotatePages(inputPath, outputPath string, degrees int, pageSelection string) MergeResult {
	conf := model.NewDefaultConfiguration()
	pageSelection = normalizePageSelection(pageSelection)
	pages, err := api.ParsePageSelection(pageSelection)
	if err != nil {
		return MergeResult{Error: fmt.Sprintf("invalid page selection: %v", err)}
	}
	if err := api.RotateFile(inputPath, outputPath, degrees, pages, conf); err != nil {
		return MergeResult{Error: fmt.Sprintf("rotate failed: %v", err)}
	}
	return MergeResult{OutputPath: outputPath}
}

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
