package agent

import (
	"context"
	"testing"
	"time"
)

func TestFileRuntimeStateStorePersistsPendingApprovalAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	first, err := NewFileRuntimeStateStore(dir)
	if err != nil {
		t.Fatalf("NewFileRuntimeStateStore: %v", err)
	}
	run := RunState{
		RunID:     "run_001",
		SessionID: "sess_001",
		Status:    RuntimeRunStatusWaitingApproval,
		CreatedAt: time.Now(),
	}
	if err := first.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	action := ActionRecord{
		ActionID:       "act_001",
		RunID:          run.RunID,
		SessionID:      run.SessionID,
		ApprovalID:     "approval_001",
		Status:         ActionStatusPendingApproval,
		IdempotencyKey: "write_001",
		CreatedAt:      time.Now(),
	}
	if _, _, err := first.FindOrCreateAction(ctx, action); err != nil {
		t.Fatalf("FindOrCreateAction: %v", err)
	}

	second, err := NewFileRuntimeStateStore(dir)
	if err != nil {
		t.Fatalf("reopen FileRuntimeStateStore: %v", err)
	}
	loaded, err := second.GetActionByApprovalID(ctx, action.ApprovalID)
	if err != nil {
		t.Fatalf("GetActionByApprovalID: %v", err)
	}
	if loaded.ActionID != action.ActionID || loaded.Status != ActionStatusPendingApproval {
		t.Fatalf("unexpected persisted action: %#v", loaded)
	}
}
