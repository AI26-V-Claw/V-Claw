package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/toolhooks"
	"vclaw/internal/tools"
)

func (r *Runtime) decideToolCall(ctx context.Context, toolCall providers.ToolCall, definition tools.ToolDefinition, found bool) contracts.RiskDecision {
	now := time.Now
	if r != nil && r.now != nil {
		now = r.now
	}
	requestID, sessionID := toolhooks.RequestContextFrom(ctx)

	hookApproval := false
	if r != nil && r.toolHooks != nil {
		preResult, err := r.toolHooks.BeforeTool(ctx, toolhooks.PreToolInput{
			RequestID:  requestID,
			SessionID:  sessionID,
			ToolCallID: toolCall.ID,
			ToolName:   toolCall.Name,
			Input:      cloneArguments(toolCall.Arguments),
			Definition: definition,
			OccurredAt: now(),
			Source:     "agent_runtime",
		})
		if err != nil {
			return blockedToolDecision(toolCall, definition, found, now(), "pre-tool hook failed: "+err.Error())
		}
		switch preResult.Decision {
		case toolhooks.DecisionBlock:
			return blockedToolDecision(toolCall, definition, found, now(), firstNonEmpty(strings.TrimSpace(preResult.Reason), "pre-tool hook blocked the tool call"))
		case toolhooks.DecisionRequiresApproval:
			hookApproval = true
		}
	}

	policyDecision := r.policy.DecideToolCall(toolCall.ID, definition, found, now())
	if policyDecision.Decision == contracts.RiskDecisionBlock {
		return policyDecision
	}
	if hookApproval && policyDecision.Decision == contracts.RiskDecisionAllow {
		policyDecision.Decision = contracts.RiskDecisionRequiresApproval
		policyDecision.RequiresApproval = true
		policyDecision.Reason = firstNonEmpty(policyDecision.Reason, "pre-tool hook requires approval")
	}
	return policyDecision
}

func (r *Runtime) approvedToolDecision(ctx context.Context, toolCall providers.ToolCall, definition tools.ToolDefinition, found bool) contracts.RiskDecision {
	now := time.Now
	if r != nil && r.now != nil {
		now = r.now
	}
	requestID, sessionID := toolhooks.RequestContextFrom(ctx)

	if r != nil && r.toolHooks != nil {
		preResult, err := r.toolHooks.BeforeTool(ctx, toolhooks.PreToolInput{
			RequestID:  requestID,
			SessionID:  sessionID,
			ToolCallID: toolCall.ID,
			ToolName:   toolCall.Name,
			Input:      cloneArguments(toolCall.Arguments),
			Definition: definition,
			OccurredAt: now(),
			Source:     "agent_runtime",
		})
		if err != nil {
			return blockedToolDecision(toolCall, definition, found, now(), "pre-tool hook failed: "+err.Error())
		}
		if preResult.Decision == toolhooks.DecisionBlock {
			return blockedToolDecision(toolCall, definition, found, now(), firstNonEmpty(strings.TrimSpace(preResult.Reason), "pre-tool hook blocked the tool call"))
		}
	}

	policyDecision := r.policy.DecideToolCall(toolCall.ID, definition, found, now())
	if policyDecision.Decision == contracts.RiskDecisionBlock {
		return policyDecision
	}
	return policyDecision
}

func (r *Runtime) runPostToolHook(ctx context.Context, toolCall providers.ToolCall, definition tools.ToolDefinition, result tools.ToolResult, execErr error, startedAt time.Time) {
	if r == nil || r.toolHooks == nil {
		return
	}
	requestID, sessionID := toolhooks.RequestContextFrom(ctx)
	if err := r.toolHooks.AfterTool(ctx, toolhooks.PostToolInput{
		RunID:      parentRunIDFromContext(ctx),
		RequestID:  requestID,
		SessionID:  sessionID,
		ToolCallID: toolCall.ID,
		ToolName:   toolCall.Name,
		Input:      cloneArguments(toolCall.Arguments),
		Definition: definition,
		Result:     result,
		Err:        execErr,
		StartedAt:  startedAt,
		FinishedAt: r.now(),
		Source:     "agent_runtime",
	}); err != nil && r.logger != nil {
		r.logger.Warn("post-tool hook failed",
			"tool_call_id", toolCall.ID,
			"tool_name", toolCall.Name,
			"error", err.Error(),
		)
	}
}

func blockedToolDecision(toolCall providers.ToolCall, definition tools.ToolDefinition, found bool, checkedAt time.Time, reason string) contracts.RiskDecision {
	risk := contracts.RiskLevelDestructive
	if found && definition.RiskLevel != "" {
		risk = contracts.RiskLevel(definition.RiskLevel)
	}
	return contracts.RiskDecision{
		ToolCallID: toolCall.ID,
		ToolName:   firstNonEmpty(definition.Name, toolCall.Name),
		RiskLevel:  risk,
		Decision:   contracts.RiskDecisionBlock,
		Reason:     strings.TrimSpace(reason),
		CheckedAt:  checkedAt,
	}
}

func toolDecisionDeniedResult(toolCall providers.ToolCall, decision contracts.RiskDecision) tools.ToolResult {
	call := providerToolCallToToolCall(toolCall)
	message := strings.TrimSpace(decision.Reason)
	if message == "" {
		message = fmt.Sprintf("tool %s blocked before execution", toolCall.Name)
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  "Tool blocked before execution: " + message,
		ContentForUser: "Tool bị chặn trước khi chạy: " + call.Name,
		Error: &tools.ToolError{
			Code:    tools.ErrorBlockedByPolicy,
			Message: message,
		},
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
