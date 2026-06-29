package sandbox

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sandboxruntime "vclaw/internal/sandbox/runtime"
	"vclaw/internal/tools"
)

var pdfIntegrationFile = flag.String("pdf-integration-file", "", "workspace PDF used by the opt-in Docker extraction test")

func TestExtractPDFWithDocker(t *testing.T) {
	if strings.TrimSpace(*pdfIntegrationFile) == "" {
		t.Skip("pass -pdf-integration-file to run Docker PDF extraction")
	}
	inputPath, err := filepath.Abs(*pdfIntegrationFile)
	if err != nil {
		t.Fatalf("resolve PDF integration file: %v", err)
	}
	workspaceDir := filepath.Dir(inputPath)
	for filepath.Base(workspaceDir) != "workspace" && filepath.Dir(workspaceDir) != workspaceDir {
		workspaceDir = filepath.Dir(workspaceDir)
	}
	if filepath.Base(workspaceDir) != "workspace" {
		t.Fatalf("integration PDF must be under agent/workspace/data/telegram_attachments: %s", inputPath)
	}
	workspaceRoot := filepath.Dir(filepath.Dir(workspaceDir))
	guard, err := sandboxruntime.NewWorkspaceGuard(workspaceRoot)
	if err != nil {
		t.Fatalf("create workspace guard: %v", err)
	}
	runner := sandboxruntime.NewDockerRunner(sandboxruntime.DockerRunnerConfig{Guard: guard})
	tool := NewExtractPDFTool(Config{Runner: runner, Guard: guard, DefaultSessionID: DefaultSessionID})
	outputName := "codex_genta_structured_review.md"

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "integration_extract_pdf",
		Name: ToolNameExtractPDF,
		Arguments: map[string]any{
			"localPath":  inputPath,
			"outputFile": outputName,
		},
	})
	if !result.Success {
		t.Fatalf("Docker PDF extraction failed: %#v\n%s", result.Error, result.ContentForLLM)
	}
	outputPath := filepath.Join(workspaceDir, outputName)
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read structured Markdown: %v", err)
	}
	markdown := string(data)
	for _, want := range []string{"# C program Cheat Sheet", "## Console Input/Output", "| Entry | Description |"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("structured Markdown missing %q:\n%s", want, markdown)
		}
	}
	if strings.ContainsAny(markdown, "\u200b\u200c\u200d\u2060\ufeff") {
		t.Fatal("structured Markdown still contains zero-width characters")
	}
	t.Logf("structured Markdown: %s (%d bytes)", outputPath, len(data))
}
