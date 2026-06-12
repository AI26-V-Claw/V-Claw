package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
)

func applyChannelToolDefaults(message contracts.UserMessage, toolCall providers.ToolCall) providers.ToolCall {
	if !strings.EqualFold(strings.TrimSpace(message.Channel), "telegram") {
		return toolCall
	}
	if toolCall.Name != "gmail.downloadAttachments" {
		return toolCall
	}
	if outputDir := strings.TrimSpace(stringArg(toolCall.Arguments, "outputDir")); outputDir != "" && filepath.IsAbs(outputDir) {
		return toolCall
	}

	outputDir, err := telegramDownloadOutputDir()
	if err != nil || strings.TrimSpace(outputDir) == "" {
		return toolCall
	}

	args := cloneArguments(toolCall.Arguments)
	if args == nil {
		args = map[string]any{}
	}
	args["outputDir"] = outputDir
	toolCall.Arguments = args
	return toolCall
}

func telegramDownloadOutputDir() (string, error) {
	homeDir := strings.TrimSpace(os.Getenv("HOME"))
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}

	downloadsDir := filepath.Join(homeDir, "Downloads")
	if info, err := os.Stat(downloadsDir); err == nil && info.IsDir() {
		return filepath.Join(downloadsDir, "Vclaw"), nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	return filepath.Join(homeDir, "Vclaw"), nil
}
