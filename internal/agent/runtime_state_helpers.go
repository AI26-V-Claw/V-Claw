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
	"vclaw/internal/governance"
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
		// Do not resume a cancelled run - treat it as a fresh start.
		if state.Status == RuntimeRunStatusCancelled {
			err = ErrRuntimeStateNotFound
		}
	}
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
		RunID:         runID,
		SessionID:     message.SessionID,
		RequestID:     message.RequestID,
		OriginalGoal:  message.Text,
		Data:          mergeTraceData(nil, ctx),
		Status:        RuntimeRunStatusRunning,
		CreatedAt:     now,
		UpdatedAt:     now,
		Model:         r.model,
		PromptVersion: r.promptVersion,
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

func (r *Runtime) finishRunState(ctx context.Context, state RunState, status RuntimeRunStatus, reason string) (RunState, *contracts.ErrorShape) {
	now := r.now()
	state.Status = status
	state.FailureReason = reason
	state.UpdatedAt = now
	switch status {
	case RuntimeRunStatusCompleted, RuntimeRunStatusFailed, RuntimeRunStatusBlocked, RuntimeRunStatusMaxIterations, RuntimeRunStatusCancelled:
		state.PendingActionID = ""
		state.PendingClarificationID = ""
		state.CompletedAt = &now
	}
	if status == RuntimeRunStatusFailed && strings.TrimSpace(state.ErrorRef) == "" {
		state.ErrorRef = newErrorRef()
	}
	if status == RuntimeRunStatusFailed && strings.TrimSpace(state.ErrorRef) != "" {
		r.attachErrorRefTraceMetadata(ctx, state.RunID, state.ErrorRef)
	}
	persistCtx := context.WithoutCancel(ctx)
	if err := r.stateStore.UpdateRun(persistCtx, state); err != nil {
		return state, internalError("finish run state: "+err.Error(), contracts.ErrorSourceAgent)
	}
	switch status {
	case RuntimeRunStatusCompleted, RuntimeRunStatusFailed, RuntimeRunStatusBlocked, RuntimeRunStatusMaxIterations, RuntimeRunStatusCancelled:
		if r.subtasks != nil {
			r.subtasks.complete(state.RunID)
		}
	}
	return state, nil
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

func (r *Runtime) finishRunByID(ctx context.Context, runID string, status RuntimeRunStatus, reason string) *contracts.ErrorShape {
	state, err := r.stateStore.GetRun(ctx, runID)
	if err != nil {
		return internalError("load run state: "+err.Error(), contracts.ErrorSourceAgent)
	}
	_, errShape := r.finishRunState(ctx, state, status, reason)
	return errShape
}

func newErrorRef() string {
	var bytes [3]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "000000"
	}
	return strings.ToUpper(hex.EncodeToString(bytes[:]))
}

func (r *Runtime) attachErrorRefTraceMetadata(ctx context.Context, runID string, errorRef string) {
	errorRef = strings.ToUpper(strings.TrimSpace(errorRef))
	if errorRef == "" {
		return
	}
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		if r != nil && r.logger != nil {
			r.logger.Warn("error_ref not attached to trace: no active span",
				"error_ref", errorRef,
				"run_id", runID,
			)
		}
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
		Model:             r.model,
		PromptVersion:     r.promptVersion,
		ToolSchemaVersion: r.toolSchemaVersionFor(toolCall.Name),
		PolicyDecisionRef: decision.PolicyDecisionRef,
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
	ctx = context.WithoutCancel(ctx)
	if r.stateStore == nil {
		return nil
	}
	if err := r.stateStore.RecordRiskDecision(ctx, RiskDecisionRecord{
		RunID:             runState.RunID,
		RequestID:         runState.RequestID,
		SessionID:         runState.SessionID,
		ToolCallID:        toolCall.ID,
		ToolName:          toolCall.Name,
		RiskLevel:         decision.RiskLevel,
		Decision:          decision.Decision,
		RequiresApproval:  decision.RequiresApproval,
		Reason:            decision.Reason,
		CheckedAt:         decision.CheckedAt,
		PolicyDecisionRef: decision.PolicyDecisionRef,
	}); err != nil {
		return internalError("record risk decision: "+err.Error(), contracts.ErrorSourceAgent)
	}
	return nil
}

func (r *Runtime) recordRuntimeToolCallStatus(ctx context.Context, runState RunState, toolCall providers.ToolCall, status ToolCallStatus, reason string, approvalID string) *contracts.ErrorShape {
	ctx = context.WithoutCancel(ctx)
	if r.stateStore == nil {
		return nil
	}
	if err := r.stateStore.RecordToolCall(ctx, ToolCallRecord{
		ToolCallID:        toolCall.ID,
		RunID:             runState.RunID,
		RequestID:         runState.RequestID,
		SessionID:         runState.SessionID,
		ToolName:          toolCall.Name,
		ArgsSnapshot:      cloneArguments(toolCall.Arguments),
		Status:            status,
		Reason:            reason,
		ApprovalID:        approvalID,
		CreatedAt:         r.now(),
		Model:             r.model,
		PromptVersion:     r.promptVersion,
		ToolSchemaVersion: r.toolSchemaVersionFor(toolCall.Name),
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
	ctx = context.WithoutCancel(ctx)
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
		ToolCallID:        toolCall.ID,
		RunID:             runID,
		RequestID:         requestID,
		SessionID:         sessionID,
		ToolName:          toolCall.Name,
		ArgsSnapshot:      cloneArguments(toolCall.Arguments),
		Status:            status,
		ApprovalID:        approvalID,
		Result:            &result,
		ErrorMessage:      errorMessage,
		LatencyMS:         latency.Milliseconds(),
		CreatedAt:         r.now(),
		Model:             r.model,
		PromptVersion:     r.promptVersion,
		ToolSchemaVersion: r.toolSchemaVersionFor(toolCall.Name),
		// PolicyDecisionRef and Source are populated when the tool layer
		// stamps them on the ToolResult; if absent here, downstream readers
		// can still cross-reference via run_id + tool_call_id.
		PolicyDecisionRef: toolResultPolicyRef(&result),
		Source:            toolResultSource(&result),
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
	ctx = context.WithoutCancel(ctx)
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
	persistCtx := context.WithoutCancel(ctx)
	if persistCtx == nil {
		persistCtx = context.Background()
	}
	if err := r.stateStore.AddRunCost(persistCtx, runState.RunID, cost); err != nil {
		r.logger.Warn("persist llm cost failed", "run_id", runState.RunID, "error", err)
		return
	}
	runState.CostUSD += cost
}

// toolSchemaVersionFor looks up the registered tool's parameter schema and
// returns its content-hash version. Empty if the tool isn't registered or the
// registry isn't available — caller stores empty rather than failing.
func (r *Runtime) toolSchemaVersionFor(toolName string) string {
	if r == nil || r.registry == nil || strings.TrimSpace(toolName) == "" {
		return ""
	}
	def, ok := r.registry.GetDefinition(toolName)
	if !ok {
		return ""
	}
	return governance.ToolSchemaVersion(def.Parameters)
}

// toolResultPolicyRef extracts the policy decision reference attached to the
// tool result. The runtime stamps result.PolicyDecisionRef from the live
// risk decision (allowed path) or the approved ActionRecord (HITL path)
// before persistence — see runtime.go and runtime_approval.go.
func toolResultPolicyRef(result *tools.ToolResult) string {
	if result == nil {
		return ""
	}
	return strings.TrimSpace(result.PolicyDecisionRef)
}

// toolResultSource mirrors the tool's declared origin attribution onto the
// tool_calls record. The runtime fills result.Source from the registered
// tool's Group right after Execute returns (see stampToolResultSource), so
// individual tool implementations don't have to set it themselves. Tools may
// override Source for unusual cases (e.g. wrapping a raw connector).
func toolResultSource(result *tools.ToolResult) string {
	if result == nil {
		return ""
	}
	return strings.TrimSpace(result.Source)
}

// stampPolicyRef populates PolicyDecisionRef on a risk decision that came from
// the policy layer (which doesn't know the run context). This avoids sprinkling
// governance.PolicyRef calls across every callsite; callers just wrap decisions.
func (r *Runtime) stampPolicyRef(runID string, toolCallID string, decision contracts.RiskDecision) contracts.RiskDecision {
	decision.PolicyDecisionRef = governance.PolicyRef(runID, toolCallID, decision.CheckedAt)
	return decision
}

// buildGovernanceMetadata returns the full provenance bundle that this runtime
// stamps onto every contract record (ToolCall, ToolResult, ApprovalRequest)
// before it crosses an Agent-Core boundary. The bundle pulls Model and
// PromptVersion from the live runtime, computes ToolSchemaVersion from the
// registered tool's parameter schema, and carries through the PolicyDecisionRef
// supplied by the caller (typically the live RiskDecision or the persisted
// ActionRecord). Returns nil when every field is empty so JSON stays compact
// for un-instrumented paths (tests, legacy entry points).
func (r *Runtime) buildGovernanceMetadata(toolName string, policyDecisionRef string) *contracts.GovernanceMetadata {
	if r == nil {
		return nil
	}
	gm := &contracts.GovernanceMetadata{
		Model:             strings.TrimSpace(r.model),
		PromptVersion:     strings.TrimSpace(r.promptVersion),
		ToolSchemaVersion: r.toolSchemaVersionFor(toolName),
		PolicyDecisionRef: strings.TrimSpace(policyDecisionRef),
	}
	if gm.Model == "" && gm.PromptVersion == "" && gm.ToolSchemaVersion == "" && gm.PolicyDecisionRef == "" {
		return nil
	}
	return gm
}

// GovernanceFromActionRecord rebuilds the provenance bundle from a persisted
// ActionRecord. Use this on the restore-from-DB path where the values stored
// when the run started are authoritative — the live runtime may have been
// recreated with a different model/prompt/registry since then. Returns nil
// when every field is empty so JSON stays compact.
func GovernanceFromActionRecord(record ActionRecord) *contracts.GovernanceMetadata {
	gm := &contracts.GovernanceMetadata{
		Model:             strings.TrimSpace(record.Model),
		PromptVersion:     strings.TrimSpace(record.PromptVersion),
		ToolSchemaVersion: strings.TrimSpace(record.ToolSchemaVersion),
		PolicyDecisionRef: strings.TrimSpace(record.PolicyDecisionRef),
	}
	if gm.Model == "" && gm.PromptVersion == "" && gm.ToolSchemaVersion == "" && gm.PolicyDecisionRef == "" {
		return nil
	}
	return gm
}
