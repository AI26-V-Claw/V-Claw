package app

import (
	"context"
	"testing"

	sandboxruntime "vclaw/internal/sandbox/runtime"
)

type fakeSandboxRunner struct{}

func (fakeSandboxRunner) RunPython(_ context.Context, req *sandboxruntime.RunPythonRequest) (*sandboxruntime.JobResult, error) {
	return &sandboxruntime.JobResult{
		RequestID: req.RequestID,
		JobID:     "job-python",
		Status:    sandboxruntime.JobSuccess,
		ExitCode:  0,
		Artifacts: []string{},
	}, nil
}

func (fakeSandboxRunner) RunShell(_ context.Context, req *sandboxruntime.RunShellRequest) (*sandboxruntime.JobResult, error) {
	return &sandboxruntime.JobResult{
		RequestID: req.RequestID,
		JobID:     "job-shell",
		Status:    sandboxruntime.JobSuccess,
		ExitCode:  0,
		Artifacts: []string{},
	}, nil
}

func TestNewAgentToolRegistryRegistersBuiltInsByDefault(t *testing.T) {
	registry, err := NewAgentToolRegistry(context.Background(), AgentRuntimeConfig{})
	if err != nil {
		t.Fatalf("NewAgentToolRegistry() error = %v", err)
	}

	if _, ok := registry.GetTool("calculator"); !ok {
		t.Fatal("expected calculator tool")
	}
	if _, ok := registry.GetTool("get_current_time"); !ok {
		t.Fatal("expected get_current_time tool")
	}
	if _, ok := registry.GetTool("calendar.listEvents"); ok {
		t.Fatal("google tools should not be registered by default")
	}
	if _, ok := registry.GetTool("sandbox.runPython"); ok {
		t.Fatal("sandbox tools should not be registered by default")
	}
}

func TestNewAgentToolRegistryRegistersSandboxToolsWhenEnabled(t *testing.T) {
	registry, err := NewAgentToolRegistry(context.Background(), AgentRuntimeConfig{
		EnableSandboxTools:  true,
		SandboxRunner:       fakeSandboxRunner{},
		SandboxWorkspaceDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewAgentToolRegistry() error = %v", err)
	}

	if _, ok := registry.GetTool("sandbox.runPython"); !ok {
		t.Fatal("expected sandbox.runPython tool")
	}
	if _, ok := registry.GetTool("sandbox.runShell"); !ok {
		t.Fatal("expected sandbox.runShell tool")
	}
}
