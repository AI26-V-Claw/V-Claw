package toolhooks

import (
	"context"
	"time"

	"vclaw/internal/tools"
)

type Decision string

const (
	DecisionAllow            Decision = "allow"
	DecisionBlock            Decision = "block"
	DecisionRequiresApproval Decision = "requires_approval"
)

type PreToolInput struct {
	RequestID  string
	SessionID  string
	ToolCallID string
	ToolName   string
	Input      map[string]any
	Definition tools.ToolDefinition
	OccurredAt time.Time
	Source     string
}

type PreToolResult struct {
	Decision Decision
	Reason   string
}

type PostToolInput struct {
	RequestID  string
	SessionID  string
	ToolCallID string
	ToolName   string
	Input      map[string]any
	Definition tools.ToolDefinition
	Result     tools.ToolResult
	Err        error
	StartedAt  time.Time
	FinishedAt time.Time
	Source     string
}

type Hooks interface {
	BeforeTool(ctx context.Context, input PreToolInput) (PreToolResult, error)
	AfterTool(ctx context.Context, input PostToolInput) error
}

type NoopHooks struct{}

func (NoopHooks) BeforeTool(context.Context, PreToolInput) (PreToolResult, error) {
	return PreToolResult{Decision: DecisionAllow}, nil
}

func (NoopHooks) AfterTool(context.Context, PostToolInput) error {
	return nil
}
