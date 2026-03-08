package convert

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	runtime "github.com/wailsapp/wails/v2/pkg/runtime"
	"veruspdf/backend/edit"
)

type Service struct {
	ctx          context.Context
	lastTempPath string // path to temp file written during extraction
}

func New() *Service { return &Service{} }

func (s *Service) SetContext(ctx context.Context) { s.ctx = ctx }

type Result struct {
	OutputPath string   `json:"outputPath"`
	Files      []string `json:"files,omitempty"`
	Error      string   `json:"error,omitempty"`
}

type TextResult struct {
	Text  string `json:"text"`
	Error string `json:"error,omitempty"`
}

func (s *Service) ImagesToPDF(imagePaths []string, outputPath string) Result {
	if len(imagePaths) == 0 {
		return Result{Error: "no images provided"}
	}
	conf := model.NewDefaultConfiguration()
	if err := api.ImportImagesFile(imagePaths, outputPath, nil, conf); err != nil {
		return Result{Error: fmt.Sprintf("images to PDF failed: %v", err)}
	}
	return Result{OutputPath: outputPath}
}

func (s *Service) PDFToImages(inputPath, outputDir string) Result {
	conf := model.NewDefaultConfiguration()
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return Result{Error: fmt.Sprintf("cannot create output dir: %v", err)}
	}
	if err := api.ExtractImagesFile(inputPath, outputDir, nil, conf); err != nil {
		return Result{Error: fmt.Sprintf("PDF to images failed: %v", err)}
	}
	matches, _ := filepath.Glob(filepath.Join(outputDir, "*"))
	return Result{OutputPath: outputDir, Files: matches}
}

func (s *Service) ExtractImages(inputPath, outputDir string) Result {
	conf := model.NewDefaultConfiguration()
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return Result{Error: fmt.Sprintf("cannot create output dir: %v", err)}
	}
	if err := api.ExtractImagesFile(inputPath, outputDir, nil, conf); err != nil {
		return Result{Error: fmt.Sprintf("extract images failed: %v", err)}
	}
	matches, _ := filepath.Glob(filepath.Join(outputDir, "*"))
	return Result{OutputPath: outputDir, Files: matches}
}

// PDFToText extracts text and writes it to a temp file on disk.
// The temp file path is stored on the service for SaveExtractedText to use.
func (s *Service) PDFToText(inputPath string) TextResult {
	ctx, err := api.ReadContextFile(inputPath)
	if err != nil {
		return TextResult{Error: fmt.Sprintf("could not open PDF: %v", err)}
	}

	var pages []string
	for page := 1; page <= ctx.PageCount; page++ {
		spans, err := edit.ExtractText(inputPath, page)
		if err != nil || len(spans) == 0 {
			continue
		}
		type line struct {
			y    float64
			text string
		}
		var lines []line
		for _, sp := range spans {
			merged := false
			for i := range lines {
				if abs(lines[i].y-sp.Y) < sp.FontSize*0.5 {
					lines[i].text += " " + sp.Text
					merged = true
					break
				}
			}
			if !merged {
				lines = append(lines, line{y: sp.Y, text: sp.Text})
			}
		}
		var lineTexts []string
		for _, l := range lines {
			lineTexts = append(lineTexts, strings.TrimSpace(l.text))
		}
		pages = append(pages, fmt.Sprintf("--- Page %d ---\n%s", page, strings.Join(lineTexts, "\n")))
	}

	text := strings.Join(pages, "\n\n")
	if len(pages) == 0 {
		text = "(No selectable text found in this PDF)"
	}

	// Safety net: strip any null bytes that slipped through from CID fonts
	// or other exotic encodings that the decoder didn't fully handle.
	text = strings.ReplaceAll(text, "\x00", "")

	// Write to temp file — this is the authoritative copy, no string over IPC
	if s.lastTempPath != "" {
		os.Remove(s.lastTempPath)
	}
	tmp, err := os.CreateTemp("", "veruspdf-*.txt")
	if err == nil {
		tmp.WriteString(text)
		tmp.Close()
		s.lastTempPath = tmp.Name()
	}

	return TextResult{Text: text}
}

// SaveExtractedText opens a native Save As dialog then copies the temp file
// to the destination. Only the short suggestedName crosses IPC.
func (s *Service) SaveExtractedText(suggestedName string) string {
	if s.lastTempPath == "" {
		return "no extracted text — run Extract Text first"
	}
	dest, err := runtime.SaveFileDialog(s.ctx, runtime.SaveDialogOptions{
		Title:           "Save Text File",
		DefaultFilename: suggestedName,
		Filters: []runtime.FileFilter{
			{DisplayName: "Text Files (*.txt)", Pattern: "*.txt"},
		},
	})
	if err != nil {
		return fmt.Sprintf("dialog error: %v", err)
	}
	if dest == "" {
		return "" // cancelled
	}
	src, err := os.Open(s.lastTempPath)
	if err != nil {
		return fmt.Sprintf("could not open temp file: %v", err)
	}
	defer src.Close()
	dst, err := os.Create(dest)
	if err != nil {
		return fmt.Sprintf("could not create destination: %v", err)
	}
	defer dst.Close()
	if _, err = io.Copy(dst, src); err != nil {
		return fmt.Sprintf("write failed: %v", err)
	}
	return ""
}

// WriteFromTemp copies the last extracted text temp file to destPath.
// destPath is a short string passed from JS — safe over IPC.
func (s *Service) WriteFromTemp(destPath string) string {
	if s.lastTempPath == "" {
		return "no extracted text — run Extract Text first"
	}
	data, err := os.ReadFile(s.lastTempPath)
	if err != nil {
		return fmt.Sprintf("could not read temp file: %v", err)
	}
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return fmt.Sprintf("could not write file: %v", err)
	}
	return ""
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
