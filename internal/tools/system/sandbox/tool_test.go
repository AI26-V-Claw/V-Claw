package sandbox

import (
	"context"
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
