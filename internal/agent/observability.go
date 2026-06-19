package agent

import (
	"context"

	"vclaw/internal/contracts"
)

// RuntimeObserver receives narrow runtime lifecycle signals for local metrics.
type RuntimeObserver interface {
	RecordRequest(response contracts.AgentResponse, err error)
	RecordToolCall(toolName string, success bool)
	RecordApprovalStateChange(status ActionStatus, pending int)
}

func (r *Runtime) recordRequestObservation(response contracts.AgentResponse, err error) {
	if r == nil || r.observer == nil {
		return
	}
	r.observer.RecordRequest(response, err)
}

func (r *Runtime) recordToolCallObservation(toolName string, success bool) {
	if r == nil || r.observer == nil {
		return
	}
	r.observer.RecordToolCall(toolName, success)
}

func (r *Runtime) recordApprovalObservation(status ActionStatus) {
	if r == nil || r.observer == nil {
		return
	}
	r.observer.RecordApprovalStateChange(status, r.pendingApprovalCount())
}

func (r *Runtime) pendingApprovalCount() int {
	if r == nil {
		return 0
	}
	r.approvalMu.Lock()
	defer r.approvalMu.Unlock()
	return len(r.pendingApprovals)
}

func (r *Runtime) startRequestTelemetry(ctx context.Context, message contracts.UserMessage) (context.Context, func(contracts.AgentResponse, error)) {
	if r == nil || r.telemetry == nil {
		return ctx, func(contracts.AgentResponse, error) {}
	}
	return r.telemetry.StartRequest(ctx, message)
}
