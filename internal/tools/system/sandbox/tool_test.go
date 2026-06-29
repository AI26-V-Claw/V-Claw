package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sandboxruntime "vclaw/internal/sandbox/runtime"
	"vclaw/internal/tools"
)

type fakeRunner struct {
	pythonReq *sandboxruntime.RunPythonRequest
	shellReq  *sandboxruntime.RunShellRequest
	result    *sandboxruntime.JobResult
}

type extractPDFRunner struct {
	request *sandboxruntime.RunPythonRequest
}

func (r *extractPDFRunner) RunPython(_ context.Context, req *sandboxruntime.RunPythonRequest) (*sandboxruntime.JobResult, error) {
	r.request = req
	if err := os.WriteFile(filepath.Join(req.WorkspaceDir, "sample_structured.md"), []byte("# Sample\n\n## Table\n"), 0o600); err != nil {
		return nil, err
	}
	return &sandboxruntime.JobResult{
		RequestID: req.RequestID,
		JobID:     "job-extract-pdf",
		Status:    sandboxruntime.JobSuccess,
		ExitCode:  0,
		Stdout:    `{"pages":1,"tables":1,"characters":20}`,
	}, nil
}

func (r *extractPDFRunner) RunShell(context.Context, *sandboxruntime.RunShellRequest) (*sandboxruntime.JobResult, error) {
	return nil, nil
}

func (f *fakeRunner) RunPython(_ context.Context, req *sandboxruntime.RunPythonRequest) (*sandboxruntime.JobResult, error) {
	f.pythonReq = req
	if f.result != nil {
		return f.result, nil
	}
	return &sandboxruntime.JobResult{
		RequestID: req.RequestID,
		JobID:     "job-python",
		Status:    sandboxruntime.JobSuccess,
		ExitCode:  0,
		Stdout:    "hello python\n",
		Artifacts: []string{},
	}, nil
}

func (f *fakeRunner) RunShell(_ context.Context, req *sandboxruntime.RunShellRequest) (*sandboxruntime.JobResult, error) {
	f.shellReq = req
	if f.result != nil {
		return f.result, nil
	}
	return &sandboxruntime.JobResult{
		RequestID: req.RequestID,
		JobID:     "job-shell",
		Status:    sandboxruntime.JobSuccess,
		ExitCode:  0,
		Stdout:    "hello shell\n",
		Artifacts: []string{},
	}, nil
}

func TestRegisterToolsMetadataRequiresApproval(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry); err != nil {
		t.Fatalf("RegisterTools() error = %v", err)
	}

	for _, name := range []string{ToolNameRunPython, ToolNameRunShell} {
		definition, ok := registry.GetDefinition(name)
		if !ok {
			t.Fatalf("expected tool definition for %s", name)
		}
		if definition.RiskLevel != tools.RiskLevelCodeExecution {
			t.Fatalf("expected code_execution risk for %s, got %q", name, definition.RiskLevel)
		}
		if !definition.RequiresApproval {
			t.Fatalf("expected %s to require approval", name)
		}
	}
	extract, ok := registry.GetDefinition(ToolNameExtractPDF)
	if !ok {
		t.Fatalf("expected tool definition for %s", ToolNameExtractPDF)
	}
	if extract.RiskLevel != tools.RiskLevelLocalWrite || !extract.RequiresApproval {
		t.Fatalf("unexpected extract PDF policy: risk=%s approval=%t", extract.RiskLevel, extract.RequiresApproval)
	}
}

func TestExtractPDFToolProducesStructuredMarkdownArtifact(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "sample.pdf")
	if err := os.WriteFile(inputPath, []byte("%PDF-test"), 0o600); err != nil {
		t.Fatalf("write PDF fixture: %v", err)
	}
	runner := &extractPDFRunner{}
	tool := NewExtractPDFTool(Config{Runner: runner, DefaultWorkspaceDir: workspace})

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "call_extract_pdf",
		Name: ToolNameExtractPDF,
		Arguments: map[string]any{
			"localPath":  inputPath,
			"outputFile": "sample_structured.md",
		},
	})

	if !result.Success {
		t.Fatalf("extract PDF failed: %#v", result.Error)
	}
	if runner.request == nil || !strings.Contains(runner.request.Code, "find_tables()") {
		t.Fatalf("expected deterministic table extraction script, got %#v", runner.request)
	}
	if !strings.Contains(runner.request.Code, `/workspace/sample.pdf`) {
		t.Fatalf("script does not use workspace PDF path:\n%s", runner.request.Code)
	}
	if result.ArtifactRef == nil || result.ArtifactRef.Label != "sample_structured.md" {
		t.Fatalf("unexpected Markdown artifact: %#v", result.ArtifactRef)
	}
	if result.Metadata["format"] != "markdown" || result.Metadata["tables"] != float64(1) {
		t.Fatalf("unexpected extraction metadata: %#v", result.Metadata)
	}
}

func TestExtractPDFToolRejectsPathOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outsidePath := filepath.Join(t.TempDir(), "outside.pdf")
	if err := os.WriteFile(outsidePath, []byte("%PDF-test"), 0o600); err != nil {
		t.Fatalf("write outside PDF: %v", err)
	}
	runner := &extractPDFRunner{}
	tool := NewExtractPDFTool(Config{Runner: runner, DefaultWorkspaceDir: workspace})
	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:        "call_extract_outside",
		Name:      ToolNameExtractPDF,
		Arguments: map[string]any{"localPath": outsidePath},
	})
	if result.Success || result.Error == nil || result.Error.Code != tools.ErrorInvalidArgument {
		t.Fatalf("expected outside path rejection, got %#v", result)
	}
	if runner.request != nil {
		t.Fatal("runner must not execute for an outside PDF")
	}
}

func TestRunPythonToolParametersAreOpenAICompatible(t *testing.T) {
	schema := NewRunPythonTool(Config{}).Parameters()

	if got := schema["type"]; got != "object" {
		t.Fatalf("expected object schema, got %#v", got)
	}
	for _, unsupported := range []string{"oneOf", "anyOf", "allOf", "enum", "not"} {
		if _, ok := schema[unsupported]; ok {
			t.Fatalf("schema must not expose top-level %q: %#v", unsupported, schema)
		}
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %#v", schema["properties"])
	}
	for _, name := range []string{"code", "script_path"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("expected %q property in schema: %#v", name, properties)
		}
	}
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("expected required fields, got %#v", schema["required"])
	}
	if len(required) != 1 || required[0] != "code" {
		t.Fatalf("expected code to be the only required field, got %#v", required)
	}
}

func TestRunPythonToolDescriptionBoundsPDFOutput(t *testing.T) {
	description := NewRunPythonTool(Config{}).Description()
	for _, want := range []string{
		"do not print the full extracted text",
		"under 4000 characters",
		"short page/section snippets or chunks",
		"write it to a workspace file",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("runPython description missing %q: %s", want, description)
		}
	}
}

func TestRunPythonToolExecutesConfiguredRunner(t *testing.T) {
	runner := &fakeRunner{}
	tool := NewRunPythonTool(Config{
		Runner:              runner,
		DefaultWorkspaceDir: "/tmp/workspace",
		DefaultSessionID:    "sess_test",
	})

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "call_python",
		Name: ToolNameRunPython,
		Arguments: map[string]any{
			"code":            "print('hello')",
			"timeout_seconds": 7,
		},
	})

	if !result.Success {
		t.Fatalf("expected success, got %#v", result)
	}
	if runner.pythonReq == nil {
		t.Fatal("expected runner RunPython call")
	}
	if runner.pythonReq.Code != "print('hello')" {
		t.Fatalf("Code = %q", runner.pythonReq.Code)
	}
	if runner.pythonReq.WorkspaceDir != "/tmp/workspace" {
		t.Fatalf("WorkspaceDir = %q", runner.pythonReq.WorkspaceDir)
	}
	if runner.pythonReq.SessionID != "sess_test" {
		t.Fatalf("SessionID = %q", runner.pythonReq.SessionID)
	}
	if runner.pythonReq.Timeout.Seconds() != 7 {
		t.Fatalf("Timeout = %s", runner.pythonReq.Timeout)
	}
}

func TestRunPythonToolSupportsScriptPath(t *testing.T) {
	runner := &fakeRunner{}
	tool := NewRunPythonTool(Config{Runner: runner, DefaultWorkspaceDir: "/tmp/workspace"})

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "call_script",
		Name: ToolNameRunPython,
		Arguments: map[string]any{
			"script_path": "scripts/job.py",
		},
	})

	if !result.Success {
		t.Fatalf("expected success, got %#v", result)
	}
	if runner.pythonReq.ScriptPath != "scripts/job.py" {
		t.Fatalf("ScriptPath = %q", runner.pythonReq.ScriptPath)
	}
}

func TestRunShellToolExecutesConfiguredRunner(t *testing.T) {
	runner := &fakeRunner{}
	tool := NewRunShellTool(Config{Runner: runner, DefaultWorkspaceDir: "/tmp/workspace"})

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "call_shell",
		Name: ToolNameRunShell,
		Arguments: map[string]any{
			"command": "ls -la",
		},
	})

	if !result.Success {
		t.Fatalf("expected success, got %#v", result)
	}
	if runner.shellReq == nil {
		t.Fatal("expected runner RunShell call")
	}
	if runner.shellReq.Command != "ls -la" {
		t.Fatalf("Command = %q", runner.shellReq.Command)
	}
	if runner.shellReq.WorkspaceDir != "/tmp/workspace" {
		t.Fatalf("WorkspaceDir = %q", runner.shellReq.WorkspaceDir)
	}
}

func TestRunPythonToolUsesSessionWorkspaceWhenGuardConfigured(t *testing.T) {
	root := t.TempDir()
	guard, err := sandboxruntime.NewWorkspaceGuard(root)
	if err != nil {
		t.Fatalf("NewWorkspaceGuard() error = %v", err)
	}

	runner := &fakeRunner{}
	tool := NewRunPythonTool(Config{
		Runner:           runner,
		Guard:            guard,
		DefaultSessionID: "fallback_session",
	})

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "call_session_workspace",
		Name: ToolNameRunPython,
		Arguments: map[string]any{
			"session_id":    "sess_123",
			"workspace_dir": filepath.Join(root, "ignored"),
			"code":          "print('hello')",
		},
	})

	if !result.Success {
		t.Fatalf("expected success, got %#v", result)
	}
	expected := filepath.Join(root, "sess_123", "workspace")
	if runner.pythonReq == nil {
		t.Fatal("expected runner RunPython call")
	}
	if runner.pythonReq.WorkspaceDir != expected {
		t.Fatalf("WorkspaceDir = %q, want %q", runner.pythonReq.WorkspaceDir, expected)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected session workspace to exist: %v", err)
	}
}

func TestRunShellToolRejectsInvalidSessionIDWhenGuardConfigured(t *testing.T) {
	root := t.TempDir()
	guard, err := sandboxruntime.NewWorkspaceGuard(root)
	if err != nil {
		t.Fatalf("NewWorkspaceGuard() error = %v", err)
	}

	runner := &fakeRunner{}
	tool := NewRunShellTool(Config{
		Runner: runner,
		Guard:  guard,
	})

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "call_invalid_session",
		Name: ToolNameRunShell,
		Arguments: map[string]any{
			"session_id": "bad/session",
			"command":    "ls -la",
		},
	})

	if result.Success {
		t.Fatalf("expected failure, got %#v", result)
	}
	if result.Error == nil || result.Error.Code != tools.ErrorInvalidArgument {
		t.Fatalf("expected invalid argument error, got %#v", result.Error)
	}
	if runner.shellReq != nil {
		t.Fatal("runner should not be called for invalid session ID")
	}
}
