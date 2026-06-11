package monitoring

import (
	"testing"
	"time"

	"vclaw/internal/agent"
	"vclaw/internal/contracts"
)

func TestMetricsSnapshot(t *testing.T) {
	startedAt := time.Date(2026, 6, 11, 9, 0, 0, 0, time.FixedZone("+07", 7*60*60))
	metrics := NewMetrics(startedAt)

	metrics.RecordRequest(contracts.AgentResponse{Status: contracts.AgentStatusCompleted}, nil)
	metrics.RecordRequest(contracts.AgentResponse{Status: contracts.AgentStatusFailed}, nil)
	metrics.RecordToolCall("gmail.createDraft", true)
	metrics.RecordToolCall("calendar.createEvent", false)
	metrics.RecordApprovalStateChange(agent.ActionStatusPendingApproval, 1)
	metrics.RecordApprovalStateChange(agent.ActionStatusApproved, 0)
	metrics.RecordApprovalStateChange(agent.ActionStatusRejected, 0)
	metrics.RecordApprovalStateChange(agent.ActionStatusExpired, 0)

	snapshot := metrics.Snapshot(startedAt.Add(10 * time.Second))
	if snapshot.RequestsTotal != 2 {
		t.Fatalf("RequestsTotal = %d, want 2", snapshot.RequestsTotal)
	}
	if snapshot.RequestsFailed != 1 {
		t.Fatalf("RequestsFailed = %d, want 1", snapshot.RequestsFailed)
	}
	if snapshot.ToolCallsTotal != 2 {
		t.Fatalf("ToolCallsTotal = %d, want 2", snapshot.ToolCallsTotal)
	}
	if snapshot.ApprovalsPending != 0 {
		t.Fatalf("ApprovalsPending = %d, want 0", snapshot.ApprovalsPending)
	}
	if snapshot.ApprovalsApprovedTotal != 1 {
		t.Fatalf("ApprovalsApprovedTotal = %d, want 1", snapshot.ApprovalsApprovedTotal)
	}
	if snapshot.ApprovalsRejectedTotal != 1 {
		t.Fatalf("ApprovalsRejectedTotal = %d, want 1", snapshot.ApprovalsRejectedTotal)
	}
	if snapshot.ApprovalsExpiredTotal != 1 {
		t.Fatalf("ApprovalsExpiredTotal = %d, want 1", snapshot.ApprovalsExpiredTotal)
	}
	if snapshot.UptimeSeconds != 10 {
		t.Fatalf("UptimeSeconds = %d, want 10", snapshot.UptimeSeconds)
	}
}
