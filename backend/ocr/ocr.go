// Package ocr provides OCR services for scanned PDFs.
// Requires go-fitz (MuPDF) + gosseract (Tesseract) — both CGO packages.
// These stubs compile without any CGO dependencies.
// See README for installation instructions to enable full OCR.
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
	return OCRResult{Error: fmt.Sprintf("OCR not available: install go-fitz + gosseract (see README). Input: %s", inputPath)}
}

func (s *Service) OCRPage(inputPath string, pageNumber int) ([]byte, string) {
	return nil, fmt.Sprintf("OCR not available: install go-fitz + gosseract (see README)")
}
