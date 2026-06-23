package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/governance"
	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

func (r *Runtime) toolContentForProvider(toolName string, content string) string {
	return enrichToolContentForLLM(toolName, content, runtimeLocalLocation(r))
}

func runtimeLocalLocation(r *Runtime) *time.Location {
	if r != nil && r.localLocation != nil {
		return r.localLocation
	}
	now := time.Now
	if r != nil && r.now != nil {
		now = r.now
	}
	return now().Location()
}

func enrichToolContentForLLM(toolName string, content string, location *time.Location) string {
	if toolName != "gmail.listEmails" {
		return content
	}
	if location == nil {
		location = time.Local
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return content
	}
	rawMessages, ok := payload["Messages"].([]any)
	if !ok {
		return content
	}
	for _, rawMessage := range rawMessages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			continue
		}
		internalDate := int64Value(message["InternalDate"])
		if internalDate <= 0 {
			continue
		}
		localTime := time.UnixMilli(internalDate).In(location)
		message["LocalDate"] = localTime.Format("2006-01-02")
		message["LocalDateTime"] = localTime.Format(time.RFC3339)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return content
	}
	return string(data)
}

func int64Value(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		parsed, err := v.Int64()
		if err == nil {
			return parsed
		}
	}
	return 0
}

func prependToolResultIfMissing(results []contracts.ToolResult, result contracts.ToolResult) []contracts.ToolResult {
	for _, existing := range results {
		if strings.TrimSpace(existing.ToolCallID) != "" && existing.ToolCallID == result.ToolCallID {
			return results
		}
	}
	merged := make([]contracts.ToolResult, 0, len(results)+1)
	merged = append(merged, result)
	merged = append(merged, results...)
	return merged
}

func (r *Runtime) prepareParallelBatch(ctx context.Context, toolCalls []providers.ToolCall, enabled bool, userText string, evidenceText string, activeClarification bool) ([]parallelToolCall, bool) {
	if !enabled || len(toolCalls) < 2 || r == nil || r.registry == nil {
		return nil, false
	}

	batch := make([]parallelToolCall, 0, len(toolCalls))
	now := r.now()
	for _, originalCall := range toolCalls {
		toolCall := originalCall
		toolCall.Arguments = cloneArguments(originalCall.Arguments)
		if toolCall.Name == PlanToolName {
			return nil, false
		}
		toolCall = sanitizeUnsupportedOptionalArguments(toolCall, evidenceText)
		if isClarifyToolCall(toolCall) {
			return nil, false
		}
		toolCall = normalizeProviderToolCall(now, toolCall, userText)

		definition, found := r.registry.GetDefinition(toolCall.Name)
		if !found {
			return nil, false
		}
		if definition.Name == "" {
			definition.Name = toolCall.Name
		}
		if len(pendingMissingFieldsForToolCall(toolCall, definition, found, activeClarification, userText)) > 0 {
			return nil, false
		}
		decision := r.decideToolCall(ctx, toolCall, definition, found)
		if decision.Decision != contracts.RiskDecisionAllow || decision.RequiresApproval {
			return nil, false
		}
		tool, ok := r.registry.GetTool(toolCall.Name)
		if !ok || tool == nil || !r.policy.CanRunInParallel(tool) {
			return nil, false
		}
		batch = append(batch, parallelToolCall{
			call:       toolCall,
			definition: definition,
			tool:       tool,
		})
	}
	return batch, len(batch) > 1
}

func (r *Runtime) executeInternalPolicyCheckedTool(ctx context.Context, toolCall providers.ToolCall) tools.ToolResult {
	if r == nil || r.registry == nil {
		return tools.ToolNotFoundResult(providerToolCallToToolCall(toolCall))
	}
	definition, found := r.registry.GetDefinition(toolCall.Name)
	if !found {
		definition.Name = toolCall.Name
	}
	decision := r.decideToolCall(ctx, toolCall, definition, found)
	if r.logger != nil {
		r.logger.Info("internal tool call proposed",
			"tool_call_id", toolCall.ID,
			"tool_name", toolCall.Name,
			"decision", decision.Decision,
			"risk_level", decision.RiskLevel,
			"arguments", logToolArguments(toolCall.Name, toolCall.Arguments),
		)
	}
	if decision.Decision != contracts.RiskDecisionAllow {
		return toolDecisionDeniedResult(toolCall, decision)
	}
	return r.executeAllowedTool(ctx, toolCall, definition)
}

func sanitizeToolResult(result tools.ToolResult, definition tools.ToolDefinition) tools.ToolResult {
	return tools.RedactResult(result, definition.RiskLevel)
}

// stampToolResultSource fills result.Source from the registered tool's Group
// when the tool layer didn't already provide one. This keeps tool
// implementations free of governance bookkeeping while still producing a
// usable Source attribution (e.g. "tool:google_workspace", "tool:sandbox")
// for audit/N4. Tools may set their own Source — e.g. a wrapper that calls a
// raw connector — and that value is preserved.
func stampToolResultSource(result tools.ToolResult, definition tools.ToolDefinition) tools.ToolResult {
	if strings.TrimSpace(result.Source) != "" {
		return result
	}
	group := strings.TrimSpace(definition.Group)
	if group == "" {
		return result
	}
	result.Source = governance.SourceToolPrefix + group
	return result
}

func convertToolArtifactRef(ref *tools.ToolArtifactRef) *contracts.ArtifactRef {
	if ref == nil {
		return nil
	}
	return &contracts.ArtifactRef{
		Kind:  ref.Kind,
		Label: ref.Label,
		URI:   ref.URI,
		ID:    ref.ID,
		Meta:  ref.Meta,
	}
}

func cloneMetadataMap(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
