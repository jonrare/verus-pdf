package edit

import (
	"fmt"
	"io"
	"os"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpupkg "github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// NOTE: True glyph-level PDF text editing is not supported by any pure-Go
// library. This service uses overlay-based editing: stamp new text over
// the original area. For redaction, we use pdfcpu's watermark API to
// paint a white filled rectangle, then stamp text on top.

type Service struct{}

func New() *Service { return &Service{} }

type Result struct {
	OutputPath string `json:"outputPath"`
	Error      string `json:"error,omitempty"`
}

type TextEdit struct {
	PageNumber int     `json:"pageNumber"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Width      float64 `json:"width"`
	Height     float64 `json:"height"`
	NewText    string  `json:"newText"`
	FontName   string  `json:"fontName"`
	FontSize   int     `json:"fontSize"`
	FontColor  string  `json:"fontColor"`
	BgColor    string  `json:"bgColor"`
}

// AddTextStamp adds a text watermark/stamp to specified pages.
// descriptor format: "font:Helvetica, points:48, color:#FF0000, rotation:45, opacity:0.3, position:c"
func (s *Service) AddTextStamp(inputPath, outputPath, text, descriptor, pageSelection string) Result {
	conf := model.NewDefaultConfiguration()
	pages, err := api.ParsePageSelection(pageSelection)
	if err != nil {
		return Result{Error: fmt.Sprintf("invalid page selection: %v", err)}
	}
	// v0.7: AddTextWatermarksFile(inFile, outFile, selectedPages, onTop bool, text, desc string, conf)
	if err := api.AddTextWatermarksFile(inputPath, outputPath, pages, true, text, descriptor, conf); err != nil {
		return Result{Error: fmt.Sprintf("add stamp failed: %v", err)}
	}
	return Result{OutputPath: outputPath}
}

// AddImageStamp adds an image overlay to specified pages.
// descriptor: "position:br, offset:-10 -10, scale:0.3 abs"
func (s *Service) AddImageStamp(inputPath, outputPath, imagePath, descriptor, pageSelection string) Result {
	conf := model.NewDefaultConfiguration()
	pages, err := api.ParsePageSelection(pageSelection)
	if err != nil {
		return Result{Error: fmt.Sprintf("invalid page selection: %v", err)}
	}
	if err := api.AddImageWatermarksFile(inputPath, outputPath, pages, true, imagePath, descriptor, conf); err != nil {
		return Result{Error: fmt.Sprintf("add image stamp failed: %v", err)}
	}
	return Result{OutputPath: outputPath}
}

// RemoveWatermarks removes all watermarks/stamps from specified pages.
func (s *Service) RemoveWatermarks(inputPath, outputPath, pageSelection string) Result {
	conf := model.NewDefaultConfiguration()
	pages, err := api.ParsePageSelection(pageSelection)
	if err != nil {
		return Result{Error: fmt.Sprintf("invalid page selection: %v", err)}
	}
	if err := api.RemoveWatermarksFile(inputPath, outputPath, pages, conf); err != nil {
		return Result{Error: fmt.Sprintf("remove watermarks failed: %v", err)}
	}
	return Result{OutputPath: outputPath}
}

// AddPageNumbers stamps page numbers onto all pages.
// descriptor example: "font:Helvetica, points:10, color:#555555, position:bc, offset:0 10"
func (s *Service) AddPageNumbers(inputPath, outputPath, descriptor string) Result {
	conf := model.NewDefaultConfiguration()
	// $p = pdfcpu page-number token.
	// scale:1 abs prevents pdfcpu from scaling the text up to fill the page.
	// rotation:0 prevents the default 45° diagonal.
	desc := descriptor + ", scale:1 abs, rotation:0"
	if err := api.AddTextWatermarksFile(inputPath, outputPath, nil, true, "$p", desc, conf); err != nil {
		return Result{Error: fmt.Sprintf("add page numbers failed: %v", err)}
	}
	return Result{OutputPath: outputPath}
}

// AddWatermarkText adds a diagonal "DRAFT" / "CONFIDENTIAL" style watermark.
func (s *Service) AddWatermarkText(inputPath, outputPath, text string) Result {
	conf := model.NewDefaultConfiguration()
	descriptor := fmt.Sprintf("font:Helvetica, points:48, color:#CCCCCC, rotation:45, opacity:0.3, position:c, scale:1.0 rel")
	if err := api.AddTextWatermarksFile(inputPath, outputPath, nil, false, text, descriptor, conf); err != nil {
		return Result{Error: fmt.Sprintf("add watermark failed: %v", err)}
	}
	return Result{OutputPath: outputPath}
}

// ExtractPageText returns all positioned text spans from the given page.
// This is the foundation for true text editing — once we know where each
// span lives in the content stream we can replace it in place.
func (s *Service) ExtractPageText(inputPath string, pageNum int) ([]TextSpan, error) {
	return ExtractText(inputPath, pageNum)
}

// DebugPageStream returns the raw decoded content stream for a page as a
// string. Use this to inspect what the PDF actually contains before editing.
func (s *Service) DebugPageStream(inputPath string, pageNum int) (string, error) {
	f, err := os.Open(inputPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	conf := model.NewDefaultConfiguration()
	ctx, err := api.ReadValidateAndOptimize(f, conf)
	if err != nil {
		return "", err
	}

	r, err := pdfcpupkg.ExtractPageContent(ctx, pageNum)
	if err != nil {
		return "", err
	}
	if r == nil {
		return "(nil reader — page has no content stream)", nil
	}

	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}

	// Show first 4000 bytes to keep the response manageable
	if len(b) > 4000 {
		return string(b[:4000]) + "\n\n... truncated ...", nil
	}
	return string(b), nil
}
