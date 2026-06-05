package audit_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vclaw/internal/audit"
)

// ─── HashCommand ──────────────────────────────────────────────────────────────

func TestHashCommand(t *testing.T) {
	h1 := audit.HashCommand("rm -rf /")
	h2 := audit.HashCommand("rm -rf /")
	h3 := audit.HashCommand("ls -la")

	if !strings.HasPrefix(h1, "sha256:") {
		t.Errorf("hash should start with 'sha256:': %s", h1)
	}
	if h1 != h2 {
		t.Errorf("same command must produce same hash: %s vs %s", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("different commands must produce different hashes")
	}
}

func TestHashCommandEmpty(t *testing.T) {
	h := audit.HashCommand("")
	if !strings.HasPrefix(h, "sha256:") {
		t.Errorf("empty command hash should still have prefix: %s", h)
	}
}

// ─── PreviewCommand ───────────────────────────────────────────────────────────

func TestPreviewCommand_Short(t *testing.T) {
	p := audit.PreviewCommand("ls -la", 120)
	if p != "ls -la" {
		t.Errorf("short command should be unchanged, got %q", p)
	}
}

func TestPreviewCommand_Truncation(t *testing.T) {
	long := strings.Repeat("x", 200)
	p := audit.PreviewCommand(long, 120)
	if len([]rune(p)) > 125 {
		t.Errorf("preview should be truncated near maxLen, got len=%d", len(p))
	}
	if !strings.HasSuffix(p, "…") {
		t.Error("truncated preview should end with ellipsis")
	}
}

func TestPreviewCommand_NewlinesReplaced(t *testing.T) {
	code := "import os\nos.remove('/etc/passwd')"
	p := audit.PreviewCommand(code, 200)
	if strings.Contains(p, "\n") {
		t.Error("preview should not contain newlines")
	}
}

// ─── SummariseOutput ──────────────────────────────────────────────────────────

func TestSummariseOutput_BothEmpty(t *testing.T) {
	s := audit.SummariseOutput("", "", 100)
	if s != "(no output)" {
		t.Errorf("expected '(no output)', got %q", s)
	}
}

func TestSummariseOutput_StdoutOnly(t *testing.T) {
	s := audit.SummariseOutput("hello", "", 100)
	if !strings.Contains(s, "stdout: hello") {
		t.Errorf("unexpected output summary: %q", s)
	}
}

func TestSummariseOutput_StderrOnly(t *testing.T) {
	s := audit.SummariseOutput("", "error: boom", 100)
	if !strings.Contains(s, "stderr: error: boom") {
		t.Errorf("unexpected output summary: %q", s)
	}
}

func TestSummariseOutput_BothStreams(t *testing.T) {
	s := audit.SummariseOutput("done", "warn", 100)
	if !strings.Contains(s, "stdout: done") || !strings.Contains(s, "stderr: warn") {
		t.Errorf("unexpected output summary: %q", s)
	}
}

func TestSummariseOutput_Truncation(t *testing.T) {
	long := strings.Repeat("x", 500)
	s := audit.SummariseOutput(long, "", 50)
	if !strings.Contains(s, "…") {
		t.Error("long output should be truncated with ellipsis")
	}
}

// ─── Constructor helpers ──────────────────────────────────────────────────────

func TestNewToolRequestEvent(t *testing.T) {
	ev := audit.NewToolRequestEvent("req_1", "sess_1", "user_1", "sandbox.runShell", audit.ActionRunShell, "ls -la")
	if ev.EventType != audit.EventToolRequest {
		t.Errorf("expected EventToolRequest, got %s", ev.EventType)
	}
	if ev.RequestID != "req_1" {
		t.Errorf("RequestID not set: %s", ev.RequestID)
	}
	if ev.SessionID != "sess_1" {
		t.Errorf("SessionID not set: %s", ev.SessionID)
	}
	if ev.Status != audit.StatusProposed {
		t.Errorf("initial status should be proposed, got %s", ev.Status)
	}
	if !strings.HasPrefix(ev.CommandHash, "sha256:") {
		t.Errorf("CommandHash should start with sha256:, got %s", ev.CommandHash)
	}
	if ev.CommandPreview == "" {
		t.Error("CommandPreview must not be empty")
	}
	if ev.EventID == "" {
		t.Error("EventID must be generated")
	}
	if ev.Timestamp.IsZero() {
		t.Error("Timestamp must be set")
	}
}

func TestNewPolicyEvent_Allow(t *testing.T) {
	base := audit.NewToolRequestEvent("req_2", "sess_1", "user_1", "sandbox.runShell", audit.ActionRunShell, "cat file.txt")
	ev := audit.NewPolicyEvent(base, "safe_read", "allow", []string{"Đọc file an toàn"})

	if ev.EventType != audit.EventPolicyDecision {
		t.Errorf("expected EventPolicyDecision, got %s", ev.EventType)
	}
	if ev.Status != audit.StatusApproved {
		t.Errorf("allow should set status to approved, got %s", ev.Status)
	}
	if ev.RiskLevel != "safe_read" {
		t.Errorf("RiskLevel not set: %s", ev.RiskLevel)
	}
	if len(ev.PolicyReasons) != 1 {
		t.Errorf("expected 1 policy reason, got %d", len(ev.PolicyReasons))
	}
}

func TestNewPolicyEvent_Block(t *testing.T) {
	base := audit.NewToolRequestEvent("req_3", "sess_1", "user_1", "sandbox.runShell", audit.ActionRunShell, "rm -rf /")
	ev := audit.NewPolicyEvent(base, "high_risk", "block", []string{"Xóa thư mục gốc"})

	if ev.Status != audit.StatusBlocked {
		t.Errorf("block should set status to blocked, got %s", ev.Status)
	}
}

func TestNewPolicyEvent_RequiresApproval(t *testing.T) {
	base := audit.NewToolRequestEvent("req_4", "sess_1", "user_1", "sandbox.runShell", audit.ActionFileDelete, "rm output/*.tmp")
	ev := audit.NewPolicyEvent(base, "destructive", "requires_approval", []string{"Xóa file"})

	if ev.Status != audit.StatusProposed {
		t.Errorf("requires_approval should set status to proposed, got %s", ev.Status)
	}
}

func TestNewHITLEvents(t *testing.T) {
	base := audit.NewToolRequestEvent("req_5", "sess_1", "user_1", "sandbox.runShell", audit.ActionFileDelete, "rm *.csv")

	proposal := audit.NewHITLProposalEvent(base, "hitl_001", "Xóa file CSV", "Lệnh này sẽ xóa file", []string{"workspace/a.csv"})
	if proposal.EventType != audit.EventHITLProposal {
		t.Errorf("expected EventHITLProposal, got %s", proposal.EventType)
	}
	if proposal.HITLApprovalID != "hitl_001" {
		t.Errorf("HITLApprovalID not set")
	}
	if len(proposal.AffectedPaths) != 1 {
		t.Errorf("expected 1 affected path, got %d", len(proposal.AffectedPaths))
	}

	approved := audit.NewHITLApprovedEvent(base, "hitl_001")
	if approved.EventType != audit.EventHITLApproved || approved.Status != audit.StatusApproved {
		t.Errorf("unexpected approved event: type=%s status=%s", approved.EventType, approved.Status)
	}

	rejected := audit.NewHITLRejectedEvent(base, "hitl_001")
	if rejected.EventType != audit.EventHITLRejected || rejected.Status != audit.StatusRejected {
		t.Errorf("unexpected rejected event: type=%s status=%s", rejected.EventType, rejected.Status)
	}
}

func TestNewBlockedEvent(t *testing.T) {
	base := audit.NewToolRequestEvent("req_6", "sess_1", "user_1", "sandbox.runShell", audit.ActionSystemShutdown, "shutdown -h now")
	ev := audit.NewBlockedEvent(base, "high_risk", []string{"Tắt máy"})

	if ev.EventType != audit.EventBlocked {
		t.Errorf("expected EventBlocked, got %s", ev.EventType)
	}
	if ev.Status != audit.StatusBlocked {
		t.Errorf("expected StatusBlocked, got %s", ev.Status)
	}
}

func TestNewExecutionEvents(t *testing.T) {
	base := audit.NewToolRequestEvent("req_7", "sess_1", "user_1", "sandbox.runPython", audit.ActionRunPython, "print('hello')")

	start := audit.NewExecutionStartEvent(base, "job_001")
	if start.EventType != audit.EventExecutionStart {
		t.Errorf("expected EventExecutionStart, got %s", start.EventType)
	}
	if start.Status != audit.StatusExecuting {
		t.Errorf("expected StatusExecuting, got %s", start.Status)
	}
	if start.JobID != "job_001" {
		t.Errorf("JobID not set: %s", start.JobID)
	}

	result := audit.NewExecutionResultEvent(base, "job_001", "success", 0, 420, "hello", false)
	if result.EventType != audit.EventExecutionResult {
		t.Errorf("expected EventExecutionResult, got %s", result.EventType)
	}
	if result.Status != audit.StatusExecuted {
		t.Errorf("success should map to StatusExecuted, got %s", result.Status)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code should be 0, got %d", result.ExitCode)
	}
	if result.DurationMs != 420 {
		t.Errorf("DurationMs should be 420, got %d", result.DurationMs)
	}
}

func TestExecutionResult_Timeout(t *testing.T) {
	base := audit.NewToolRequestEvent("req_8", "sess_1", "user_1", "sandbox.runPython", audit.ActionRunPython, "import time; time.sleep(9999)")
	ev := audit.NewExecutionResultEvent(base, "job_002", "timeout", -1, 30000, "", false)
	if ev.Status != audit.StatusTimeout {
		t.Errorf("timeout result should map to StatusTimeout, got %s", ev.Status)
	}
}

func TestExecutionResult_Failed(t *testing.T) {
	base := audit.NewToolRequestEvent("req_9", "sess_1", "user_1", "sandbox.runPython", audit.ActionRunPython, "1/0")
	ev := audit.NewExecutionResultEvent(base, "job_003", "failed", 1, 50, "ZeroDivisionError", false)
	if ev.Status != audit.StatusFailed {
		t.Errorf("failed result should map to StatusFailed, got %s", ev.Status)
	}
}

// ─── Event ID uniqueness ──────────────────────────────────────────────────────

func TestEventIDUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		ev := audit.NewToolRequestEvent(
			fmt.Sprintf("req_%d", i), "sess", "user", "sandbox.runShell", audit.ActionRunShell, "ls",
		)
		if seen[ev.EventID] {
			t.Fatalf("duplicate EventID: %s (i=%d)", ev.EventID, i)
		}
		seen[ev.EventID] = true
	}
}

// ─── JSON serialisation ───────────────────────────────────────────────────────

func TestAuditEventJSON_RoundTrip(t *testing.T) {
	original := audit.NewToolRequestEvent("req_j", "sess_j", "user_j", "sandbox.runShell", audit.ActionRunShell, "echo hello")
	original.AffectedPaths = []string{"workspace/foo.txt"}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded audit.AuditEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.EventID != original.EventID {
		t.Errorf("EventID mismatch: %s vs %s", decoded.EventID, original.EventID)
	}
	if decoded.RequestID != original.RequestID {
		t.Errorf("RequestID mismatch")
	}
	if decoded.CommandHash != original.CommandHash {
		t.Errorf("CommandHash mismatch")
	}
	if len(decoded.AffectedPaths) != 1 || decoded.AffectedPaths[0] != "workspace/foo.txt" {
		t.Errorf("AffectedPaths mismatch: %v", decoded.AffectedPaths)
	}
}

func TestAuditEventJSON_FieldNames(t *testing.T) {
	ev := audit.NewToolRequestEvent("req_fn", "s", "u", "sandbox.runShell", audit.ActionRunShell, "ls")
	data, _ := json.Marshal(ev)
	s := string(data)
	for _, key := range []string{
		"event_id", "event_type", "timestamp", "session_id", "user_id",
		"request_id", "tool", "action_type", "status",
		"command_hash", "command_preview",
	} {
		if !strings.Contains(s, fmt.Sprintf("%q:", key)) {
			t.Errorf("JSON missing field %q", key)
		}
	}
}

// ─── MemoryLogger ─────────────────────────────────────────────────────────────

func TestMemoryLogger_LogAndQuery(t *testing.T) {
	logger := audit.NewMemoryLogger()

	for i := 0; i < 5; i++ {
		ev := audit.NewToolRequestEvent(fmt.Sprintf("req_%d", i), "sess_A", "user_1", "sandbox.runShell", audit.ActionRunShell, "ls")
		_ = logger.Log(ev)
	}
	ev6 := audit.NewToolRequestEvent("req_6", "sess_B", "user_2", "sandbox.runPython", audit.ActionRunPython, "print(1)")
	_ = logger.Log(ev6)

	if logger.Count() != 6 {
		t.Errorf("expected 6 events, got %d", logger.Count())
	}

	// Filter by session
	results, _ := logger.Query(audit.Filter{SessionID: "sess_A"})
	if len(results) != 5 {
		t.Errorf("expected 5 events for sess_A, got %d", len(results))
	}

	// Filter by user
	results, _ = logger.Query(audit.Filter{UserID: "user_2"})
	if len(results) != 1 {
		t.Errorf("expected 1 event for user_2, got %d", len(results))
	}

	// Filter by request ID
	results, _ = logger.Query(audit.Filter{RequestID: "req_3"})
	if len(results) != 1 || results[0].RequestID != "req_3" {
		t.Errorf("expected 1 event for req_3, got %d", len(results))
	}

	// Limit
	results, _ = logger.Query(audit.Filter{SessionID: "sess_A", Limit: 3})
	if len(results) != 3 {
		t.Errorf("expected 3 events with limit, got %d", len(results))
	}
}

func TestMemoryLogger_FilterByStatus(t *testing.T) {
	logger := audit.NewMemoryLogger()
	base := audit.NewToolRequestEvent("req_s", "sess", "user", "sandbox.runShell", audit.ActionRunShell, "cmd")

	_ = logger.Log(audit.NewBlockedEvent(base, "high_risk", []string{"Blocked"}))
	_ = logger.Log(audit.NewHITLApprovedEvent(base, "hitl_x"))

	blocked, _ := logger.Query(audit.Filter{Status: audit.StatusBlocked})
	if len(blocked) != 1 {
		t.Errorf("expected 1 blocked event, got %d", len(blocked))
	}

	approved, _ := logger.Query(audit.Filter{Status: audit.StatusApproved})
	if len(approved) != 1 {
		t.Errorf("expected 1 approved event, got %d", len(approved))
	}
}

func TestMemoryLogger_FilterByTimeRange(t *testing.T) {
	logger := audit.NewMemoryLogger()
	before := time.Now().UTC()

	ev := audit.NewToolRequestEvent("req_t", "sess", "user", "sandbox.runShell", audit.ActionRunShell, "ls")
	_ = logger.Log(ev)

	after := time.Now().UTC()

	results, _ := logger.Query(audit.Filter{Since: before, Until: after})
	if len(results) != 1 {
		t.Errorf("expected 1 event in time range, got %d", len(results))
	}

	results, _ = logger.Query(audit.Filter{Since: after.Add(time.Second)})
	if len(results) != 0 {
		t.Errorf("expected 0 events after future timestamp, got %d", len(results))
	}
}

func TestMemoryLogger_Clear(t *testing.T) {
	logger := audit.NewMemoryLogger()
	ev := audit.NewToolRequestEvent("req_c", "sess", "user", "sandbox.runShell", audit.ActionRunShell, "ls")
	_ = logger.Log(ev)

	logger.Clear()
	if logger.Count() != 0 {
		t.Errorf("expected 0 after Clear, got %d", logger.Count())
	}
}

// ─── FileLogger ───────────────────────────────────────────────────────────────

func TestFileLogger_WriteAndRead(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "audit_test.jsonl")

	logger, err := audit.NewFileLogger(path)
	if err != nil {
		t.Fatalf("NewFileLogger failed: %v", err)
	}

	ev1 := audit.NewToolRequestEvent("req_f1", "sess_f", "user_f", "sandbox.runShell", audit.ActionRunShell, "ls")
	ev2 := audit.NewToolRequestEvent("req_f2", "sess_f", "user_f", "sandbox.runPython", audit.ActionRunPython, "print(1)")
	ev3 := audit.NewBlockedEvent(ev1, "high_risk", []string{"test"})

	_ = logger.Log(ev1)
	_ = logger.Log(ev2)
	_ = logger.Log(ev3)
	_ = logger.Close()

	// Reopen and query
	logger2, err := audit.NewFileLogger(path)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer logger2.Close()

	// Check file has 3 lines
	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 JSONL lines, got %d", len(lines))
	}

	// Each line should parse as valid JSON
	for i, line := range lines {
		var ev audit.AuditEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}

	results, err := logger2.Query(audit.Filter{SessionID: "sess_f"})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results for sess_f, got %d", len(results))
	}

	blocked, _ := logger2.Query(audit.Filter{Status: audit.StatusBlocked})
	if len(blocked) != 1 {
		t.Errorf("expected 1 blocked event, got %d", len(blocked))
	}
}

func TestFileLogger_Append(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "audit_append.jsonl")

	// Write 2 events, close.
	l1, _ := audit.NewFileLogger(path)
	_ = l1.Log(audit.NewToolRequestEvent("r1", "s", "u", "sandbox.runShell", audit.ActionRunShell, "ls"))
	_ = l1.Log(audit.NewToolRequestEvent("r2", "s", "u", "sandbox.runShell", audit.ActionRunShell, "pwd"))
	_ = l1.Close()

	// Open again and write 1 more.
	l2, _ := audit.NewFileLogger(path)
	_ = l2.Log(audit.NewToolRequestEvent("r3", "s", "u", "sandbox.runShell", audit.ActionRunShell, "date"))
	_ = l2.Close()

	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines after append, got %d", len(lines))
	}
}

// ─── MultiLogger ─────────────────────────────────────────────────────────────

func TestMultiLogger(t *testing.T) {
	mem1 := audit.NewMemoryLogger()
	mem2 := audit.NewMemoryLogger()
	multi := audit.NewMultiLogger(mem1, mem2)

	ev := audit.NewToolRequestEvent("req_m", "sess_m", "user_m", "sandbox.runShell", audit.ActionRunShell, "ls")
	if err := multi.Log(ev); err != nil {
		t.Fatalf("MultiLogger.Log failed: %v", err)
	}

	if mem1.Count() != 1 {
		t.Errorf("mem1 should have 1 event, got %d", mem1.Count())
	}
	if mem2.Count() != 1 {
		t.Errorf("mem2 should have 1 event, got %d", mem2.Count())
	}

	results, _ := multi.Query(audit.Filter{RequestID: "req_m"})
	if len(results) != 1 {
		t.Errorf("MultiLogger.Query should delegate to first logger, got %d", len(results))
	}
}

// ─── NopLogger ────────────────────────────────────────────────────────────────

func TestNopLogger(t *testing.T) {
	nop := &audit.NopLogger{}
	ev := audit.NewToolRequestEvent("req_n", "s", "u", "sandbox.runShell", audit.ActionRunShell, "ls")
	if err := nop.Log(ev); err != nil {
		t.Errorf("NopLogger.Log should not error: %v", err)
	}
	results, err := nop.Query(audit.Filter{})
	if err != nil || len(results) != 0 {
		t.Errorf("NopLogger.Query should return empty, got err=%v len=%d", err, len(results))
	}
}
