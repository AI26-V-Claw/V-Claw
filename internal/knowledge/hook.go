package knowledge

import (
	"context"
	"time"

	"vclaw/internal/toolhooks"
)

type Hook struct {
	Service *Service
}

func (h Hook) BeforeTool(context.Context, toolhooks.PreToolInput) (toolhooks.PreToolResult, error) {
	return toolhooks.PreToolResult{Decision: toolhooks.DecisionAllow}, nil
}

func (h Hook) AfterTool(ctx context.Context, input toolhooks.PostToolInput) error {
	if h.Service == nil || !input.Result.Success {
		return nil
	}
	observedAt := input.FinishedAt
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	h.Service.IngestToolResult(context.WithoutCancel(ctx), ingestInput{
		RequestID:  input.RequestID,
		SessionID:  input.SessionID,
		RunID:      input.RunID,
		ToolCallID: input.ToolCallID,
		ToolName:   input.ToolName,
		Input:      input.Input,
		Result:     input.Result,
		ObservedAt: observedAt,
	})
	return nil
}
