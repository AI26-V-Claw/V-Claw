package toolrouter_test

import (
	"context"
	"strings"
	"testing"

	"vclaw/internal/audit"
	"vclaw/internal/policies"
	"vclaw/internal/safety"
	"vclaw/internal/sandbox/gate"
	"vclaw/internal/sandbox/runtime"
	"vclaw/internal/toolrouter"
)

// ─── Stub runner ─────────────────────────────────────────────────────────────

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

// ─── Router factory ───────────────────────────────────────────────────────────

func newRouter(stub *stubRunner) *toolrouter.ToolRouter {
	gated := gate.NewGatedRunner(gate.Config{
		Checker:  policies.DefaultChecker,
		Detector: safety.DefaultScanner,
		Logger:   &audit.NopLogger{},
		Runner:   stub,
	})
	return toolrouter.New(toolrouter.Config{Runner: gated})
}

func req(tool, workspaceDir string) toolrouter.ToolRequest {
	return toolrouter.ToolRequest{
		RequestID: "req_" + tool,
		SessionID: "sess_test",
		Tool:      tool,
		Input:     toolrouter.ToolInput{WorkspaceDir: workspaceDir},
		Context:   toolrouter.ToolContext{Source: "agent"},
	}
}

// ─── sandbox.runPython: requires approval ───────────────────────────────────────────

func TestRouter_RunPython_SafeCode_NeedsApproval(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	r := req("sandbox.runPython", "/tmp/ws")
	r.Input.Code = "print('hello')"
	resp := router.Dispatch(context.Background(), r)

	if resp.Status != "pending_approval" {
		t.Errorf("expected status 'pending_approval', got %q (err: %s)", resp.Status, resp.ErrorMessage)
	}
	if resp.JobID != "" {
		t.Error("JobID must not be set before approval")
	}
	if resp.PolicyDecision != "requires_approval" {
		t.Errorf("expected policy_decision 'requires_approval', got %q", resp.PolicyDecision)
	}
	if resp.ApprovalID == "" {
		t.Error("approval_id must be set for pending approval")
	}
	if stub.calls != 0 {
		t.Errorf("runner must not be called before approval, got %d", stub.calls)
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

	r := req("sandbox.runPython", "/tmp/ws")
	r.Input.Code = "print(42)"
	resp := router.Dispatch(context.Background(), r)

	if resp.Status != "pending_approval" {
		t.Fatalf("expected pending_approval before execution, got %q", resp.Status)
	}
	if resp.Stdout != "" {
		t.Errorf("stdout must be empty before approval, got %q", resp.Stdout)
	}
	if stub.calls != 0 {
		t.Errorf("runner must not be called before approval, got %d", stub.calls)
	}
}

// ─── sandbox.runPython: blocked ──────────────────────────────────────────────────────

func TestRouter_RunPython_BlockedByPolicy_Credential(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	r := req("sandbox.runPython", "/tmp/ws")
	r.Input.Code = "open('/workspace/.env').read()"
	resp := router.Dispatch(context.Background(), r)

	if resp.Status != "blocked" {
		t.Errorf("expected status 'blocked', got %q", resp.Status)
	}
	if resp.PolicyDecision != "block" {
		t.Errorf("expected policy_decision 'block', got %q", resp.PolicyDecision)
	}
	if resp.PolicyRiskLevel == "" {
		t.Error("policy_risk_level must be set on blocked response")
	}
	if len(resp.PolicyReasons) == 0 {
		t.Error("policy_reasons must not be empty for blocked response")
	}
	if stub.calls != 0 {
		t.Error("runner must NOT be called when blocked")
	}
	if resp.JobID != "" {
		t.Error("JobID must be empty when blocked")
	}
}

func TestRouter_RunPython_BlockedByPolicy_Shutdown(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	r := req("sandbox.runPython", "/tmp/ws")
	r.Input.Code = "import os; os.system('shutdown -h now')"
	resp := router.Dispatch(context.Background(), r)

	if resp.Status != "blocked" {
		t.Errorf("expected 'blocked', got %q", resp.Status)
	}
	if stub.calls != 0 {
		t.Error("runner must not be called when blocked")
	}
}

// ─── sandbox.runPython: requires_approval ───────────────────────────────────────────────

func TestRouter_RunPython_NeedsApproval_DeleteFile(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	r := req("sandbox.runPython", "/tmp/ws")
	r.Input.Code = "import os; os.remove('/workspace/output/old.csv')"
	resp := router.Dispatch(context.Background(), r)

	if resp.Status != "pending_approval" {
		t.Errorf("expected 'pending_approval', got %q", resp.Status)
	}
	if resp.PolicyDecision != "requires_approval" {
		t.Errorf("expected policy_decision 'requires_approval', got %q", resp.PolicyDecision)
	}
	if resp.ApprovalID == "" {
		t.Error("approval_id must be set for pending_approval response")
	}
	if resp.ApprovalSummaryVI == "" {
		t.Error("approval_summary_vi must be set for pending_approval response")
	}
	if stub.calls != 0 {
		t.Error("runner must NOT be called when requires_approval")
	}
}

// ─── sandbox.runShell: requires approval ────────────────────────────────────────────

func TestRouter_RunShell_SafeCommand_NeedsApproval(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	r := req("sandbox.runShell", "/tmp/ws")
	r.Input.Command = "ls -la /workspace"
	resp := router.Dispatch(context.Background(), r)

	if resp.Status != "pending_approval" {
		t.Errorf("expected 'pending_approval', got %q (err: %s)", resp.Status, resp.ErrorMessage)
	}
	if stub.calls != 0 {
		t.Errorf("runner must not be called before approval, got %d", stub.calls)
	}
}

func TestRouter_RunShell_SafeCommand_HeadNeedsApproval(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	r := req("sandbox.runShell", "/tmp/ws")
	r.Input.Command = "head -10 /workspace/data.csv"
	resp := router.Dispatch(context.Background(), r)

	if resp.Status != "pending_approval" {
		t.Errorf("expected 'pending_approval', got %q", resp.Status)
	}
	if stub.calls != 0 {
		t.Errorf("runner must not be called before approval, got %d", stub.calls)
	}
}

// ─── sandbox.runShell: blocked ───────────────────────────────────────────────────────

func TestRouter_RunShell_BlockedByPolicy_Shutdown(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	r := req("sandbox.runShell", "/tmp/ws")
	r.Input.Command = "shutdown -h now"
	resp := router.Dispatch(context.Background(), r)

	if resp.Status != "blocked" {
		t.Errorf("expected 'blocked', got %q", resp.Status)
	}
	if stub.calls != 0 {
		t.Error("runner must not be called when blocked")
	}
}

func TestRouter_RunShell_BlockedByPolicy_Credential(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	r := req("sandbox.runShell", "/tmp/ws")
	r.Input.Command = "cat /root/.ssh/id_rsa"
	resp := router.Dispatch(context.Background(), r)

	if resp.Status != "blocked" {
		t.Errorf("expected 'blocked', got %q", resp.Status)
	}
}

func TestRouter_RunShell_BlockedByPolicy_Sudo(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	r := req("sandbox.runShell", "/tmp/ws")
	r.Input.Command = "sudo rm -rf /"
	resp := router.Dispatch(context.Background(), r)

	if resp.Status != "blocked" {
		t.Errorf("expected 'blocked', got %q", resp.Status)
	}
}

// ─── sandbox.runShell: requires_approval ────────────────────────────────────────────────

func TestRouter_RunShell_NeedsApproval_RmRf(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	r := req("sandbox.runShell", "/tmp/ws")
	r.Input.Command = "rm -rf /workspace/temp"
	resp := router.Dispatch(context.Background(), r)

	if resp.Status != "pending_approval" {
		t.Errorf("expected 'pending_approval', got %q", resp.Status)
	}
	if resp.ApprovalID == "" {
		t.Error("approval_id must be set")
	}
	if !strings.Contains(resp.ApprovalID, "req_sandbox.runShell") {
		t.Errorf("approval_id should reference request ID, got %q", resp.ApprovalID)
	}
}

func TestRouter_RunShell_NeedsApproval_Curl(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	r := req("sandbox.runShell", "/tmp/ws")
	r.Input.Command = "curl https://api.example.com/data"
	resp := router.Dispatch(context.Background(), r)

	if resp.Status != "pending_approval" {
		t.Errorf("expected 'pending_approval', got %q", resp.Status)
	}
}

// ─── Unknown tool ─────────────────────────────────────────────────────────────

func TestRouter_UnknownTool_ReturnsError(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	r := req("unknown_tool", "/tmp/ws")
	r.Input.Command = "list"
	resp := router.Dispatch(context.Background(), r)

	if resp.Status != "error" {
		t.Errorf("expected 'error' for unknown tool, got %q", resp.Status)
	}
	if resp.ErrorMessage == "" {
		t.Error("error_message must be set for unknown tool")
	}
	if stub.calls != 0 {
		t.Error("runner must not be called for unknown tool")
	}
}

// ─── Input validation ─────────────────────────────────────────────────────────

func TestRouter_MissingCode_ReturnsError(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	r := req("sandbox.runPython", "/tmp/ws")
	// Code and ScriptPath both empty → validation error
	resp := router.Dispatch(context.Background(), r)

	if resp.Status == "success" {
		t.Error("missing code should not produce success")
	}
	if stub.calls != 0 {
		t.Errorf("runner should not be called on invalid input, got %d calls", stub.calls)
	}
}

func TestRouter_MissingCommand_ReturnsError(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	r := req("sandbox.runShell", "/tmp/ws")
	// Command empty → validation error
	resp := router.Dispatch(context.Background(), r)

	if resp.Status == "success" {
		t.Error("missing command should not produce success")
	}
	if stub.calls != 0 {
		t.Errorf("runner should not be called on invalid input, got %d calls", stub.calls)
	}
}

// ─── Response fields completeness ─────────────────────────────────────────────

func TestRouter_Response_AlwaysHasArtifactsSlice(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	cases := []toolrouter.ToolRequest{
		{RequestID: "r1", SessionID: "s", Tool: "sandbox.runShell",
			Input: toolrouter.ToolInput{WorkspaceDir: "/tmp", Command: "ls"}},
		{RequestID: "r2", SessionID: "s", Tool: "sandbox.runShell",
			Input: toolrouter.ToolInput{WorkspaceDir: "/tmp", Command: "shutdown"}},
		{RequestID: "r3", SessionID: "s", Tool: "sandbox.runShell",
			Input: toolrouter.ToolInput{WorkspaceDir: "/tmp", Command: "rm /workspace/a.csv"}},
		{RequestID: "r4", SessionID: "s", Tool: "unknown_tool"},
	}

	for _, tc := range cases {
		tc := tc
		resp := router.Dispatch(context.Background(), tc)
		if resp.Artifacts == nil {
			t.Errorf("Artifacts must never be nil (req %s, status %s)", tc.RequestID, resp.Status)
		}
		if resp.RequestID != tc.RequestID {
			t.Errorf("RequestID mismatch: want %q got %q", tc.RequestID, resp.RequestID)
		}
	}
}

// ─── Blocked response has no execution fields ─────────────────────────────────

func TestRouter_BlockedResponse_NoExecutionFields(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	r := req("sandbox.runShell", "/tmp/ws")
	r.Input.Command = "shutdown -h now"
	resp := router.Dispatch(context.Background(), r)

	if resp.JobID != "" {
		t.Errorf("blocked response should not have JobID, got %q", resp.JobID)
	}
	if resp.ExitCode != 0 {
		t.Errorf("blocked response should have ExitCode=0, got %d", resp.ExitCode)
	}
	if resp.Stdout != "" || resp.Stderr != "" {
		t.Error("blocked response should not have stdout/stderr")
	}
}

// ─── Case insensitivity ───────────────────────────────────────────────────────

func TestRouter_ToolName_CaseInsensitive(t *testing.T) {
	stub := &stubRunner{}
	router := newRouter(stub)

	r := req("SANDBOX.RUNSHELL", "/tmp/ws")
	r.Input.Command = "ls /workspace"
	resp := router.Dispatch(context.Background(), r)

	if resp.Status != "pending_approval" {
		t.Errorf("tool name should be case-insensitive and still require approval, got status=%q", resp.Status)
	}
}
