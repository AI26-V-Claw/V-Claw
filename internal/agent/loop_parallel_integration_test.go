package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"vclaw/internal/tools"
)

func TestAgentLoopParallelFlagDisabledUsesSequential(t *testing.T) {
	llm := &mockLLM{responses: []LLMResponse{
		{
			Content: "Chạy hai tool.",
			ToolCalls: []tools.ToolCall{
				{ID: "call_a", Name: "safe.a"},
				{ID: "call_b", Name: "safe.b"},
			},
		},
		{Content: "Done."},
	}}

	registry := tools.NewToolRegistry()
	if err := registry.Register(testSafeBlockingTool{name: "safe.a", result: "safe a result"}); err != nil {
		t.Fatalf("register safe.a: %v", err)
	}
	if err := registry.Register(testSafeBlockingTool{name: "safe.b", result: "safe b result"}); err != nil {
		t.Fatalf("register safe.b: %v", err)
	}

	loop := NewAgentLoop(llm, registry)
	loop.parallelExecutionEnabled = false

	result := loop.Run(context.Background(), RunRequest{
		UserMessage:   "Chạy 2 tool an toàn",
		SessionID:     "sess_disabled",
		MaxIterations: 3,
	})

	if result.Status != RunStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if result.ToolCallsCount != 2 {
		t.Fatalf("expected 2 tool calls, got %d", result.ToolCallsCount)
	}
	if result.Messages[2].ToolCallID != "call_a" || result.Messages[2].Content != "safe a result" {
		t.Fatalf("unexpected first tool result: %#v", result.Messages[2])
	}
	if result.Messages[3].ToolCallID != "call_b" || result.Messages[3].Content != "safe b result" {
		t.Fatalf("unexpected second tool result: %#v", result.Messages[3])
	}
}

func TestAgentLoopParallelFlagEnabledRunsParallel(t *testing.T) {
	llm := &mockLLM{responses: []LLMResponse{
		{
			Content: "Chạy song song.",
			ToolCalls: []tools.ToolCall{
				{ID: "call_a", Name: "parallel.a"},
				{ID: "call_b", Name: "parallel.b"},
			},
		},
		{Content: "Done."},
	}}

	started := make(chan string, 2)
	release := make(chan struct{})
	once := &sync.Once{}

	registry := tools.NewToolRegistry()
	if err := registry.Register(blockingSafeToolWithDelay{name: "parallel.a", started: started, release: release, startOnce: once, delay: 20 * time.Millisecond}); err != nil {
		t.Fatalf("register parallel.a: %v", err)
	}
	if err := registry.Register(blockingSafeToolWithDelay{name: "parallel.b", started: started, release: release, startOnce: once, delay: 20 * time.Millisecond}); err != nil {
		t.Fatalf("register parallel.b: %v", err)
	}

	loop := NewAgentLoop(llm, registry)
	loop.parallelExecutionEnabled = true
	loop.parallelMaxWorkers = 2
	loop.parallelToolTimeoutDefault = 2 * time.Second

	done := make(chan RunResult, 1)
	go func() {
		done <- loop.Run(context.Background(), RunRequest{
			UserMessage:   "Chạy song song",
			SessionID:     "sess_enabled",
			MaxIterations: 3,
		})
	}()

	first := <-started
	second := <-started
	if first == second {
		t.Fatalf("expected two different tools to start, got %q twice", first)
	}
	result := <-done
	if result.Status != RunStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if result.ToolCallsCount != 2 {
		t.Fatalf("expected 2 tool calls, got %d", result.ToolCallsCount)
	}
	if result.Messages[2].ToolCallID != "call_a" || result.Messages[2].Content != "result from parallel.a" {
		t.Fatalf("unexpected first tool result: %#v", result.Messages[2])
	}
	if result.Messages[3].ToolCallID != "call_b" || result.Messages[3].Content != "result from parallel.b" {
		t.Fatalf("unexpected second tool result: %#v", result.Messages[3])
	}
}

func TestAgentLoopParallelMixedBatchFallsBackToSequential(t *testing.T) {
	llm := &mockLLM{responses: []LLMResponse{
		{
			Content: "Chạy batch mixed.",
			ToolCalls: []tools.ToolCall{
				{ID: "call_safe", Name: "safe.a"},
				{ID: "call_sensitive", Name: "danger.count"},
			},
		},
		{Content: "Done."},
	}}

	executions := 0
	registry := tools.NewToolRegistry()
	if err := registry.Register(testSafeBlockingTool{name: "safe.a", result: "safe a result"}); err != nil {
		t.Fatalf("register safe.a: %v", err)
	}
	if err := registry.Register(countingDangerousTool{executions: &executions}); err != nil {
		t.Fatalf("register danger.count: %v", err)
	}

	loop := NewAgentLoop(llm, registry)
	loop.parallelExecutionEnabled = true
	loop.parallelMaxWorkers = 2
	loop.parallelToolTimeoutDefault = 2 * time.Second

	result := loop.Run(context.Background(), RunRequest{
		UserMessage:   "Chạy mixed tools",
		SessionID:     "sess_mixed",
		MaxIterations: 3,
	})

	if result.Status != RunStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if result.ToolCallsCount != 2 {
		t.Fatalf("expected 2 tool calls, got %d", result.ToolCallsCount)
	}
	if executions != 0 {
		t.Fatalf("expected sensitive tool to be blocked before execution, got %d executions", executions)
	}
	if result.Messages[2].ToolCallID != "call_safe" || result.Messages[2].Content != "safe a result" {
		t.Fatalf("unexpected safe tool result: %#v", result.Messages[2])
	}
	if result.Messages[3].ToolCallID != "call_sensitive" {
		t.Fatalf("unexpected sensitive tool slot: %#v", result.Messages[3])
	}
	if result.Messages[3].Content != "Permission denied for tool: danger.count" {
		t.Fatalf("expected sensitive tool denial result, got %#v", result.Messages[3])
	}
}

type testSafeBlockingTool struct {
	name   string
	result string
}

func (t testSafeBlockingTool) Name() string        { return t.name }
func (t testSafeBlockingTool) Description() string { return "safe blocking tool" }
func (t testSafeBlockingTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{"type": "object"}
}
func (t testSafeBlockingTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (t testSafeBlockingTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSafeRead }
func (t testSafeBlockingTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	if t.result == "" {
		t.result = t.name + " result"
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  t.result,
		ContentForUser: t.result,
	}
}
