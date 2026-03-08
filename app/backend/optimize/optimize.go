package optimize

import (
	"fmt"
	"os"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

type Service struct{}

func New() *Service { return &Service{} }

type OptimizeResult struct {
	OutputPath     string  `json:"outputPath"`
	OriginalBytes  int64   `json:"originalBytes"`
	OptimizedBytes int64   `json:"optimizedBytes"`
	ReductionPct   float64 `json:"reductionPct"`
	Error          string  `json:"error,omitempty"`
}

// Optimize compresses a PDF by deduplicating resources and recompressing streams.
func (s *Service) Optimize(inputPath, outputPath string) OptimizeResult {
	origInfo, err := os.Stat(inputPath)
	if err != nil {
		return OptimizeResult{Error: fmt.Sprintf("cannot stat input: %v", err)}
	}
	origSize := origInfo.Size()

	conf := model.NewDefaultConfiguration()
	// v0.7: optimization settings live directly on the conf
	conf.WriteObjectStream = true
	conf.WriteXRefStream = true

	if err := api.OptimizeFile(inputPath, outputPath, conf); err != nil {
		return OptimizeResult{Error: fmt.Sprintf("optimize failed: %v", err)}
	}

	target := outputPath
	if outputPath == "" {
		target = inputPath
	}

	newInfo, err := os.Stat(target)
	if err != nil {
		return OptimizeResult{OutputPath: target, OriginalBytes: origSize}
	}
	newSize := newInfo.Size()

	reduction := 0.0
	if origSize > 0 {
		reduction = float64(origSize-newSize) / float64(origSize) * 100
	}

	return OptimizeResult{
		OutputPath:     target,
		OriginalBytes:  origSize,
		OptimizedBytes: newSize,
		ReductionPct:   reduction,
	}
}

// RemoveMetadata strips XMP and document-level metadata.
func (s *Service) RemoveMetadata(inputPath, outputPath string) OptimizeResult {
	conf := model.NewDefaultConfiguration()
	if err := api.RemovePropertiesFile(inputPath, outputPath, nil, conf); err != nil {
		return OptimizeResult{Error: fmt.Sprintf("remove metadata failed: %v", err)}
	}
	return OptimizeResult{OutputPath: outputPath}
}

// Validate checks PDF spec compliance.
func (s *Service) Validate(inputPath string) (bool, string) {
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationStrict
	if err := api.ValidateFile(inputPath, conf); err != nil {
		return false, err.Error()
	}
	return true, ""
}
