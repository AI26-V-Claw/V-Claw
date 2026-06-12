package agent

import (
	"context"
	"errors"
	"strings"

	"vclaw/internal/contracts"
)

func (r *Runtime) expirePendingApprovalIfNeeded(ctx context.Context, sessionID string) *contracts.ErrorShape {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}

	r.approvalMu.Lock()
	approvalID := r.pendingBySession[sessionID]
	r.approvalMu.Unlock()
	if approvalID == "" {
		return nil
	}

	r.approvalMu.Lock()
	pending, ok := r.pendingApprovals[approvalID]
	r.approvalMu.Unlock()
	if !ok {
		return nil
	}

	if r.now().Before(pending.request.ExpiresAt) {
		return nil
	}

	r.takePendingApproval(sessionID, approvalID)

	if pending.actionID != "" && r.stateStore != nil {
		if _, err := r.stateStore.MarkActionExpired(ctx, pending.actionID); err != nil && !errors.Is(err, ErrRuntimeStateNotFound) {
			return internalError("expire pending approval action: "+err.Error(), contracts.ErrorSourceAgent)
		}
	}
	if errShape := r.finishRunByID(ctx, pending.runID, RuntimeRunStatusFailed); errShape != nil {
		return errShape
	}
	return &contracts.ErrorShape{
		Code:      contracts.ErrorApprovalExpired,
		Message:   "approval expired",
		Source:    contracts.ErrorSourceAgent,
		Retryable: false,
	}
}

func (r *Runtime) clearExpiredApprovalsForSession(ctx context.Context, sessionID string) {
	_ = r.expirePendingApprovalIfNeeded(ctx, strings.TrimSpace(sessionID))
}
