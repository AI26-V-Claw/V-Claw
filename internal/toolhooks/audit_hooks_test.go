package toolhooks

import (
	"context"
	"errors"
	"testing"
	"time"

	"vclaw/internal/audit"
	"vclaw/internal/tools"
)

func TestAuditHooksBeforeToolLogsRequest(t *testing.T) {
	logger := audit.NewMemoryLogger()
	hooks := AuditHooks{Logger: logger}
	ctx := WithRequestContext(context.Background(), "req_1", "sess_1")

	result, err := hooks.BeforeTool(ctx, PreToolInput{
		ToolCallID: "call_1",
		ToolName:   "calendar.createEvent",
		Input:      map[string]any{"title": "demo"},
		Definition: tools.ToolDefinition{RiskLevel: tools.RiskLevelExternalWrite},
	})
	if err != nil {
		t.Fatalf("BeforeTool() error = %v", err)
	}
	if result.Decision != DecisionAllow {
		t.Fatalf("BeforeTool() decision = %q", result.Decision)
	}
	events, err := logger.Query(audit.Filter{RequestID: "req_1"})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(events) != 1 || events[0].EventType != audit.EventToolRequest {
		t.Fatalf("unexpected events: %#v", events)
	}
}

func TestAuditHooksAfterToolLogsResult(t *testing.T) {
	logger := audit.NewMemoryLogger()
	hooks := AuditHooks{Logger: logger}
	ctx := WithRequestContext(context.Background(), "req_2", "sess_2")
	startedAt := time.Now().Add(-time.Second)

	err := hooks.AfterTool(ctx, PostToolInput{
		ToolCallID: "call_2",
		ToolName:   "safe.count",
		Definition: tools.ToolDefinition{RiskLevel: tools.RiskLevelSafeCompute},
		Result: tools.ToolResult{
			ToolCallID:     "call_2",
			ToolName:       "safe.count",
			Success:        true,
			ContentForLLM:  "ok",
			ContentForUser: "ok",
		},
		StartedAt:  startedAt,
		FinishedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("AfterTool() error = %v", err)
	}
	events, err := logger.Query(audit.Filter{RequestID: "req_2"})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %#v", events)
	}
	if events[0].EventType != audit.EventExecutionResult {
		t.Fatalf("unexpected events: %#v", events)
	}
	if events[0].Status != audit.StatusExecuted {
		t.Fatalf("expected executed status, got %#v", events[0])
	}
}

func TestAuditHooksAfterToolLogsError(t *testing.T) {
	logger := audit.NewMemoryLogger()
	hooks := AuditHooks{Logger: logger}
	ctx := WithRequestContext(context.Background(), "req_3", "sess_3")

	err := hooks.AfterTool(ctx, PostToolInput{
		ToolCallID: "call_3",
		ToolName:   "danger.count",
		Definition: tools.ToolDefinition{RiskLevel: tools.RiskLevelExternalWrite},
		Result: tools.ToolResult{
			ToolCallID: "call_3",
			ToolName:   "danger.count",
			Success:    false,
			Error: &tools.ToolError{
				Code:    tools.ErrorExecutionFailed,
				Message: "boom",
			},
		},
		Err:        errors.New("boom"),
		StartedAt:  time.Now().Add(-time.Second),
		FinishedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("AfterTool() error = %v", err)
	}
	events, _ := logger.Query(audit.Filter{RequestID: "req_3"})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %#v", events)
	}
	if events[0].EventType != audit.EventExecutionResult {
		t.Fatalf("expected execution_result event, got %#v", events[0])
	}
	if events[0].Status != audit.StatusFailed {
		t.Fatalf("expected failed status, got %#v", events[0])
	}
	if events[0].ErrorMessage != "boom" {
		t.Fatalf("expected error message boom, got %#v", events[0])
	}
}
