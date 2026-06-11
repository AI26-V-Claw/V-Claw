package agent

import (
	"context"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

func (r *Runtime) executeInternalPolicyCheckedTool(ctx context.Context, toolCall providers.ToolCall) tools.ToolResult {
	if r == nil || r.registry == nil {
		return tools.ToolNotFoundResult(providerToolCallToToolCall(toolCall))
	}
	definition, found := r.registry.GetDefinition(toolCall.Name)
	if !found {
		definition.Name = toolCall.Name
	}
	now := time.Now
	if r.now != nil {
		now = r.now
	}
	decision := r.policy.DecideToolCall(toolCall.ID, definition, found, now())
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
		return tools.PermissionDeniedResult(providerToolCallToToolCall(toolCall))
	}
	return r.executeAllowedTool(ctx, toolCall, definition)
}
