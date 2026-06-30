package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vclaw/internal/providers"
)

func sandboxExtractPDFLocalPathObservation(toolCall providers.ToolCall) string {
	if strings.TrimSpace(toolCall.Name) != "sandbox.extractPDF" {
		return ""
	}
	localPath := stringArgument(toolCall.Arguments, "localPath")
	if localPath == "" {
		return ""
	}
	workspaceDir := defaultSandboxWorkspaceDir()
	if workspaceDir == "" {
		return ""
	}
	resolvedPath, err := resolveSandboxPreflightPath(workspaceDir, localPath)
	if err != nil {
		return sandboxExtractPDFPathObservation(localPath, err.Error())
	}
	workspaceAbs, err := filepath.Abs(workspaceDir)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(workspaceAbs, resolvedPath)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return sandboxExtractPDFPathObservation(localPath, "path is outside the sandbox workspace")
	}
	if !strings.EqualFold(filepath.Ext(resolvedPath), ".pdf") {
		return sandboxExtractPDFPathObservation(localPath, "path is not a .pdf file")
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return sandboxExtractPDFPathObservation(localPath, err.Error())
	}
	if !info.Mode().IsRegular() {
		return sandboxExtractPDFPathObservation(localPath, "path is not a regular PDF file")
	}
	return ""
}

func resolveSandboxPreflightPath(workspaceDir, localPath string) (string, error) {
	path := strings.TrimSpace(localPath)
	if strings.HasPrefix(filepath.ToSlash(path), "/workspace/") {
		path = filepath.Join(workspaceDir, filepath.FromSlash(strings.TrimPrefix(filepath.ToSlash(path), "/workspace/")))
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(workspaceDir, path)
	}
	return filepath.Abs(path)
}

func sandboxExtractPDFPathObservation(localPath, reason string) string {
	return fmt.Sprintf(`NEEDS_LOCAL_PDF_FILE: sandbox.extractPDF requires localPath to reference an existing PDF file inside the sandbox workspace, but %q is not available (%s).
Do not invent a local path from a Google Drive filename.
If the PDF comes from Google Drive, call drive.saveFile with the resolved Drive fileId first, then retry sandbox.extractPDF using the exact Path returned by drive.saveFile.
If the PDF comes from Gmail or Telegram, use the exact downloaded/attachment host path from the current tool result or attachment context.`, localPath, reason)
}
