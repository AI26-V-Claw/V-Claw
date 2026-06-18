package agent

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"vclaw/internal/orchestration"
)

// TestRunState_HasFailureReasonField verifies FailureReason field exists on RunState.
func TestRunState_HasFailureReasonField(t *testing.T) {
	state := RunState{}
	state.FailureReason = string(orchestration.FailureReasonTimeout)
	if state.FailureReason != "timeout" {
		t.Errorf("expected timeout, got %s", state.FailureReason)
	}
}

// TestOrchestration_StatusAlignment verifies string values are aligned between packages.
func TestOrchestration_StatusAlignment(t *testing.T) {
	if string(orchestration.RunStatusCanceled) != string(RuntimeRunStatusCancelled) {
		t.Errorf("canceled mismatch: orchestration=%q runtime=%q",
			orchestration.RunStatusCanceled, RuntimeRunStatusCancelled)
	}
	if string(orchestration.RunStatusMaxIteration) != string(RuntimeRunStatusMaxIterations) {
		t.Errorf("max_iteration mismatch: orchestration=%q runtime=%q",
			orchestration.RunStatusMaxIteration, RuntimeRunStatusMaxIterations)
	}
}

// TestFailureReason_ToolError verifies FailureReason is set when tool execution fails.
func TestFailureReason_ToolError(t *testing.T) {
	store := NewInMemoryRuntimeStateStore()
	r := &Runtime{
		stateStore: store,
		now:        func() time.Time { return time.Now() },
		logger:     slog.Default(),
	}

	runState := RunState{
		RunID:     "run_tool_error",
		SessionID: "sess_001",
		RequestID: "req_001",
		Status:    RuntimeRunStatusRunning,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.CreateRun(context.Background(), runState); err != nil {
		t.Fatalf("create run: %v", err)
	}

	_, _ = r.finishRunState(context.Background(), runState, RuntimeRunStatusFailed, "tool_error")

	updated, err := store.GetRun(context.Background(), "run_tool_error")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if updated.Status != RuntimeRunStatusFailed {
		t.Errorf("expected status failed, got %s", updated.Status)
	}
	if updated.FailureReason != "tool_error" {
		t.Errorf("expected FailureReason=tool_error, got %q", updated.FailureReason)
	}
}

// TestFailureReason_ApprovalExpired verifies FailureReason is set correctly for approval expiry.
func TestFailureReason_ApprovalExpired(t *testing.T) {
	store := NewInMemoryRuntimeStateStore()
	r := &Runtime{
		stateStore: store,
		now:        func() time.Time { return time.Now() },
		logger:     slog.Default(),
	}

	runState := RunState{
		RunID:     "run_approval_expired",
		SessionID: "sess_001",
		RequestID: "req_001",
		Status:    RuntimeRunStatusRunning,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.CreateRun(context.Background(), runState); err != nil {
		t.Fatalf("create run: %v", err)
	}

	_, _ = r.finishRunState(context.Background(), runState, RuntimeRunStatusFailed, "approval_expired")

	updated, err := store.GetRun(context.Background(), "run_approval_expired")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if updated.FailureReason != "approval_expired" {
		t.Errorf("expected FailureReason=approval_expired, got %q", updated.FailureReason)
	}
}

// TestFailureReason_HappyPath verifies FailureReason is empty on completed runs.
func TestFailureReason_HappyPath(t *testing.T) {
	store := NewInMemoryRuntimeStateStore()
	r := &Runtime{
		stateStore: store,
		now:        func() time.Time { return time.Now() },
		logger:     slog.Default(),
	}

	runState := RunState{
		RunID:     "run_happy",
		SessionID: "sess_001",
		RequestID: "req_001",
		Status:    RuntimeRunStatusRunning,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.CreateRun(context.Background(), runState); err != nil {
		t.Fatalf("create run: %v", err)
	}

	_, _ = r.finishRunState(context.Background(), runState, RuntimeRunStatusCompleted, "")

	updated, err := store.GetRun(context.Background(), "run_happy")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if updated.Status != RuntimeRunStatusCompleted {
		t.Errorf("expected status completed, got %s", updated.Status)
	}
	if updated.FailureReason != "" {
		t.Errorf("expected empty FailureReason on happy path, got %q", updated.FailureReason)
	}
}
