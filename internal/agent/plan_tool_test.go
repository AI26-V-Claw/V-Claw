package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vclaw/internal/tools"
)

func TestPlanToolWriteAndReadReturnsFullPlanAndSummary(t *testing.T) {
	store := NewPlanStore()
	tool := NewPlanTool(store)
	ctx := WithPlanScope(context.Background(), "session-1", "run-1")

	write := tool.Execute(ctx, tools.ToolCall{
		ID:   "call-1",
		Name: PlanToolName,
		Arguments: map[string]any{
			"steps": []any{
				map[string]any{"id": "1", "description": "Read project data", "status": "completed"},
				map[string]any{"id": "2", "description": "Call required tools", "status": "in_progress"},
				map[string]any{"id": "3", "description": "Summarize final answer", "status": "pending"},
			},
		},
	})

	if !write.Success {
		t.Fatalf("write failed: %+v", write.Error)
	}
	if !strings.Contains(write.ContentForLLM, "1 completed") || !strings.Contains(write.ContentForLLM, "1 in_progress") || !strings.Contains(write.ContentForLLM, "1 pending") {
		t.Fatalf("write summary missing counts: %s", write.ContentForLLM)
	}

	read := tool.Execute(ctx, tools.ToolCall{ID: "call-2", Name: PlanToolName, Arguments: map[string]any{}})
	if !read.Success {
		t.Fatalf("read failed: %+v", read.Error)
	}

	var payload planToolResponse
	if err := json.Unmarshal([]byte(read.ContentForLLM), &payload); err != nil {
		t.Fatalf("read content is not JSON: %v\n%s", err, read.ContentForLLM)
	}
	if len(payload.Plan.Steps) != 3 {
		t.Fatalf("expected full plan with 3 steps, got %d", len(payload.Plan.Steps))
	}
	if payload.Summary != "1 completed, 1 in_progress, 1 pending" {
		t.Fatalf("unexpected summary: %q", payload.Summary)
	}
}

func TestPlanToolRejectsInvalidStatus(t *testing.T) {
	tool := NewPlanTool(NewPlanStore())
	result := tool.Execute(WithPlanScope(context.Background(), "session-1", "run-1"), tools.ToolCall{
		ID:   "call-1",
		Name: PlanToolName,
		Arguments: map[string]any{
			"steps": []any{map[string]any{"id": "1", "description": "Bad", "status": "started"}},
		},
	})

	if result.Success {
		t.Fatal("expected invalid status to fail")
	}
	if result.Error == nil || result.Error.Code != tools.ErrorInvalidArgument {
		t.Fatalf("expected invalid argument error, got %+v", result.Error)
	}
}

func TestRuntimeRegistersPlanToolForProvider(t *testing.T) {
	registry := tools.NewToolRegistry()
	runtime := NewRuntime(RuntimeConfig{Provider: &fakeProvider{}, Registry: registry})

	found := false
	for _, definition := range runtime.providerTools() {
		if definition.Name == PlanToolName {
			found = true
			if !strings.Contains(definition.Description, "complex tasks with 3+ steps") {
				t.Fatalf("plan tool description does not guide complex task usage: %q", definition.Description)
			}
			if !strings.Contains(definition.Description, "Mark completed items completed immediately") {
				t.Fatalf("plan tool description does not guide milestone updates: %q", definition.Description)
			}
		}
	}
	if !found {
		t.Fatal("runtime did not expose plan tool to provider")
	}
}

func TestRuntimeSystemPromptGuidesPlanTracking(t *testing.T) {
	prompt := runtimeSystemPrompt(time.Time{})
	if !strings.Contains(prompt, "Track multi-step work with the plan tool") {
		t.Fatalf("runtime prompt missing plan guidance")
	}
	if !strings.Contains(prompt, "Do not use the plan as the final answer") {
		t.Fatalf("runtime prompt missing final-answer plan boundary")
	}
	if !strings.Contains(prompt, "include the concrete results from tool outputs") {
		t.Fatalf("runtime prompt missing concrete tool-result synthesis guidance")
	}
}
