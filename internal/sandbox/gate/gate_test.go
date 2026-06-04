package gate_test

import (
	"context"
	"errors"
	"testing"

	"vclaw/internal/audit"
	"vclaw/internal/policies"
	"vclaw/internal/safety"
	"vclaw/internal/sandbox/gate"
	"vclaw/internal/sandbox/runtime"
)

// ─── Stub runner ─────────────────────────────────────────────────────────────

// stubRunner is a mock runtime.Runner for unit testing.
// It does not start Docker; it returns a pre-configured result or error.
type stubRunner struct {
	result *runtime.JobResult
	err    error
	calls  int // number of times RunPython or RunShell was called
}

func (s *stubRunner) RunPython(_ context.Context, req *runtime.RunPythonRequest) (*runtime.JobResult, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	r := s.result
	if r == nil {
		r = &runtime.JobResult{
			RequestID: req.RequestID,
			JobID:     "stub-job-001",
			Status:    runtime.JobSuccess,
			ExitCode:  0,
			Stdout:    "hello from stub",
		}
	}
	return r, nil
}

func (s *stubRunner) RunShell(_ context.Context, req *runtime.RunShellRequest) (*runtime.JobResult, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	r := s.result
	if r == nil {
		r = &runtime.JobResult{
			RequestID: req.RequestID,
			JobID:     "stub-job-002",
			Status:    runtime.JobSuccess,
			ExitCode:  0,
			Stdout:    "stub output",
		}
	}
	return r, nil
}

// ─── Test helpers ─────────────────────────────────────────────────────────────

func newGate(stub *stubRunner, logger audit.AuditEventLogger) *gate.GatedRunner {
	return gate.NewGatedRunner(gate.Config{
		Checker:  policies.DefaultChecker,
		Detector: safety.DefaultScanner,
		Logger:   logger,
		Runner:   stub,
	})
}

func pythonReq(id, code string) *runtime.RunPythonRequest {
	return &runtime.RunPythonRequest{
		RequestID:    id,
		SessionID:    "sess_test",
		UserID:       "user_test",
		WorkspaceDir: "/tmp/vclaw-test-ws",
		Code:         code,
	}
}

func shellReq(id, cmd string) *runtime.RunShellRequest {
	return &runtime.RunShellRequest{
		RequestID:    id,
		SessionID:    "sess_test",
		UserID:       "user_test",
		WorkspaceDir: "/tmp/vclaw-test-ws",
		Command:      cmd,
	}
}

// ─── RunPython: requires approval ─────────────────────────────────────────────

func TestGate_RunPython_NeedsApproval_SafeCode(t *testing.T) {
	stub := &stubRunner{}
	logger := audit.NewMemoryLogger()
	g := newGate(stub, logger)

	_, err := g.RunPython(context.Background(), pythonReq("req_py_01",
		"import pandas as pd\ndf = pd.read_csv('/workspace/data.csv')\nprint(df.shape)"))

	if !gate.IsNeedsApproval(err) {
		t.Fatalf("safe code execution must need approval, got: %v", err)
	}
	if stub.calls != 0 {
		t.Errorf("runner must not be called before approval, got %d", stub.calls)
	}

	if logger.Count() < 3 {
		t.Errorf("expected at least 3 audit events, got %d", logger.Count())
	}

	results, _ := logger.Query(audit.Filter{EventType: audit.EventExecutionResult})
	if len(results) != 0 {
		t.Errorf("must not log execution_result before approval, got %d", len(results))
	}
}

// ─── RunPython: block ─────────────────────────────────────────────────────────

func TestGate_RunPython_Block_CredentialAccess(t *testing.T) {
	stub := &stubRunner{}
	logger := audit.NewMemoryLogger()
	g := newGate(stub, logger)

	_, err := g.RunPython(context.Background(), pythonReq("req_py_02",
		"with open('/workspace/.env', 'r') as f:\n    print(f.read())"))

	if err == nil {
		t.Fatal("expected ErrBlocked, got nil")
	}
	if !gate.IsBlocked(err) {
		t.Errorf("expected IsBlocked(err)=true, got false. err=%v", err)
	}
	var blocked *gate.ErrBlocked
	if !errors.As(err, &blocked) {
		t.Fatalf("cannot unwrap ErrBlocked from: %v", err)
	}
	if blocked.RequestID != "req_py_02" {
		t.Errorf("RequestID mismatch: %s", blocked.RequestID)
	}
	if blocked.PolicyResult.Decision != policies.DecisionBlock {
		t.Errorf("decision should be block, got %s", blocked.PolicyResult.Decision)
	}
	if stub.calls != 0 {
		t.Errorf("runner must NOT be called when blocked, got %d calls", stub.calls)
	}

	// Audit: should have a blocked event
	events, _ := logger.Query(audit.Filter{EventType: audit.EventBlocked})
	if len(events) != 1 {
		t.Errorf("expected 1 blocked audit event, got %d", len(events))
	}
	if events[0].Status != audit.StatusBlocked {
		t.Errorf("expected StatusBlocked, got %s", events[0].Status)
	}
}

func TestGate_RunPython_Block_SystemShutdown(t *testing.T) {
	stub := &stubRunner{}
	logger := audit.NewMemoryLogger()
	g := newGate(stub, logger)

	_, err := g.RunPython(context.Background(), pythonReq("req_py_03",
		"import os\nos.system('shutdown -h now')"))

	if !gate.IsBlocked(err) {
		t.Errorf("system shutdown code should be blocked, got err=%v", err)
	}
	if stub.calls != 0 {
		t.Error("runner must NOT be called for blocked request")
	}
}

// ─── RunPython: requires_approval ─────────────────────────────────────────────

func TestGate_RunPython_NeedsApproval_DeleteFile(t *testing.T) {
	stub := &stubRunner{}
	logger := audit.NewMemoryLogger()
	g := newGate(stub, logger)

	_, err := g.RunPython(context.Background(), pythonReq("req_py_04",
		"import os\nos.remove('/workspace/output/old.csv')"))

	if err == nil {
		t.Fatal("expected ErrNeedsApproval, got nil")
	}
	if gate.IsBlocked(err) {
		t.Error("os.remove should need approval, not be blocked")
	}
	if !gate.IsNeedsApproval(err) {
		t.Errorf("expected IsNeedsApproval=true, got false. err=%v", err)
	}

	var na *gate.ErrNeedsApproval
	if !errors.As(err, &na) {
		t.Fatalf("cannot unwrap ErrNeedsApproval from: %v", err)
	}
	if na.RequestID != "req_py_04" {
		t.Errorf("RequestID mismatch: %s", na.RequestID)
	}
	if na.PolicyResult.Decision != policies.DecisionRequiresApproval {
		t.Errorf("decision should be requires_approval, got %s", na.PolicyResult.Decision)
	}
	if stub.calls != 0 {
		t.Error("runner must NOT be called when requires_approval")
	}

	// Audit: should have hitl_proposal
	events, _ := logger.Query(audit.Filter{EventType: audit.EventHITLProposal})
	if len(events) != 1 {
		t.Errorf("expected 1 hitl_proposal audit event, got %d", len(events))
	}
}

// ─── RunShell: requires approval ──────────────────────────────────────────────

func TestGate_RunShell_NeedsApproval_ListFiles(t *testing.T) {
	stub := &stubRunner{}
	logger := audit.NewMemoryLogger()
	g := newGate(stub, logger)

	_, err := g.RunShell(context.Background(), shellReq("req_sh_01", "ls -la /workspace"))

	if !gate.IsNeedsApproval(err) {
		t.Fatalf("shell code execution must need approval, got: %v", err)
	}
	if stub.calls != 0 {
		t.Errorf("runner must not be called before approval, got %d", stub.calls)
	}
}

func TestGate_RunShell_NeedsApproval_HeadFile(t *testing.T) {
	stub := &stubRunner{}
	g := newGate(stub, audit.NewMemoryLogger())

	_, err := g.RunShell(context.Background(), shellReq("req_sh_02", "head -20 /workspace/report.csv"))
	if !gate.IsNeedsApproval(err) {
		t.Fatalf("head command should need approval, got: %v", err)
	}
	if stub.calls != 0 {
		t.Errorf("runner must not be called before approval, got %d", stub.calls)
	}
}

// ─── RunShell: block ──────────────────────────────────────────────────────────

func TestGate_RunShell_Block_Shutdown(t *testing.T) {
	stub := &stubRunner{}
	logger := audit.NewMemoryLogger()
	g := newGate(stub, logger)

	_, err := g.RunShell(context.Background(), shellReq("req_sh_03", "shutdown -h now"))

	if !gate.IsBlocked(err) {
		t.Errorf("shutdown should be blocked, got err=%v", err)
	}
	if stub.calls != 0 {
		t.Error("runner must NOT be called when blocked")
	}

	events, _ := logger.Query(audit.Filter{EventType: audit.EventBlocked})
	if len(events) != 1 {
		t.Errorf("expected 1 blocked event, got %d", len(events))
	}
}

func TestGate_RunShell_Block_CredentialRead(t *testing.T) {
	stub := &stubRunner{}
	g := newGate(stub, audit.NewMemoryLogger())

	_, err := g.RunShell(context.Background(), shellReq("req_sh_04", "cat /root/.ssh/id_rsa"))
	if !gate.IsBlocked(err) {
		t.Errorf("reading private key should be blocked, got: %v", err)
	}
}

func TestGate_RunShell_Block_Sudo(t *testing.T) {
	stub := &stubRunner{}
	g := newGate(stub, audit.NewMemoryLogger())

	_, err := g.RunShell(context.Background(), shellReq("req_sh_05", "sudo rm -rf /"))
	if !gate.IsBlocked(err) {
		t.Errorf("sudo rm should be blocked, got: %v", err)
	}
}

// ─── RunShell: requires_approval ──────────────────────────────────────────────

func TestGate_RunShell_NeedsApproval_RmRf(t *testing.T) {
	stub := &stubRunner{}
	logger := audit.NewMemoryLogger()
	g := newGate(stub, logger)

	_, err := g.RunShell(context.Background(), shellReq("req_sh_06", "rm -rf /workspace/temp"))

	if !gate.IsNeedsApproval(err) {
		t.Errorf("rm -rf should need approval, got err=%v", err)
	}
	if stub.calls != 0 {
		t.Error("runner must not be called when requires_approval")
	}

	hitl, _ := logger.Query(audit.Filter{EventType: audit.EventHITLProposal})
	if len(hitl) != 1 {
		t.Errorf("expected 1 hitl_proposal event, got %d", len(hitl))
	}
	if hitl[0].HITLApprovalID == "" {
		t.Error("hitl_approval_id must be set on HITL proposal event")
	}
}

func TestGate_RunShell_NeedsApproval_Curl(t *testing.T) {
	stub := &stubRunner{}
	g := newGate(stub, audit.NewMemoryLogger())

	_, err := g.RunShell(context.Background(), shellReq("req_sh_07", "curl https://api.example.com/data"))
	if !gate.IsNeedsApproval(err) {
		t.Errorf("curl should need approval, got: %v", err)
	}
}

// ─── Audit log completeness ───────────────────────────────────────────────────

func TestGate_Audit_CodeExecutionRequest_HasHITLProposal(t *testing.T) {
	stub := &stubRunner{}
	logger := audit.NewMemoryLogger()
	g := newGate(stub, logger)

	_, _ = g.RunShell(context.Background(), shellReq("req_audit_01", "ls /workspace"))

	events, _ := logger.Query(audit.Filter{RequestID: "req_audit_01"})
	if len(events) < 3 {
		t.Errorf("held code execution request should produce at least 3 audit events, got %d", len(events))
	}

	hasType := func(et audit.EventType) bool {
		for _, ev := range events {
			if ev.EventType == et {
				return true
			}
		}
		return false
	}
	for _, et := range []audit.EventType{
		audit.EventToolRequest,
		audit.EventPolicyDecision,
		audit.EventHITLProposal,
	} {
		if !hasType(et) {
			t.Errorf("missing audit event type %q", et)
		}
	}
	for _, et := range []audit.EventType{audit.EventExecutionStart, audit.EventExecutionResult} {
		if hasType(et) {
			t.Errorf("must not log audit event type %q before approval", et)
		}
	}
}

func TestGate_Audit_BlockedRequest_HasThreeEvents(t *testing.T) {
	stub := &stubRunner{}
	logger := audit.NewMemoryLogger()
	g := newGate(stub, logger)

	_, _ = g.RunShell(context.Background(), shellReq("req_audit_02", "shutdown -h now"))

	events, _ := logger.Query(audit.Filter{RequestID: "req_audit_02"})
	// Expected: tool_request, policy_decision, blocked
	if len(events) < 3 {
		t.Errorf("blocked request should produce ≥3 audit events, got %d", len(events))
	}

	// execution_result should NOT exist for blocked requests
	for _, ev := range events {
		if ev.EventType == audit.EventExecutionResult {
			t.Error("blocked request must not have an execution_result event")
		}
	}
}

func TestGate_Audit_NeedsApprovalRequest_HasHITLProposal(t *testing.T) {
	stub := &stubRunner{}
	logger := audit.NewMemoryLogger()
	g := newGate(stub, logger)

	_, _ = g.RunShell(context.Background(), shellReq("req_audit_03", "rm /workspace/old.csv"))

	events, _ := logger.Query(audit.Filter{RequestID: "req_audit_03"})

	hasHITL := false
	for _, ev := range events {
		if ev.EventType == audit.EventHITLProposal {
			hasHITL = true
			if ev.HITLSummaryVI == "" {
				t.Error("hitl_summary_vi must not be empty")
			}
			if ev.HITLApprovalID == "" {
				t.Error("hitl_approval_id must not be empty")
			}
		}
		if ev.EventType == audit.EventExecutionResult {
			t.Error("requires_approval request must not have execution_result event")
		}
	}
	if !hasHITL {
		t.Error("requires_approval request must have a hitl_proposal event")
	}
}

// ─── Runner error passthrough ─────────────────────────────────────────────────

func TestGate_RunShell_DefaultPolicyHoldsBeforeRunnerError(t *testing.T) {
	runnerErr := errors.New("docker daemon is not running")
	stub := &stubRunner{err: runnerErr}
	logger := audit.NewMemoryLogger()
	g := newGate(stub, logger)

	_, err := g.RunShell(context.Background(), shellReq("req_re_01", "ls /workspace"))
	if err == nil {
		t.Fatal("expected approval error, got nil")
	}
	if errors.Is(err, runnerErr) {
		t.Errorf("runner error must not surface before approval, got: %v", err)
	}
	if !gate.IsNeedsApproval(err) {
		t.Errorf("expected ErrNeedsApproval before runner dispatch, got: %v", err)
	}
	if stub.calls != 0 {
		t.Errorf("runner must not be called before approval, got %d", stub.calls)
	}
}

// ─── Error message content ────────────────────────────────────────────────────

func TestGate_ErrBlocked_ErrorMessage(t *testing.T) {
	stub := &stubRunner{}
	g := newGate(stub, audit.NewMemoryLogger())

	_, err := g.RunShell(context.Background(), shellReq("req_em_01", "shutdown -h now"))
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !containsAny(msg, "block", "req_em_01") {
		t.Errorf("ErrBlocked message should contain 'block' and request ID, got: %s", msg)
	}
}

func TestGate_ErrNeedsApproval_ErrorMessage(t *testing.T) {
	stub := &stubRunner{}
	g := newGate(stub, audit.NewMemoryLogger())

	_, err := g.RunShell(context.Background(), shellReq("req_em_02", "rm /workspace/old.csv"))
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !containsAny(msg, "approval", "req_em_02") {
		t.Errorf("ErrNeedsApproval message should contain 'approval' and request ID, got: %s", msg)
	}
}

// ─── NeedsApproval carries threats ───────────────────────────────────────────

func TestGate_NeedsApproval_CarriesThreats(t *testing.T) {
	stub := &stubRunner{}
	g := newGate(stub, audit.NewMemoryLogger())

	_, err := g.RunShell(context.Background(), shellReq("req_thr_01", "rm -rf /workspace/output"))
	var na *gate.ErrNeedsApproval
	if !errors.As(err, &na) {
		t.Fatalf("expected ErrNeedsApproval, got: %v", err)
	}
	if len(na.Threats) == 0 {
		t.Error("ErrNeedsApproval should carry at least one DangerReport for rm -rf")
	}
	if !safety.HasCategory(na.Threats, safety.ThreatFileDeletion) {
		t.Errorf("expected file_deletion threat in rm -rf reports, got: %v",
			safety.Categories(na.Threats))
	}
}

// ─── Nil logger defaults to NopLogger ────────────────────────────────────────

func TestGate_NilLogger_DoesNotPanic(t *testing.T) {
	stub := &stubRunner{}
	g := gate.NewGatedRunner(gate.Config{
		Checker:  policies.DefaultChecker,
		Detector: safety.DefaultScanner,
		Logger:   nil, // should silently use NopLogger
		Runner:   stub,
	})

	_, err := g.RunShell(context.Background(), shellReq("req_nop_01", "ls /workspace"))
	if !gate.IsNeedsApproval(err) {
		t.Fatalf("nil logger should not cause panic and shell execution should need approval, got: %v", err)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
