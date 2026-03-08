package security

import (
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

type Service struct{}

func New() *Service { return &Service{} }

type Result struct {
	OutputPath string `json:"outputPath"`
	Error      string `json:"error,omitempty"`
}

// PermissionLevel maps to pdfcpu's permission hex values.
// See: https://pdfcpu.io config — 0xF0C3=none, 0xF8C7=print only, 0xFFFF=all
type PermissionLevel string

const (
	PermissionsNone  PermissionLevel = "none"  // 0xF0C3 — no user permissions
	PermissionsPrint PermissionLevel = "print" // 0xF8C7 — print only
	PermissionsAll   PermissionLevel = "all"   // 0xFFFF — full access
)

// Encrypt protects a PDF with AES-256 encryption.
// ownerPassword: full access password
// userPassword:  restricted open password (can be empty for open-but-restricted)
// permissions:   "none", "print", or "all"
func (s *Service) Encrypt(inputPath, outputPath, ownerPassword, userPassword string, permissions PermissionLevel) Result {
	conf := model.NewDefaultConfiguration()
	conf.EncryptUsingAES = true
	conf.EncryptKeyLength = 256
	conf.OwnerPW = ownerPassword
	conf.UserPW = userPassword

	// pdfcpu v0.7 uses raw int16 bitmask for permissions — no named constants
	switch permissions {
	case PermissionsAll:
		conf.Permissions = model.PermissionFlags(0xFFFF)
	case PermissionsPrint:
		conf.Permissions = model.PermissionFlags(0xF8C7)
	default: // none
		conf.Permissions = model.PermissionFlags(0xF0C3)
	}

	if err := api.EncryptFile(inputPath, outputPath, conf); err != nil {
		return Result{Error: fmt.Sprintf("encrypt failed: %v", err)}
	}
	return Result{OutputPath: outputPath}
}

// Decrypt removes password protection from a PDF.
func (s *Service) Decrypt(inputPath, outputPath, password string) Result {
	conf := model.NewDefaultConfiguration()
	conf.OwnerPW = password
	conf.UserPW = password
	if err := api.DecryptFile(inputPath, outputPath, conf); err != nil {
		return Result{Error: fmt.Sprintf("decrypt failed: %v", err)}
	}
	return Result{OutputPath: outputPath}
}

// ChangePassword updates the owner password on an encrypted PDF.
func (s *Service) ChangePassword(inputPath, outputPath, currentOwnerPW, newOwnerPW string) Result {
	conf := model.NewDefaultConfiguration()
	conf.OwnerPW = currentOwnerPW
	if err := api.ChangeOwnerPasswordFile(inputPath, outputPath, currentOwnerPW, newOwnerPW, conf); err != nil {
		return Result{Error: fmt.Sprintf("password change failed: %v", err)}
	}
	return Result{OutputPath: outputPath}
}
