package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
)

func applyChannelToolDefaults(_ contracts.UserMessage, toolCall providers.ToolCall) providers.ToolCall {
	switch toolCall.Name {
	case "gmail.downloadAttachments":
		outputDir, _ := toolCall.Arguments["outputDir"].(string)
		outputDir = strings.TrimSpace(outputDir)
		// Relative outputDir would be joined with workspace root by PathGuard, silently
		// creating a nested directory (e.g. ".sandbox-workspace/agent/workspace" becomes
		// workspace_root/.sandbox-workspace/agent/workspace). Clear it so the tool defaults
		// to workspace root. Absolute paths are kept and validated by PathGuard at execution.
		if outputDir != "" && !filepath.IsAbs(outputDir) {
			args := cloneArguments(toolCall.Arguments)
			delete(args, "outputDir")
			toolCall.Arguments = args
		}
	case "sandbox.runPython":
		code, _ := toolCall.Arguments["code"].(string)
		code = strings.TrimSpace(code)
		if code == "" {
			return toolCall
		}
		normalized := normalizeSandboxWorkspacePaths(code)
		if normalized != code {
			args := cloneArguments(toolCall.Arguments)
			args["code"] = normalized
			toolCall.Arguments = args
		}
	}
	return toolCall
}

func normalizeSandboxWorkspacePaths(code string) string {
	workspaceDir := defaultSandboxWorkspaceDir()
	if workspaceDir == "" {
		return code
	}
	candidates := []string{
		filepath.Clean(workspaceDir) + string(filepath.Separator),
		filepath.ToSlash(filepath.Clean(workspaceDir)) + "/",
	}
	normalized := code
	for _, prefix := range candidates {
		normalized = strings.ReplaceAll(normalized, prefix, "/workspace/")
	}
	return normalized
}

func defaultSandboxWorkspaceDir() string {
	workspaceRoot := strings.TrimSpace(os.Getenv("VCLAW_SANDBOX_WORKSPACE_DIR"))
	if workspaceRoot == "" {
		workspaceRoot = ".sandbox-workspace"
	}
	if !filepath.IsAbs(workspaceRoot) {
		abs, err := filepath.Abs(workspaceRoot)
		if err != nil {
			return ""
		}
		workspaceRoot = abs
	}
	return filepath.Join(workspaceRoot, "agent", "workspace")
}

func telegramDownloadOutputDir() (string, error) {
	homeDir := telegramHomeDir()

	downloadsDir := filepath.Join(homeDir, "Downloads")
	if info, err := os.Stat(downloadsDir); err == nil && info.IsDir() {
		return filepath.Join(downloadsDir, "Vclaw"), nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	return filepath.Join(homeDir, "Vclaw"), nil
}

func telegramHomeDir() string {
	if homeDir := strings.TrimSpace(os.Getenv("HOME")); homeDir != "" {
		return homeDir
	}
	if homeDir := strings.TrimSpace(os.Getenv("USERPROFILE")); homeDir != "" {
		return homeDir
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return homeDir
}
