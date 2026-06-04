package toolrouter_test

import (
	"context"
	"strings"
	"testing"

	"vclaw/internal/audit"
	"vclaw/internal/contracts"
	"vclaw/internal/policies"
	"vclaw/internal/safety"
	"vclaw/internal/sandbox/gate"
	"vclaw/internal/sandbox/runtime"
	"vclaw/internal/toolrouter"
)

type stubRunner struct {
	result *runtime.JobResult
	calls  int
}

func (s *stubRunner) RunPython(_ context.Context, req *runtime.RunPythonRequest) (*runtime.JobResult, error) {
	s.calls++
	if s.result != nil {
		return s.result, nil
	}
	return &runtime.JobResult{
		RequestID: req.RequestID,
		JobID:     "stub-py-job",
		Status:    runtime.JobSuccess,
		ExitCode:  0,
		Stdout:    "hello from python",
		Artifacts: []string{},
	}, nil
}

func (s *stubRunner) RunShell(_ context.Context, req *runtime.RunShellRequest) (*runtime.JobResult, error) {
	s.calls++
	if s.result != nil {
		return s.result, nil
	}
	return &runtime.JobResult{
		RequestID: req.RequestID,
		JobID:     "stub-sh-job",
		Status:    runtime.JobSuccess,
		ExitCode:  0,
		Stdout:    "stub shell output",
		Artifacts: []string{},
	}, nil
}

func newRouter(stub *stubRunner) *toolrouter.ToolRouter {
	gated := gate.NewGatedRunner(gate.Config{
		Checker:  policies.DefaultChecker,
		Detector: safety.DefaultScanner,
		Logger:   &audit.NopLogger{},
		Runner:   stub,
	})
	return toolrouter.New(toolrouter.Config{Runner: gated})
}

func call(toolName, workspaceDir string) contracts.ToolCall {
	return contracts.ToolCall{
		ToolCallID: "toolcall_" + strings.ReplaceAll(toolName, ".", "_"),
		RequestID:  "req_" + toolName,
		SessionID:  "sess_test",
		ToolName:   toolName,
		Input: map[string]any{
			"workspace_dir": workspaceDir,
		},
	}
}

func dataMap(t *testing.T, result contracts.ToolResult) map[string]any {
	t.Helper()
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %#v", result.Data)
	}
	return data
}

func TestRouter_UsesCanonicalToolCallAndToolResult(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	c := call("sandbox.runShell", "/tmp/ws")
	c.Input["command"] = "ls -la /workspace"
	result := router.Dispatch(context.Background(), c)

	if result.ToolCallID != c.ToolCallID {
		t.Fatalf("ToolCallID = %q, want %q", result.ToolCallID, c.ToolCallID)
	}
	if result.ToolName != c.ToolName {
		t.Fatalf("ToolName = %q, want %q", result.ToolName, c.ToolName)
	}
	if result.Success {
		t.Fatal("sandbox code execution must not succeed before approval")
	}
	if result.Error == nil || result.Error.Code != contracts.ErrorActionRequiresApproval {
		t.Fatalf("expected approval error, got %#v", result.Error)
	}
	data := dataMap(t, result)
	if data["status"] != "pending_approval" {
		t.Fatalf("status = %#v, want pending_approval", data["status"])
	}
	if data["policyDecision"] != string(contracts.RiskDecisionRequiresApproval) {
		t.Fatalf("policyDecision = %#v", data["policyDecision"])
	}
	if data["approvalId"] == "" {
		t.Fatal("approvalId must be set for pending approval")
	}
	if stub.calls != 0 {
		t.Fatalf("runner must not be called before approval, got %d", stub.calls)
	}
}

func TestRouter_RunPython_HeldBeforeStdout(t *testing.T) {
	stub := &stubRunner{result: &runtime.JobResult{
		JobID:     "job-stdout",
		Status:    runtime.JobSuccess,
		ExitCode:  0,
		Stdout:    "42\n",
		Artifacts: []string{},
	}}
	router := newRouter(stub)

	c := call("sandbox.runPython", "/tmp/ws")
	c.Input["code"] = "print(42)"
	result := router.Dispatch(context.Background(), c)

	if result.Success {
		t.Fatal("expected pending approval before execution")
	}
	data := dataMap(t, result)
	if data["status"] != "pending_approval" {
		t.Fatalf("status = %#v, want pending_approval", data["status"])
	}
	if _, ok := data["stdout"]; ok {
		t.Fatalf("stdout must not be present before approval: %#v", data)
	}
	if stub.calls != 0 {
		t.Fatalf("runner must not be called before approval, got %d", stub.calls)
	}
}

func TestRouter_RunPython_BlockedByPolicy(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	c := call("sandbox.runPython", "/tmp/ws")
	c.Input["code"] = "open('/workspace/.env').read()"
	result := router.Dispatch(context.Background(), c)

	if result.Success {
		t.Fatal("blocked request must not succeed")
	}
	if result.Error == nil || result.Error.Code != contracts.ErrorActionBlockedByPolicy {
		t.Fatalf("expected blocked error, got %#v", result.Error)
	}
	data := dataMap(t, result)
	if data["status"] != "blocked" {
		t.Fatalf("status = %#v, want blocked", data["status"])
	}
	if data["policyDecision"] != string(contracts.RiskDecisionBlock) {
		t.Fatalf("policyDecision = %#v", data["policyDecision"])
	}
	if reasons, ok := data["policyReasons"].([]string); !ok || len(reasons) == 0 {
		t.Fatalf("policyReasons must be non-empty, got %#v", data["policyReasons"])
	}
	if stub.calls != 0 {
		t.Fatal("runner must not be called when blocked")
	}
}

func TestRouter_RunShell_DestructiveNeedsApproval(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	c := call("sandbox.runShell", "/tmp/ws")
	c.Input["command"] = "rm -rf /workspace/temp"
	result := router.Dispatch(context.Background(), c)

	if result.Error == nil || result.Error.Code != contracts.ErrorActionRequiresApproval {
		t.Fatalf("expected approval error, got %#v", result.Error)
	}
	data := dataMap(t, result)
	if data["status"] != "pending_approval" {
		t.Fatalf("status = %#v, want pending_approval", data["status"])
	}
	if !strings.Contains(data["approvalId"].(string), c.RequestID) {
		t.Fatalf("approvalId should reference request ID, got %#v", data["approvalId"])
	}
	if stub.calls != 0 {
		t.Fatal("runner must not be called when approval is required")
	}
}

func TestRouter_UnknownTool_ReturnsContractError(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	c := call("unknown_tool", "/tmp/ws")
	result := router.Dispatch(context.Background(), c)

	if result.Success {
		t.Fatal("unknown tool must not succeed")
	}
	if result.Error == nil || result.Error.Code != contracts.ErrorToolNotFound {
		t.Fatalf("expected tool not found error, got %#v", result.Error)
	}
	if stub.calls != 0 {
		t.Fatal("runner must not be called for unknown tool")
	}
}

func TestRouter_MissingInput_ReturnsContractError(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	for _, tc := range []contracts.ToolCall{
		call("sandbox.runPython", "/tmp/ws"),
		call("sandbox.runShell", "/tmp/ws"),
	} {
		result := router.Dispatch(context.Background(), tc)
		if result.Success {
			t.Fatalf("%s missing input should not succeed", tc.ToolName)
		}
		if result.Error == nil || result.Error.Code != contracts.ErrorToolInputInvalid {
			t.Fatalf("%s expected input error, got %#v", tc.ToolName, result.Error)
		}
		data := dataMap(t, result)
		if data["status"] == "" {
			t.Fatalf("%s error data should include status: %#v", tc.ToolName, data)
		}
	}
	if stub.calls != 0 {
		t.Fatalf("runner should not be called on invalid input, got %d", stub.calls)
	}
}

func TestRouter_DataAlwaysHasArtifactsSlice(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	cases := []contracts.ToolCall{
		call("sandbox.runShell", "/tmp"),
		call("sandbox.runShell", "/tmp"),
		call("sandbox.runShell", "/tmp"),
	}
	cases[0].Input["command"] = "ls"
	cases[1].Input["command"] = "shutdown"
	cases[2].Input["command"] = "rm /workspace/a.csv"

	for _, tc := range cases {
		result := router.Dispatch(context.Background(), tc)
		data := dataMap(t, result)
		if data["artifacts"] == nil {
			t.Fatalf("artifacts must not be nil for %s: %#v", tc.RequestID, data)
		}
	}
}

func TestRouter_ToolName_CaseInsensitive(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	c := call("SANDBOX.RUNSHELL", "/tmp/ws")
	c.Input["command"] = "ls /workspace"
	result := router.Dispatch(context.Background(), c)

	if result.Error == nil || result.Error.Code != contracts.ErrorActionRequiresApproval {
		t.Fatalf("tool name should be case-insensitive and still require approval, got %#v", result.Error)
	}
}
