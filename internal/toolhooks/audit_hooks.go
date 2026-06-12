package toolhooks

import (
	"context"
	"fmt"
	"strings"

	"vclaw/internal/audit"
	"vclaw/internal/tools"
)

type AuditHooks struct {
	Logger audit.AuditEventLogger
}

func (h AuditHooks) BeforeTool(ctx context.Context, input PreToolInput) (PreToolResult, error) {
	if h.Logger == nil {
		return PreToolResult{Decision: DecisionAllow}, nil
	}
	ctxRequestID, ctxSessionID := RequestContextFrom(ctx)
	requestID := firstNonEmpty(ctxRequestID, input.RequestID, input.ToolCallID)
	sessionID := firstNonEmpty(ctxSessionID, input.SessionID)
	event := audit.NewToolRequestEvent(
		requestID,
		sessionID,
		"",
		input.ToolName,
		actionTypeForDefinition(input.Definition),
		commandPreview(input.ToolName, input.Input),
	)
	if err := h.Logger.Log(event); err != nil {
		return PreToolResult{}, fmt.Errorf("audit pre-tool log failed: %w", err)
	}
	return PreToolResult{Decision: DecisionAllow}, nil
}

func (h AuditHooks) AfterTool(ctx context.Context, input PostToolInput) error {
	if h.Logger == nil {
		return nil
	}
	ctxRequestID, ctxSessionID := RequestContextFrom(ctx)
	requestID := firstNonEmpty(ctxRequestID, input.RequestID, input.ToolCallID)
	sessionID := firstNonEmpty(ctxSessionID, input.SessionID)
	base := audit.NewToolRequestEvent(
		requestID,
		sessionID,
		"",
		input.ToolName,
		actionTypeForDefinition(input.Definition),
		commandPreview(input.ToolName, input.Input),
	)
	start := audit.NewExecutionStartEvent(base, "")
	if err := h.Logger.Log(start); err != nil {
		return fmt.Errorf("audit execution-start log failed: %w", err)
	}
	if input.Err != nil {
		failed := base
		failed.ErrorMessage = input.Err.Error()
		failed.Status = audit.StatusFailed
		return h.Logger.Log(failed)
	}
	resultStatus := "failed"
	switch {
	case input.Result.Success:
		resultStatus = "success"
	case input.Result.Error != nil && input.Result.Error.Code == tools.ErrorTimeout:
		resultStatus = "timeout"
	}
	duration := input.FinishedAt.Sub(input.StartedAt).Milliseconds()
	event := audit.NewExecutionResultEvent(
		base,
		"",
		resultStatus,
		0,
		duration,
		audit.SummariseOutput(input.Result.ContentForLLM, "", 200),
		false,
	)
	if !input.Result.Success && input.Result.Error != nil {
		event.ErrorMessage = input.Result.Error.Message
	}
	return h.Logger.Log(event)
}

func actionTypeForDefinition(definition tools.ToolDefinition) audit.ActionType {
	switch definition.RiskLevel {
	case tools.RiskLevelCodeExecution:
		if strings.Contains(definition.Name, "Python") || strings.Contains(definition.Name, "python") {
			return audit.ActionRunPython
		}
		if strings.Contains(definition.Name, "Shell") || strings.Contains(definition.Name, "shell") {
			return audit.ActionRunShell
		}
		return audit.ActionRunShell
	case tools.RiskLevelDestructive:
		return audit.ActionFileDelete
	case tools.RiskLevelLocalWrite:
		return audit.ActionFileWrite
	case tools.RiskLevelSafeRead, tools.RiskLevelSensitiveRead:
		return audit.ActionFileRead
	default:
		return audit.ActionNetworkAccess
	}
}

func commandPreview(toolName string, input map[string]any) string {
	if len(input) == 0 {
		return toolName
	}
	if command, ok := input["command"].(string); ok && strings.TrimSpace(command) != "" {
		return strings.TrimSpace(command)
	}
	if code, ok := input["code"].(string); ok && strings.TrimSpace(code) != "" {
		return strings.TrimSpace(code)
	}
	if scriptPath, ok := input["script_path"].(string); ok && strings.TrimSpace(scriptPath) != "" {
		return strings.TrimSpace(scriptPath)
	}
	return toolName
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
