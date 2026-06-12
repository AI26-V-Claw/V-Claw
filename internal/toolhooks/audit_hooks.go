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
	resultStatus := resultStatusForPostTool(input)
	duration := input.FinishedAt.Sub(input.StartedAt).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	event := audit.NewExecutionResultEvent(
		base,
		input.JobID,
		resultStatus,
		input.ExitCode,
		duration,
		audit.SummariseOutput(input.Result.ContentForLLM, "", 200),
		input.OutputTruncated,
	)
	if message := executionErrorMessage(input); message != "" {
		event.ErrorMessage = message
	}
	return h.Logger.Log(event)
}

func resultStatusForPostTool(input PostToolInput) string {
	switch {
	case input.Result.Success:
		return "success"
	case input.Result.Error != nil && input.Result.Error.Code == tools.ErrorTimeout:
		return "timeout"
	default:
		return "failed"
	}
}

func executionErrorMessage(input PostToolInput) string {
	if input.Result.Error != nil && strings.TrimSpace(input.Result.Error.Message) != "" {
		return strings.TrimSpace(input.Result.Error.Message)
	}
	if input.Err != nil {
		return strings.TrimSpace(input.Err.Error())
	}
	return ""
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
