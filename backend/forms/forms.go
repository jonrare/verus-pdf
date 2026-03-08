// Package forms provides PDF form filling services.
// NOTE: Full form fill support is pending pdfcpu API stabilization.
// These stubs compile cleanly and return graceful errors until implemented.
package forms

import "fmt"

type Service struct{}

func New() *Service { return &Service{} }

type Result struct {
	OutputPath string `json:"outputPath"`
	Error      string `json:"error,omitempty"`
}

type FormField struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

func (s *Service) GetFormFields(inputPath string) ([]FormField, string) {
	return nil, fmt.Sprintf("not yet implemented: list fields in %s", inputPath)
}

func (s *Service) FillFormFields(inputPath, outputPath string, values map[string]string) Result {
	return Result{Error: fmt.Sprintf("not yet implemented: fill %d fields", len(values))}
}

func (s *Service) LockForm(inputPath, outputPath string) Result {
	return Result{Error: "not yet implemented: lock form"}
}

func (s *Service) ResetForm(inputPath, outputPath string) Result {
	return Result{Error: "not yet implemented: reset form"}
}
