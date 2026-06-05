package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sandboxruntime "vclaw/internal/sandbox/runtime"
	"vclaw/internal/tools"
)

type fakeRunner struct {
	pythonReq *sandboxruntime.RunPythonRequest
	shellReq  *sandboxruntime.RunShellRequest
	result    *sandboxruntime.JobResult
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
