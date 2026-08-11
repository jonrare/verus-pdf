// Package ocr provides OCR services for scanned PDFs.
//
// NOT IMPLEMENTED. Every method here is a stub that returns an error. A real
// implementation needs go-fitz (MuPDF) for page rasterisation and gosseract
// (Tesseract) for recognition — both CGO packages, which is why they are not
// wired up yet: adding them makes the build require a C toolchain and native
// libraries on all three target platforms.
package ocr

import "fmt"

type Service struct{}

func New() *Service { return &Service{} }

type OCRResult struct {
	OutputPath string    `json:"outputPath"`
	Pages      []PageOCR `json:"pages,omitempty"`
	FullText   string    `json:"fullText,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type PageOCR struct {
	PageNumber int    `json:"pageNumber"`
	Text       string `json:"text"`
}

func (s *Service) IsInstalled() bool {
	return false
}

func (s *Service) OCRDocument(inputPath, outputPath string, lang string) OCRResult {
	return OCRResult{Error: fmt.Sprintf("OCR not available: requires go-fitz + gosseract, which are not built in. Input: %s", inputPath)}
}

func (s *Service) OCRPage(inputPath string, pageNumber int) ([]byte, string) {
	return nil, fmt.Sprintf("OCR not available: requires go-fitz + gosseract, which are not built in")
}
