package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vclaw/internal/audit"
	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	sandboxgate "vclaw/internal/sandbox/gate"
	sandboxruntime "vclaw/internal/sandbox/runtime"
	"vclaw/internal/toolhooks"
)

type fakeSandboxRunner struct{}
type countingSandboxRunner struct {
	shellCalls int
}
type blockToolHooks struct{}
type recordingToolHooks struct {
	beforeCalls int
}

type fakeRuntimeProvider struct{}
type toolCallRuntimeProvider struct {
	responses []providers.ChatResponse
	index     int
}

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

func (p *toolCallRuntimeProvider) Chat(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
	response := p.responses[p.index]
	if p.index < len(p.responses)-1 {
		p.index++
	}
	return response, nil
}

func (*toolCallRuntimeProvider) Generate(context.Context, *providers.GenerateRequest) (*providers.GenerateResponse, error) {
	return &providers.GenerateResponse{Text: "summary"}, nil
}

func (*toolCallRuntimeProvider) Name() string { return "test-tool-call" }
func (*toolCallRuntimeProvider) Close() error { return nil }

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

func (r *countingSandboxRunner) RunPython(_ context.Context, req *sandboxruntime.RunPythonRequest) (*sandboxruntime.JobResult, error) {
	return &sandboxruntime.JobResult{
		RequestID: req.RequestID,
		JobID:     "job-python",
		Status:    sandboxruntime.JobSuccess,
		ExitCode:  0,
		Artifacts: []string{},
	}, nil
}

func (r *countingSandboxRunner) RunShell(_ context.Context, req *sandboxruntime.RunShellRequest) (*sandboxruntime.JobResult, error) {
	r.shellCalls++
	return &sandboxruntime.JobResult{
		RequestID: req.RequestID,
		JobID:     "job-shell",
		Status:    sandboxruntime.JobSuccess,
		ExitCode:  0,
		Artifacts: []string{},
	}, nil
}

func (blockToolHooks) BeforeTool(context.Context, toolhooks.PreToolInput) (toolhooks.PreToolResult, error) {
	return toolhooks.PreToolResult{
		Decision: toolhooks.DecisionBlock,
		Reason:   "blocked by test hook",
	}, nil
}

func (blockToolHooks) AfterTool(context.Context, toolhooks.PostToolInput) error {
	return nil
}

func (h *recordingToolHooks) BeforeTool(context.Context, toolhooks.PreToolInput) (toolhooks.PreToolResult, error) {
	h.beforeCalls++
	return toolhooks.PreToolResult{Decision: toolhooks.DecisionAllow}, nil
}

func (*recordingToolHooks) AfterTool(context.Context, toolhooks.PostToolInput) error {
	return nil
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

func TestNewSandboxToolConfigWrapsCustomRunnerWithHooks(t *testing.T) {
	runner := &countingSandboxRunner{}
	cfg, err := newSandboxToolConfig(AgentRuntimeConfig{
		SandboxRunner: runner,
		AuditLogger:   audit.NewMemoryLogger(),
		ToolHooks:     blockToolHooks{},
	}, blockToolHooks{})
	if err != nil {
		t.Fatalf("newSandboxToolConfig() error = %v", err)
	}

	_, err = cfg.Runner.RunShell(context.Background(), &sandboxruntime.RunShellRequest{
		RequestID:    "req-custom-runner",
		SessionID:    "sess-custom-runner",
		WorkspaceDir: t.TempDir(),
		Command:      "echo hello",
	})
	if err == nil {
		t.Fatal("expected gated runner to block request")
	}
	if runner.shellCalls != 0 {
		t.Fatalf("custom runner must not execute when hook blocks, calls=%d", runner.shellCalls)
	}
}

func TestNewSandboxToolConfigWrapsExistingGatedRunnerWithHooks(t *testing.T) {
	runner := &countingSandboxRunner{}
	innerGate := sandboxgate.NewGatedRunner(sandboxgate.Config{
		ToolHooks: toolhooks.NoopHooks{},
		Runner:    runner,
	})
	cfg, err := newSandboxToolConfig(AgentRuntimeConfig{
		SandboxRunner: innerGate,
		AuditLogger:   audit.NewMemoryLogger(),
		ToolHooks:     blockToolHooks{},
	}, blockToolHooks{})
	if err != nil {
		t.Fatalf("newSandboxToolConfig() error = %v", err)
	}

	_, err = cfg.Runner.RunShell(context.Background(), &sandboxruntime.RunShellRequest{
		RequestID:    "req-existing-gate",
		SessionID:    "sess-existing-gate",
		WorkspaceDir: t.TempDir(),
		Command:      "echo hello",
	})
	if err == nil {
		t.Fatal("expected gated runner to block request")
	}
	if runner.shellCalls != 0 {
		t.Fatalf("existing gated runner must still be wrapped, calls=%d", runner.shellCalls)
	}
}

func TestBuildRuntimeAppliesCustomHooksToAgentRuntime(t *testing.T) {
	hooks := &recordingToolHooks{}
	provider := &toolCallRuntimeProvider{
		responses: []providers.ChatResponse{
			{
				Message: providers.Message{
					Role: providers.MessageRoleAssistant,
					ToolCalls: []providers.ToolCall{{
						ID:   "call_time",
						Name: "get_current_time",
					}},
				},
			},
			{
				Message: providers.Message{
					Role:    providers.MessageRoleAssistant,
					Content: "done",
				},
			},
		},
	}

	bundle, err := BuildRuntime(context.Background(), AgentRuntimeConfig{
		DataDir:         t.TempDir(),
		Provider:        provider,
		GoogleToolsMode: ToolModeOff,
		WebToolsMode:    ToolModeOff,
		ToolHooks:       hooks,
	})
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}

	_, err = bundle.Runtime.Run(context.Background(), contracts.UserMessage{
		RequestID: "req-hooked-runtime",
		SessionID: "sess-hooked-runtime",
		Channel:   "test",
		Text:      "cho minh biet gio hien tai",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("runtime.Run() error = %v", err)
	}
	if hooks.beforeCalls == 0 {
		t.Fatal("expected custom runtime hook to run before tool execution")
	}
}
