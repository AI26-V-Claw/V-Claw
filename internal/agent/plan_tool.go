package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

const PlanToolName = "plan"

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

type PlanStore struct {
	mu    sync.RWMutex
	plans map[string]contracts.Plan
}

func NewPlanStore() *PlanStore {
	return &PlanStore{plans: make(map[string]contracts.Plan)}
}

func (s *PlanStore) Get(sessionID string) (contracts.Plan, bool) {
	if s == nil || strings.TrimSpace(sessionID) == "" {
		return contracts.Plan{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	plan, ok := s.plans[sessionID]
	if !ok || len(plan.Steps) == 0 {
		return contracts.Plan{}, false
	}
	return clonePlan(plan), true
}

func (s *PlanStore) Set(sessionID string, plan contracts.Plan) contracts.Plan {
	if s == nil || strings.TrimSpace(sessionID) == "" {
		return contracts.Plan{}
	}
	plan = clonePlan(plan)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(plan.Steps) == 0 {
		delete(s.plans, sessionID)
		return contracts.Plan{}
	}
	s.plans[sessionID] = plan
	return clonePlan(plan)
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
	Plan    contracts.Plan `json:"plan"`
	Summary string         `json:"summary"`
	RunID   string         `json:"runId,omitempty"`
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

	plan, ok, err := planFromArguments(call.Arguments)
	if err != nil {
		return planToolError(call, tools.ErrorInvalidArgument, err.Error())
	}
	if ok {
		plan = t.store.Set(scope.SessionID, plan)
	} else {
		plan, _ = t.store.Get(scope.SessionID)
	}

	response := planToolResponse{Plan: plan, Summary: summarizePlan(plan), RunID: scope.RunID}
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
		Metadata:       map[string]any{"summary": response.Summary, "step_count": len(plan.Steps)},
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

func (r *Runtime) responsePlan(sessionID string) *contracts.Plan {
	if r == nil || r.planStore == nil {
		return nil
	}
	plan, ok := r.planStore.Get(sessionID)
	if !ok {
		return nil
	}
	return &plan
}

func (r *Runtime) activePlanPrompt(sessionID string) string {
	if r == nil || r.planStore == nil {
		return ""
	}
	plan, ok := r.planStore.Get(sessionID)
	if !ok {
		return ""
	}
	data, err := json.Marshal(planToolResponse{Plan: plan, Summary: summarizePlan(plan)})
	if err != nil {
		return ""
	}
	return "<active-plan>\n" + string(data) + "\n</active-plan>"
}

func (r *Runtime) hydratePlanFromTranscript(sessionID string, transcript []providers.Message) {
	if r == nil || r.planStore == nil {
		return
	}
	if _, ok := r.planStore.Get(sessionID); ok {
		return
	}
	for index := len(transcript) - 1; index >= 0; index-- {
		message := transcript[index]
		if message.Role != providers.MessageRoleTool || strings.TrimSpace(message.Content) == "" {
			continue
		}
		var payload planToolResponse
		if err := json.Unmarshal([]byte(message.Content), &payload); err != nil || len(payload.Plan.Steps) == 0 {
			continue
		}
		r.planStore.Set(sessionID, payload.Plan)
		return
	}
}
