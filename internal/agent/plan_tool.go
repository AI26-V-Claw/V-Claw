package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

const PlanToolName = "plan"

const planRetentionTTL = 2 * time.Hour

type planScopeKey struct{}

type planScope struct {
	SessionID string
	RunID     string
}

func WithPlanScope(ctx context.Context, sessionID string, runID string) context.Context {
	return context.WithValue(ctx, planScopeKey{}, planScope{SessionID: sessionID, RunID: runID})
}

func planScopeFromContext(ctx context.Context) planScope {
	scope, _ := ctx.Value(planScopeKey{}).(planScope)
	return scope
}

// PlanStore keeps active plans scoped to a single session/run pair. The store
// is in-memory, synchronized, and returns cloned plan snapshots so continuations
// never mutate stored state by accident.
type PlanStore struct {
	mu    sync.RWMutex
	plans map[string]planRecord
}

type planRecord struct {
	Plan      contracts.Plan
	Revision  int64
	UpdatedAt time.Time
}

type PlanMetadata struct {
	Revision  int64
	UpdatedAt time.Time
}

func NewPlanStore() *PlanStore {
	return &PlanStore{plans: make(map[string]planRecord)}
}

func planStoreKey(sessionID string, runID string) string {
	sessionID = strings.TrimSpace(sessionID)
	runID = strings.TrimSpace(runID)
	if sessionID == "" || runID == "" {
		return ""
	}
	return sessionID + "\x00" + runID
}

func (s *PlanStore) Get(sessionID string, runID string) (contracts.Plan, PlanMetadata, bool) {
	key := planStoreKey(sessionID, runID)
	if s == nil || key == "" {
		return contracts.Plan{}, PlanMetadata{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.plans[key]
	if !ok || len(record.Plan.Steps) == 0 {
		return contracts.Plan{}, PlanMetadata{}, false
	}
	return clonePlan(record.Plan), PlanMetadata{Revision: record.Revision, UpdatedAt: record.UpdatedAt}, true
}

func (s *PlanStore) Set(sessionID string, runID string, plan contracts.Plan) (contracts.Plan, PlanMetadata) {
	key := planStoreKey(sessionID, runID)
	if s == nil || key == "" {
		return contracts.Plan{}, PlanMetadata{}
	}
	plan = clonePlan(plan)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(plan.Steps) == 0 {
		delete(s.plans, key)
		return contracts.Plan{}, PlanMetadata{}
	}
	revision := s.plans[key].Revision + 1
	s.plans[key] = planRecord{Plan: plan, Revision: revision, UpdatedAt: now}
	return clonePlan(plan), PlanMetadata{Revision: revision, UpdatedAt: now}
}

func (s *PlanStore) Clear(sessionID string, runID string) {
	key := planStoreKey(sessionID, runID)
	if s == nil || key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.plans, key)
}

func (s *PlanStore) PruneExpired(now time.Time) int {
	if s == nil {
		return 0
	}
	cutoff := now.Add(-planRetentionTTL)
	removed := 0
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, record := range s.plans {
		if record.UpdatedAt.Before(cutoff) {
			delete(s.plans, key)
			removed++
		}
	}
	return removed
}

func shouldRetainPlanForRun(state RunState) bool {
	hasPending := strings.TrimSpace(state.PendingActionID) != "" || strings.TrimSpace(state.PendingClarificationID) != ""
	if !hasPending {
		return false
	}
	switch state.Status {
	case RuntimeRunStatusBlocked, RuntimeRunStatusIterationBudget:
		return true
	default:
		return false
	}
}

func (r *Runtime) finishPlanLifecycle(ctx context.Context, state RunState) {
	if r == nil || r.planStore == nil {
		return
	}
	r.planStore.PruneExpired(r.now())
	plan, meta, ok := r.planStore.Get(state.SessionID, state.RunID)
	if !ok {
		return
	}
	if shouldRetainPlanForRun(state) {
		r.logger.Info("plan retained", "session_id", state.SessionID, "run_id", state.RunID, "step_count", len(plan.Steps), "revision", meta.Revision, "reason", string(state.Status))
		return
	}
	r.planStore.Clear(state.SessionID, state.RunID)
	r.logger.Info("plan cleared", "session_id", state.SessionID, "run_id", state.RunID, "step_count", len(plan.Steps), "revision", meta.Revision, "reason", string(state.Status))
}
func clonePlan(plan contracts.Plan) contracts.Plan {
	if len(plan.Steps) == 0 {
		return contracts.Plan{}
	}
	steps := make([]contracts.PlanStep, len(plan.Steps))
	copy(steps, plan.Steps)
	return contracts.Plan{Steps: steps}
}

type PlanTool struct {
	store *PlanStore
}

type planToolResponse struct {
	Plan     contracts.Plan `json:"plan"`
	Summary  string         `json:"summary"`
	RunID    string         `json:"runId,omitempty"`
	Revision int64          `json:"revision,omitempty"`
}

func NewPlanTool(store *PlanStore) *PlanTool {
	return &PlanTool{store: store}
}

func (*PlanTool) Name() string { return PlanToolName }

func (*PlanTool) Description() string {
	return "Stateful internal planning tool. Use for complex tasks with 3+ steps or multiple tasks. Read the current plan by omitting steps; write by providing the full steps array. Mark completed items completed immediately, keep only one step in_progress when possible, and treat this as housekeeping rather than the final user answer."
}

func (*PlanTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"steps": map[string]any{
				"type":        "array",
				"description": "Full plan replacement. Use for complex tasks with 3+ steps or multiple tasks. Each call should include all active steps, not only the changed step.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":          map[string]any{"type": "string", "description": "Short stable step id."},
						"description": map[string]any{"type": "string", "description": "Human-readable step description."},
						"status":      map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed", "cancelled"}},
					},
					"required":             []string{"description", "status"},
					"additionalProperties": false,
				},
			},
		},
		"additionalProperties": false,
	}
}

func (*PlanTool) Capability() tools.Capability { return tools.CapabilityReadOnly }

func (*PlanTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelSafeCompute }

func (t *PlanTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	if t == nil || t.store == nil {
		return planToolError(call, tools.ErrorExecutionFailed, "plan store is not initialized")
	}
	scope := planScopeFromContext(ctx)
	if strings.TrimSpace(scope.SessionID) == "" {
		return planToolError(call, tools.ErrorInvalidArgument, "plan scope session id is required")
	}
	if strings.TrimSpace(scope.RunID) == "" {
		return planToolError(call, tools.ErrorInvalidArgument, "plan scope run id is required")
	}

	plan, ok, err := planFromArguments(call.Arguments)
	if err != nil {
		return planToolError(call, tools.ErrorInvalidArgument, err.Error())
	}
	var meta PlanMetadata
	if ok {
		plan, meta = t.store.Set(scope.SessionID, scope.RunID, plan)
	} else {
		plan, meta, _ = t.store.Get(scope.SessionID, scope.RunID)
	}

	response := planToolResponse{Plan: plan, Summary: summarizePlan(plan), RunID: scope.RunID, Revision: meta.Revision}
	content, err := json.Marshal(response)
	if err != nil {
		return planToolError(call, tools.ErrorExecutionFailed, err.Error())
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  string(content),
		ContentForUser: response.Summary,
		Metadata:       map[string]any{"summary": response.Summary, "step_count": len(plan.Steps), "revision": meta.Revision},
	}
}

func planFromArguments(args map[string]any) (contracts.Plan, bool, error) {
	if args == nil {
		return contracts.Plan{}, false, nil
	}
	raw, ok := args["steps"]
	if !ok {
		return contracts.Plan{}, false, nil
	}
	rawSteps, ok := raw.([]any)
	if !ok {
		return contracts.Plan{}, true, fmt.Errorf("steps must be an array")
	}
	steps := make([]contracts.PlanStep, 0, len(rawSteps))
	for index, rawStep := range rawSteps {
		stepMap, ok := rawStep.(map[string]any)
		if !ok {
			return contracts.Plan{}, true, fmt.Errorf("steps[%d] must be an object", index)
		}
		description := strings.TrimSpace(planStringValue(stepMap["description"]))
		if description == "" {
			return contracts.Plan{}, true, fmt.Errorf("steps[%d].description is required", index)
		}
		status := strings.TrimSpace(planStringValue(stepMap["status"]))
		if !validPlanStatus(status) {
			return contracts.Plan{}, true, fmt.Errorf("steps[%d].status must be pending, in_progress, completed, or cancelled", index)
		}
		steps = append(steps, contracts.PlanStep{ID: strings.TrimSpace(planStringValue(stepMap["id"])), Description: description, Status: status})
	}
	return contracts.Plan{Steps: steps}, true, nil
}

func validPlanStatus(status string) bool {
	switch status {
	case "pending", "in_progress", "completed", "cancelled":
		return true
	default:
		return false
	}
}

func planStringValue(value any) string {
	text, _ := value.(string)
	return text
}

func summarizePlan(plan contracts.Plan) string {
	counts := map[string]int{}
	for _, step := range plan.Steps {
		counts[step.Status]++
	}
	parts := make([]string, 0, 4)
	for _, status := range []string{"completed", "in_progress", "pending", "cancelled"} {
		if counts[status] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[status], status))
		}
	}
	if len(parts) == 0 {
		return "no active plan"
	}
	return strings.Join(parts, ", ")
}

func planToolError(call tools.ToolCall, code string, message string) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  "Plan tool error: " + message,
		ContentForUser: "Plan tool error: " + message,
		Error:          &tools.ToolError{Code: code, Message: message},
	}
}

func (r *Runtime) responsePlan(sessionID string, runID string) *contracts.Plan {
	if r == nil || r.planStore == nil {
		return nil
	}
	plan, _, ok := r.planStore.Get(sessionID, runID)
	if !ok {
		return nil
	}
	return &plan
}

func (r *Runtime) activePlanPrompt(sessionID string, runID string) string {
	if r == nil || r.planStore == nil {
		return ""
	}
	plan, meta, ok := r.planStore.Get(sessionID, runID)
	if !ok {
		return ""
	}
	data, err := json.Marshal(planToolResponse{Plan: plan, Summary: summarizePlan(plan), RunID: strings.TrimSpace(runID), Revision: meta.Revision})
	if err != nil {
		return ""
	}
	r.logger.Debug("plan prompt injected", "session_id", sessionID, "run_id", runID, "step_count", len(plan.Steps), "revision", meta.Revision, "reason", "active_plan")
	return "<active-plan>\n" + string(data) + "\n</active-plan>"
}

func (r *Runtime) hydratePlanFromTranscript(sessionID string, runID string, transcript []providers.Message) {
	if r == nil || r.planStore == nil {
		return
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	if _, _, ok := r.planStore.Get(sessionID, runID); ok {
		return
	}
	for index := len(transcript) - 1; index >= 0; index-- {
		message := transcript[index]
		if message.Role != providers.MessageRoleTool || strings.TrimSpace(message.Content) == "" {
			continue
		}
		var payload planToolResponse
		if err := json.Unmarshal([]byte(message.Content), &payload); err != nil || !validHydratedPlan(payload, runID) {
			continue
		}
		plan, meta := r.planStore.Set(sessionID, runID, payload.Plan)
		r.logger.Info("plan hydrated", "session_id", sessionID, "run_id", runID, "step_count", len(plan.Steps), "revision", meta.Revision, "reason", "transcript")
		return
	}
}

func validHydratedPlan(payload planToolResponse, runID string) bool {
	if strings.TrimSpace(payload.RunID) == "" || strings.TrimSpace(payload.RunID) != strings.TrimSpace(runID) {
		return false
	}
	if len(payload.Plan.Steps) == 0 || len(payload.Plan.Steps) > 50 {
		return false
	}
	for _, step := range payload.Plan.Steps {
		if strings.TrimSpace(step.Description) == "" || !validPlanStatus(strings.TrimSpace(step.Status)) {
			return false
		}
	}
	return true
}
