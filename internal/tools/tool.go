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

// ToolArtifactRef holds a reference to a resource produced or accessed by a tool,
// such as a local file, a URL, or an external resource (calendar event, email, etc.).
// Tools should populate this when they read or write a concrete artifact so that
// the agent and user can trace what was touched.
type ToolArtifactRef struct {
	// Kind classifies the artifact: "file", "url", "calendar_event", "email", "message", etc.
	Kind string `json:"kind"`
	// Label is an optional human-readable display name (e.g. filename, page title).
	Label string `json:"label,omitempty"`
	// URI is a file path or absolute URL that uniquely identifies the artifact.
	URI string `json:"uri,omitempty"`
	// ID is a resource identifier used by external APIs (e.g. email ID, event ID).
	ID string `json:"id,omitempty"`
}

// ToolResult is returned by every tool execution. Fields are populated by the
// tool and must not be mutated by the caller after Execute returns.
type ToolResult struct {
	// ToolCallID echoes the call ID for correlation.
	ToolCallID string
	// ToolName echoes the tool name for correlation.
	ToolName string
	// Success indicates whether the execution completed without error.
	Success bool
	// ContentForLLM is the string representation sent back into the LLM context.
	// Sensitive content may be redacted before this is used.
	ContentForLLM string
	// ContentForUser is the display string shown to the end-user.
	// This may differ from ContentForLLM; it is never redacted.
	ContentForUser string
	// Error is populated when Success is false.
	Error *ToolError

	// ArtifactRef is an optional reference to the primary resource this tool
	// produced or accessed (e.g. a file path, URL, or external resource ID).
	// Set this whenever the tool touches a concrete artifact.
	ArtifactRef *ToolArtifactRef `json:"artifactRef,omitempty"`
	// Metadata holds optional structured key-value pairs for downstream consumers
	// (e.g. line counts, byte sizes, query parameters).
	Metadata map[string]any `json:"metadata,omitempty"`
	// Truncated is true when ContentForLLM or ContentForUser was cut short due to
	// size limits. Consumers can use this to request a more targeted read.
	Truncated bool `json:"truncated,omitempty"`
}

type ToolError struct {
	Code    string
	Message string
}

const (
	ErrorToolNotFound         = "TOOL_NOT_FOUND"
	ErrorInvalidArgument      = "TOOL_INPUT_INVALID"
	ErrorExecutionFailed      = "INTERNAL_ERROR"
	ErrorBlockedByPolicy      = "ACTION_BLOCKED_BY_POLICY"
	ErrorTimeout              = "PROVIDER_TIMEOUT"
	ErrorMaxIterationsReached = "INTERNAL_ERROR"
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
