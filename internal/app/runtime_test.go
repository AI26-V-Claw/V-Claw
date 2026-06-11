package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	sandboxruntime "vclaw/internal/sandbox/runtime"
)

type fakeSandboxRunner struct{}

type fakeRuntimeProvider struct{}

func (fakeRuntimeProvider) Chat(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "ok"},
	}, nil
}

func (fakeRuntimeProvider) Generate(context.Context, *providers.GenerateRequest) (*providers.GenerateResponse, error) {
	return &providers.GenerateResponse{Text: "summary"}, nil
}

func (fakeRuntimeProvider) Name() string { return "test" }
func (fakeRuntimeProvider) Close() error { return nil }

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

func TestNewAgentToolRegistryWebToolModes(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		apiKey    string
		wantTools bool
		wantError string
	}{
		{name: "auto skips missing key", mode: ToolModeAuto},
		{name: "required rejects missing key", mode: ToolModeRequired, wantError: "TAVILY_API_KEY"},
		{name: "auto registers with key", mode: ToolModeAuto, apiKey: "test-key", wantTools: true},
		{name: "off skips configured key", mode: ToolModeOff, apiKey: "test-key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, err := NewAgentToolRegistry(context.Background(), AgentRuntimeConfig{
				WebToolsMode: tt.mode,
				TavilyAPIKey: tt.apiKey,
			})
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("expected error containing %q, got %v", tt.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewAgentToolRegistry() error = %v", err)
			}
			_, hasSearch := registry.GetTool("web.search")
			_, hasFetch := registry.GetTool("web.fetch")
			if hasSearch != tt.wantTools || hasFetch != tt.wantTools {
				t.Fatalf("web tool registration mismatch: search=%t fetch=%t want=%t", hasSearch, hasFetch, tt.wantTools)
			}
		})
	}
}

func TestBuildRuntimeUsesFileBackedStoresByDefault(t *testing.T) {
	dataDir := t.TempDir()
	bundle, err := BuildRuntime(context.Background(), AgentRuntimeConfig{
		DataDir:         dataDir,
		Provider:        fakeRuntimeProvider{},
		GoogleToolsMode: ToolModeOff,
		WebToolsMode:    ToolModeOff,
	})
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}

	response, err := bundle.Runtime.Run(context.Background(), contracts.UserMessage{
		RequestID: "req-app-runtime",
		SessionID: "session-app-runtime",
		Channel:   "test",
		Text:      "hello",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("runtime.Run() error = %v", err)
	}
	if response.Message != "ok" {
		t.Fatalf("unexpected runtime response: %#v", response)
	}
	for _, path := range []string{
		filepath.Join(dataDir, "runtime-state.json"),
		filepath.Join(dataDir, "sessions", "session-app-runtime", "transcript.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected persisted runtime file %s: %v", path, err)
		}
	}
}
