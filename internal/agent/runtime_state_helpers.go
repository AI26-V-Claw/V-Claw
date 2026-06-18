package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/tools"
	"vclaw/internal/traceutil"
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
		state.Data = mergeTraceData(state.Data, ctx)
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
		Data:         mergeTraceData(nil, ctx),
		Status:       RuntimeRunStatusRunning,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := r.stateStore.CreateRun(ctx, state); err != nil {
		return RunState{}, internalError("create run state: "+err.Error(), contracts.ErrorSourceAgent)
	}
	return state, nil
}

func mergeTraceData(data map[string]any, ctx context.Context) map[string]any {
	traceID := traceutil.TraceIDFromContext(ctx)
	if traceID == "" {
		return cloneRunData(data)
	}
	merged := cloneRunData(data)
	if merged == nil {
		merged = map[string]any{}
	}
	merged["trace_id"] = traceID
	return merged
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
	if status == RuntimeRunStatusFailed && strings.TrimSpace(state.ErrorRef) == "" {
		state.ErrorRef = newErrorRef()
	}
	if status == RuntimeRunStatusFailed && strings.TrimSpace(state.ErrorRef) != "" {
		attachErrorRefTraceMetadata(ctx, state.ErrorRef)
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

func newErrorRef() string {
	var bytes [3]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "000000"
	}
	return strings.ToUpper(hex.EncodeToString(bytes[:]))
}

func attachErrorRefTraceMetadata(ctx context.Context, errorRef string) {
	errorRef = strings.ToUpper(strings.TrimSpace(errorRef))
	if errorRef == "" {
		return
	}
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return
	}
	span.SetAttributes(attribute.String("langfuse.trace.metadata.error_ref", errorRef))
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
		ApprovalSummary:   approval.Summary,
		ApprovalDetails:   approval.Details,
		ApprovalExpiresAt: approval.ExpiresAt,
		IdempotencyKey:    key,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	stored, _, err := r.stateStore.FindOrCreateAction(ctx, record)
	if err != nil {
		return ActionRecord{}, internalError("create action record: "+err.Error(), contracts.ErrorSourceAgent)
	}
	r.appendRunEvent(ctx, runState.RunID, "approval.requested", map[string]any{
		"approvalId": approval.ApprovalID,
		"toolName":   toolCall.Name,
		"riskLevel":  string(decision.RiskLevel),
		"summary":    approval.Summary,
	})
	return stored, nil
}

func (r *Runtime) recordRuntimeRiskDecision(ctx context.Context, runState RunState, toolCall providers.ToolCall, decision contracts.RiskDecision) *contracts.ErrorShape {
	if r.stateStore == nil {
		return nil
	}
	if err := r.stateStore.RecordRiskDecision(ctx, RiskDecisionRecord{
		RunID:            runState.RunID,
		RequestID:        runState.RequestID,
		SessionID:        runState.SessionID,
		ToolCallID:       toolCall.ID,
		ToolName:         toolCall.Name,
		RiskLevel:        decision.RiskLevel,
		Decision:         decision.Decision,
		RequiresApproval: decision.RequiresApproval,
		Reason:           decision.Reason,
		CheckedAt:        decision.CheckedAt,
	}); err != nil {
		return internalError("record risk decision: "+err.Error(), contracts.ErrorSourceAgent)
	}
	return nil
}

func (r *Runtime) recordRuntimeToolCallStatus(ctx context.Context, runState RunState, toolCall providers.ToolCall, status ToolCallStatus, reason string, approvalID string) *contracts.ErrorShape {
	if r.stateStore == nil {
		return nil
	}
	if err := r.stateStore.RecordToolCall(ctx, ToolCallRecord{
		ToolCallID:   toolCall.ID,
		RunID:        runState.RunID,
		RequestID:    runState.RequestID,
		SessionID:    runState.SessionID,
		ToolName:     toolCall.Name,
		ArgsSnapshot: cloneArguments(toolCall.Arguments),
		Status:       status,
		Reason:       reason,
		ApprovalID:   approvalID,
		CreatedAt:    r.now(),
	}); err != nil {
		return internalError("record tool call: "+err.Error(), contracts.ErrorSourceAgent)
	}
	return nil
}

func (r *Runtime) appendRunEvent(ctx context.Context, runID string, eventType string, data map[string]any) {
	if r == nil || r.stateStore == nil || strings.TrimSpace(runID) == "" || strings.TrimSpace(eventType) == "" {
		return
	}
	_ = r.stateStore.AppendRunEvent(ctx, RunEvent{
		RunID:     runID,
		Type:      eventType,
		Data:      data,
		CreatedAt: r.now(),
	})
}

func (r *Runtime) recordRuntimeToolCall(ctx context.Context, runState *RunState, runID string, toolCall providers.ToolCall, result tools.ToolResult, latency time.Duration, approvalID string) *contracts.ErrorShape {
	r.recordToolCallObservation(toolCall.Name, result.Success)
	if r != nil && r.telemetry != nil {
		r.telemetry.RecordToolCall(ctx, toolCall, result, latency)
	}
	if r.stateStore == nil {
		return nil
	}
	requestID := ""
	sessionID := ""
	if runState, err := r.stateStore.GetRun(ctx, runID); err == nil {
		requestID = runState.RequestID
		sessionID = runState.SessionID
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
		RequestID:    requestID,
		SessionID:    sessionID,
		ToolName:     toolCall.Name,
		ArgsSnapshot: cloneArguments(toolCall.Arguments),
		Status:       status,
		ApprovalID:   approvalID,
		Result:       &result,
		ErrorMessage: errorMessage,
		LatencyMS:    latency.Milliseconds(),
		CreatedAt:    r.now(),
	}); err != nil {
		return internalError("record tool call: "+err.Error(), contracts.ErrorSourceAgent)
	}
	if errShape := r.recordRunStep(ctx, runState, runID, RunStep{
		OK:   result.Success,
		Text: strings.TrimSpace(result.ContentForUser),
	}); errShape != nil {
		return errShape
	}
	return nil
}

func (r *Runtime) recordRunStep(ctx context.Context, runState *RunState, runID string, step RunStep) *contracts.ErrorShape {
	if r == nil || r.stateStore == nil || strings.TrimSpace(runID) == "" {
		return nil
	}
	if err := r.stateStore.AppendRunStep(ctx, runID, step); err != nil {
		return internalError("append run step: "+err.Error(), contracts.ErrorSourceAgent)
	}
	if runState != nil {
		runState.Steps = append(runState.Steps, step)
	}
	return nil
}

func (r *Runtime) recordLLMUsageCost(ctx context.Context, runState *RunState, usage *providers.Usage) {
	if r == nil || r.stateStore == nil || usage == nil || runState == nil {
		return
	}
	cost := float64(usage.PromptTokens)*0.000003 + float64(usage.CompletionTokens)*0.000015
	if cost <= 0 {
		return
	}
	if err := r.stateStore.AddRunCost(ctx, runState.RunID, cost); err != nil {
		r.logger.Warn("persist llm cost failed", "run_id", runState.RunID, "error", err)
		return
	}
	runState.CostUSD += cost
}
