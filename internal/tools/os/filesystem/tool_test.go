package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vclaw/internal/tools"
)

func tempWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Create test structure
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello world"), 0644)
	os.WriteFile(filepath.Join(dir, "data.csv"), []byte("a,b,c\n1,2,3"), 0644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)
	os.WriteFile(filepath.Join(dir, "subdir", "nested.go"), []byte("package main"), 0644)
	return dir
}

func TestListDirReturnsFiles(t *testing.T) {
	dir := tempWorkspace(t)
	guard := NewPathGuard([]string{dir})
	tool := NewListDirTool(guard)

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID: "call_1", Name: ToolNameListDir,
		Arguments: map[string]any{"path": dir},
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if !strings.Contains(result.ContentForLLM, "readme.txt") {
		t.Fatalf("expected readme.txt in output, got: %s", result.ContentForLLM)
	}
	if !strings.Contains(result.ContentForLLM, "subdir") {
		t.Fatalf("expected subdir in output, got: %s", result.ContentForLLM)
	}
}

func TestListDirWithPattern(t *testing.T) {
	dir := tempWorkspace(t)
	guard := NewPathGuard([]string{dir})
	tool := NewListDirTool(guard)

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID: "call_2", Name: ToolNameListDir,
		Arguments: map[string]any{"path": dir, "pattern": "*.txt"},
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if !strings.Contains(result.ContentForLLM, "readme.txt") {
		t.Fatalf("expected readme.txt in output")
	}
	if strings.Contains(result.ContentForLLM, "data.csv") {
		t.Fatalf("expected data.csv to be filtered out")
	}
}

func TestListDirRecursive(t *testing.T) {
	dir := tempWorkspace(t)
	guard := NewPathGuard([]string{dir})
	tool := NewListDirTool(guard)

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID: "call_3", Name: ToolNameListDir,
		Arguments: map[string]any{"path": dir, "recursive": true},
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if !strings.Contains(result.ContentForLLM, "nested.go") {
		t.Fatalf("expected nested.go in recursive output, got: %s", result.ContentForLLM)
	}
}

func TestReadFileReturnsContent(t *testing.T) {
	dir := tempWorkspace(t)
	guard := NewPathGuard([]string{dir})
	tool := NewReadFileTool(guard)

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID: "call_4", Name: ToolNameReadFile,
		Arguments: map[string]any{"path": filepath.Join(dir, "readme.txt")},
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if !strings.Contains(result.ContentForLLM, "hello world") {
		t.Fatalf("expected file content, got: %s", result.ContentForLLM)
	}
}

func TestReadFileWithLineRange(t *testing.T) {
	dir := tempWorkspace(t)
	guard := NewPathGuard([]string{dir})
	tool := NewReadFileTool(guard)

	// data.csv has 2 lines
	result := tool.Execute(context.Background(), tools.ToolCall{
		ID: "call_5", Name: ToolNameReadFile,
		Arguments: map[string]any{"path": filepath.Join(dir, "data.csv"), "start_line": 1, "end_line": 1},
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if !strings.Contains(result.ContentForLLM, "a,b,c") {
		t.Fatalf("expected first line, got: %s", result.ContentForLLM)
	}
}

func TestReadFileTruncatesLargeContent(t *testing.T) {
	dir := tempWorkspace(t)
	guard := NewPathGuard([]string{dir})
	tool := NewReadFileTool(guard)

	// Create a large file
	largeContent := strings.Repeat("x", maxReadFileChars+1000)
	os.WriteFile(filepath.Join(dir, "large.txt"), []byte(largeContent), 0644)

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID: "call_6", Name: ToolNameReadFile,
		Arguments: map[string]any{"path": filepath.Join(dir, "large.txt")},
	})

	if !result.Success {
		t.Fatalf("expected success")
	}
	if !strings.Contains(result.ContentForLLM, "[content truncated]") {
		t.Fatalf("expected truncation marker")
	}
}

func TestFileInfoReturnsMetadata(t *testing.T) {
	dir := tempWorkspace(t)
	guard := NewPathGuard([]string{dir})
	tool := NewFileInfoTool(guard)

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID: "call_7", Name: ToolNameFileInfo,
		Arguments: map[string]any{"path": filepath.Join(dir, "readme.txt")},
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if !strings.Contains(result.ContentForLLM, "Type: file") {
		t.Fatalf("expected file type metadata, got: %s", result.ContentForLLM)
	}
	if !strings.Contains(result.ContentForLLM, ".txt") {
		t.Fatalf("expected .txt extension")
	}
}

func TestWriteFileCreatesFile(t *testing.T) {
	dir := tempWorkspace(t)
	guard := NewPathGuard([]string{dir})
	tool := NewWriteFileTool(guard)

	target := filepath.Join(dir, "new_file.txt")
	result := tool.Execute(context.Background(), tools.ToolCall{
		ID: "call_8", Name: ToolNameWriteFile,
		Arguments: map[string]any{"path": target, "content": "new content", "mode": "create"},
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if string(data) != "new content" {
		t.Fatalf("expected 'new content', got %q", string(data))
	}
}

func TestWriteFileCreateFailsIfExists(t *testing.T) {
	dir := tempWorkspace(t)
	guard := NewPathGuard([]string{dir})
	tool := NewWriteFileTool(guard)

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID: "call_9", Name: ToolNameWriteFile,
		Arguments: map[string]any{"path": filepath.Join(dir, "readme.txt"), "content": "overwrite", "mode": "create"},
	})

	if result.Success {
		t.Fatal("expected create mode to fail for existing file")
	}
}

func TestWriteFileAppend(t *testing.T) {
	dir := tempWorkspace(t)
	guard := NewPathGuard([]string{dir})
	tool := NewWriteFileTool(guard)

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID: "call_10", Name: ToolNameWriteFile,
		Arguments: map[string]any{"path": filepath.Join(dir, "readme.txt"), "content": " appended", "mode": "append"},
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "readme.txt"))
	if string(data) != "hello world appended" {
		t.Fatalf("expected appended content, got %q", string(data))
	}
}

func TestPathTraversalBlocked(t *testing.T) {
	dir := tempWorkspace(t)
	guard := NewPathGuard([]string{dir})
	tool := NewReadFileTool(guard)

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID: "call_11", Name: ToolNameReadFile,
		Arguments: map[string]any{"path": filepath.Join(dir, "..", "..", "..", "etc", "passwd")},
	})

	if result.Success {
		t.Fatal("expected path traversal to be blocked")
	}
	if result.Error == nil {
		t.Fatal("expected error")
	}
}

func TestPathGuardBlocksOutsideWorkspace(t *testing.T) {
	dir := tempWorkspace(t)
	guard := NewPathGuard([]string{dir})

	// Use a sibling of the workspace — guaranteed absolute and outside on any OS
	outsidePath := filepath.Join(filepath.Dir(dir), "outside-workspace", "secret.txt")
	_, err := guard.Resolve(outsidePath)
	if err == nil {
		t.Fatal("expected error for path outside workspace")
	}
}

func TestRegisterToolsRegistersAllTools(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry, Config{}); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}

	defs := registry.ListTools()
	if len(defs) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(defs))
	}

	// Verify group
	for _, d := range defs {
		if d.Group != "filesystem" {
			t.Errorf("tool %s has group %q, expected filesystem", d.Name, d.Group)
		}
	}

	// Verify writeFile requires approval
	def, ok := registry.GetDefinition(ToolNameWriteFile)
	if !ok {
		t.Fatal("writeFile not found")
	}
	if !def.RequiresApproval {
		t.Fatal("writeFile should require approval")
	}
	if def.Capability != tools.CapabilityMutating {
		t.Fatalf("writeFile capability should be mutating, got %s", def.Capability)
	}

}
