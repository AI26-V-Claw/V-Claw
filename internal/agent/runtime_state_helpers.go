package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

func (r *Runtime) startRunState(ctx context.Context, message contracts.UserMessage) (RunState, *contracts.ErrorShape) {
	if r.stateStore == nil {
		return RunState{}, internalError("runtime state store is required", contracts.ErrorSourceAgent)
	}
	now := r.now()
	runID := runIDForMessage(message)
	state, err := r.stateStore.GetRun(ctx, runID)
	if err == nil {
		state.SessionID = message.SessionID
		state.RequestID = message.RequestID
		if strings.TrimSpace(state.OriginalGoal) == "" {
			state.OriginalGoal = message.Text
		}
		state.Status = RuntimeRunStatusRunning
		state.PendingActionID = ""
		state.PendingClarificationID = ""
		state.UpdatedAt = now
		state.CompletedAt = nil
		if err := r.stateStore.UpdateRun(ctx, state); err != nil {
			return RunState{}, internalError("update run state: "+err.Error(), contracts.ErrorSourceAgent)
		}
		return state, nil
	}
	if !errors.Is(err, ErrRuntimeStateNotFound) {
		return RunState{}, internalError("load run state: "+err.Error(), contracts.ErrorSourceAgent)
	}
	state = RunState{
		RunID:        runID,
		SessionID:    message.SessionID,
		RequestID:    message.RequestID,
		OriginalGoal: message.Text,
		Status:       RuntimeRunStatusRunning,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := r.stateStore.CreateRun(ctx, state); err != nil {
		return RunState{}, internalError("create run state: "+err.Error(), contracts.ErrorSourceAgent)
	}
	return state, nil
}

func (r *Runtime) updateRunState(ctx context.Context, state RunState) *contracts.ErrorShape {
	if r.stateStore == nil {
		return internalError("runtime state store is required", contracts.ErrorSourceAgent)
	}
	state.UpdatedAt = r.now()
	if err := r.stateStore.UpdateRun(ctx, state); err != nil {
		return internalError("update run state: "+err.Error(), contracts.ErrorSourceAgent)
	}
	return nil
}

func (r *Runtime) finishRunState(ctx context.Context, state RunState, status RuntimeRunStatus) *contracts.ErrorShape {
	now := r.now()
	state.Status = status
	state.UpdatedAt = now
	switch status {
	case RuntimeRunStatusCompleted, RuntimeRunStatusFailed, RuntimeRunStatusBlocked, RuntimeRunStatusMaxIterations:
		state.PendingActionID = ""
		state.PendingClarificationID = ""
		state.CompletedAt = &now
	}
	if err := r.stateStore.UpdateRun(ctx, state); err != nil {
		return internalError("finish run state: "+err.Error(), contracts.ErrorSourceAgent)
	}
	return nil
}

func (r *Runtime) resumeRunState(ctx context.Context, runID string) *contracts.ErrorShape {
	state, err := r.stateStore.GetRun(ctx, runID)
	if err != nil {
		return internalError("load run state: "+err.Error(), contracts.ErrorSourceAgent)
	}
	state.Status = RuntimeRunStatusRunning
	state.PendingActionID = ""
	state.PendingClarificationID = ""
	state.CompletedAt = nil
	return r.updateRunState(ctx, state)
}

func (r *Runtime) finishRunByID(ctx context.Context, runID string, status RuntimeRunStatus) *contracts.ErrorShape {
	state, err := r.stateStore.GetRun(ctx, runID)
	if err != nil {
		return internalError("load run state: "+err.Error(), contracts.ErrorSourceAgent)
	}
	return r.finishRunState(ctx, state, status)
}

func runIDForMessage(message contracts.UserMessage) string {
	if runID, ok := metadataString(message.Metadata, "runId"); ok {
		return "run_" + safeID(runID)
	}
	if runID, ok := metadataString(message.Metadata, "runID"); ok {
		return "run_" + safeID(runID)
	}
	return "run_" + safeID(message.SessionID+"_"+message.RequestID)
}

func metadataString(metadata map[string]any, key string) (string, bool) {
	if metadata == nil {
		return "", false
	}
	value, ok := metadata[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	text = strings.TrimSpace(text)
	return text, ok && text != ""
}

func actionIDFor(runID string, toolCall providers.ToolCall) string {
	return "act_" + safeID(runID+"_"+toolCall.ID)
}

func actionIdempotencyKey(runID string, toolCall providers.ToolCall) string {
	payload := map[string]any{
		"run_id":    runID,
		"tool_name": toolCall.Name,
		"arguments": cloneArguments(toolCall.Arguments),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(runID + "|" + toolCall.Name)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (r *Runtime) createApprovalAction(ctx context.Context, runState RunState, message contracts.UserMessage, toolCall providers.ToolCall, decision contracts.RiskDecision, approval contracts.ApprovalRequest) (ActionRecord, *contracts.ErrorShape) {
	key := actionIdempotencyKey(runState.RunID, toolCall)
	now := r.now()
	record := ActionRecord{
		ActionID:          actionIDFor(runState.RunID, toolCall),
		RunID:             runState.RunID,
		SessionID:         message.SessionID,
		RequestID:         message.RequestID,
		ToolCallID:        toolCall.ID,
		ToolName:          toolCall.Name,
		ArgsSnapshot:      cloneArguments(toolCall.Arguments),
		RiskLevel:         decision.RiskLevel,
		Status:            ActionStatusPendingApproval,
		ApprovalID:        approval.ApprovalID,
		ApprovalExpiresAt: approval.ExpiresAt,
		IdempotencyKey:    key,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	stored, _, err := r.stateStore.FindOrCreateAction(ctx, record)
	if err != nil {
		return ActionRecord{}, internalError("create action record: "+err.Error(), contracts.ErrorSourceAgent)
	}
	return stored, nil
}

func (r *Runtime) recordRuntimeToolCall(ctx context.Context, runID string, toolCall providers.ToolCall, result tools.ToolResult, latency time.Duration) *contracts.ErrorShape {
	if r.stateStore == nil {
		return nil
	}
	status := ToolCallStatusCompleted
	errorMessage := ""
	if !result.Success {
		status = ToolCallStatusFailed
		if result.Error != nil {
			errorMessage = result.Error.Message
		}
	}
	if err := r.stateStore.RecordToolCall(ctx, ToolCallRecord{
		ToolCallID:   toolCall.ID,
		RunID:        runID,
		ToolName:     toolCall.Name,
		ArgsSnapshot: cloneArguments(toolCall.Arguments),
		Status:       status,
		Result:       &result,
		ErrorMessage: errorMessage,
		LatencyMS:    latency.Milliseconds(),
		CreatedAt:    r.now(),
	}); err != nil {
		return internalError("record tool call: "+err.Error(), contracts.ErrorSourceAgent)
	}
	return nil
}
