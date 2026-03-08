// Package annotate provides PDF annotation services.
// NOTE: Full annotation support is pending pdfcpu API stabilization.
// These stubs compile cleanly and return graceful errors until implemented.
package annotate

import "fmt"

type Service struct{}

func New() *Service { return &Service{} }

type Result struct {
	OutputPath string `json:"outputPath"`
	Error      string `json:"error,omitempty"`
}

type AnnotationRequest struct {
	PageNumber int     `json:"pageNumber"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Width      float64 `json:"width"`
	Height     float64 `json:"height"`
	Content    string  `json:"content"`
	Author     string  `json:"author"`
	R          float64 `json:"r"`
	G          float64 `json:"g"`
	B          float64 `json:"b"`
}

func (s *Service) AddTextAnnotation(inputPath, outputPath string, req AnnotationRequest) Result {
	return Result{Error: fmt.Sprintf("not yet implemented: add annotation to page %d", req.PageNumber)}
}

func (s *Service) RemoveAllAnnotations(inputPath, outputPath, pageSelection string) Result {
	return Result{Error: "not yet implemented: remove annotations"}
}

func (s *Service) ListAnnotations(inputPath string) ([]map[string]interface{}, string) {
	return nil, "not yet implemented: list annotations"
}
