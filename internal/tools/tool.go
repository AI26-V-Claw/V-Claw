package tools

import (
	"context"
	"fmt"
)

type Capability string

const (
	CapabilityReadOnly Capability = "read_only"
	CapabilityMutating Capability = "mutating"
)

type RiskLevel string

const (
	RiskLevelSafeRead      RiskLevel = "safe_read"
	RiskLevelSafeCompute   RiskLevel = "safe_compute"
	RiskLevelSensitiveRead RiskLevel = "sensitive_read"
	RiskLevelExternalWrite RiskLevel = "external_write"
	RiskLevelLocalWrite    RiskLevel = "local_write"
	RiskLevelCodeExecution RiskLevel = "code_execution"
	RiskLevelDestructive   RiskLevel = "destructive"
	RiskLevelBlocked       RiskLevel = "blocked"
)

type ToolSchema map[string]any

type Tool interface {
	Name() string
	Description() string
	Parameters() ToolSchema
	Capability() Capability
	RiskLevel() RiskLevel
	Execute(ctx context.Context, call ToolCall) ToolResult
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

type ToolResult struct {
	ToolCallID     string
	ToolName       string
	Success        bool
	ContentForLLM  string
	ContentForUser string
	Error          *ToolError
}

type ToolError struct {
	Code    string
	Message string
}

const (
	ErrorToolNotFound         = "tool_not_found"
	ErrorInvalidArgument      = "invalid_arguments"
	ErrorExecutionFailed      = "execution_error"
	ErrorBlockedByPolicy      = "permission_denied"
	ErrorTimeout              = "timeout"
	ErrorMaxIterationsReached = "max_iterations_reached"
)

func ToolNotFoundResult(call ToolCall) ToolResult {
	return ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  "Tool not found: " + call.Name,
		ContentForUser: "Không tìm thấy tool: " + call.Name,
		Error: &ToolError{
			Code:    ErrorToolNotFound,
			Message: "tool not found: " + call.Name,
		},
	}
}

func PermissionDeniedResult(call ToolCall) ToolResult {
	return ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  "Permission denied for tool: " + call.Name,
		ContentForUser: "Không có quyền dùng tool: " + call.Name,
		Error: &ToolError{
			Code:    ErrorBlockedByPolicy,
			Message: "tool blocked by policy: " + call.Name,
		},
	}
}

func ExecutionErrorResult(call ToolCall, err error) ToolResult {
	message := "tool execution failed"
	if err != nil {
		message = err.Error()
	}

	return ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  fmt.Sprintf("Tool execution error for %s: %s", call.Name, message),
		ContentForUser: "Tool lỗi khi chạy: " + call.Name,
		Error: &ToolError{
			Code:    ErrorExecutionFailed,
			Message: message,
		},
	}
}
