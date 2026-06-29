package filesafety

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type QuarantinedFile struct {
	Path     string
	Dir      string
	Filename string
}

func QuarantineDir(workspaceDir string) string {
	return filepath.Join(workspaceDir, ".quarantine")
}

func QuarantineBytes(workspaceDir string, filename string, data []byte) (QuarantinedFile, error) {
	dir := QuarantineDir(workspaceDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return QuarantinedFile{}, err
	}
	name := filepath.Base(filepath.FromSlash(strings.TrimSpace(filename)))
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "attachment.bin"
	}
	token, err := randomToken()
	if err != nil {
		return QuarantinedFile{}, err
	}
	path := filepath.Join(dir, token+"-"+name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return QuarantinedFile{}, err
	}
	return QuarantinedFile{Path: path, Dir: dir, Filename: name}, nil
}

func Promote(q QuarantinedFile, destPath string, decision Result) error {
	if !decision.Allowed() {
		return fmt.Errorf("file safety decision is %s: %s", decision.Decision, decision.ReasonUser)
	}
	return promote(q, destPath)
}

func PromoteTransfer(q QuarantinedFile, destPath string, decision Result) error {
	if !decision.TransferAllowed() {
		return fmt.Errorf("file safety decision is %s: %s", decision.Decision, decision.ReasonUser)
	}
	return promote(q, destPath)
}

func promote(q QuarantinedFile, destPath string) error {
	if strings.TrimSpace(q.Path) == "" {
		return fmt.Errorf("quarantine path is required")
	}
	if strings.TrimSpace(destPath) == "" {
		return fmt.Errorf("destination path is required")
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
		return err
	}
	if err := os.Rename(q.Path, destPath); err == nil {
		return nil
	}
	data, err := os.ReadFile(q.Path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(destPath, data, 0o600); err != nil {
		return err
	}
	return os.Remove(q.Path)
}

func randomToken() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
