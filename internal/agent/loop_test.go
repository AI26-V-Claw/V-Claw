package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"vclaw/internal/tools"
)

type mockLLM struct {
	responses []LLMResponse
	calls     []mockLLMCall
}

type mockLLMCall struct {
	messages []Message
	tools    []tools.ToolDefinition
}

type dangerousTool struct{}

func (dangerousTool) Name() string                 { return "sandbox.runShell" }
func (dangerousTool) Description() string          { return "Runs a shell command." }
func (dangerousTool) Parameters() tools.ToolSchema { return tools.ToolSchema{"type": "object"} }
func (dangerousTool) Capability() tools.Capability { return tools.CapabilityMutating }
func (dangerousTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelCodeExecution }
func (dangerousTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "dangerous tool executed",
		ContentForUser: "dangerous tool executed",
	}
}

type blockingSafeTool struct {
	name    string
	started chan string
	release chan struct{}
	once    *sync.Once
}

func (t blockingSafeTool) Name() string               { return t.name }
func (blockingSafeTool) Description() string          { return "Blocks until released." }
func (blockingSafeTool) Parameters() tools.ToolSchema { return tools.ToolSchema{"type": "object"} }
func (blockingSafeTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (blockingSafeTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSafeCompute }
func (t blockingSafeTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	t.started <- t.name
	t.once.Do(func() { close(t.release) })
	<-t.release
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "result from " + t.name,
		ContentForUser: "result from " + t.name,
	}
}

type panicTool struct{}

func (panicTool) Name() string                 { return "test.panic" }
func (panicTool) Description() string          { return "Panics during execution." }
func (panicTool) Parameters() tools.ToolSchema { return tools.ToolSchema{"type": "object"} }
func (panicTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (panicTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSafeCompute }
func (panicTool) Execute(_ context.Context, _ tools.ToolCall) tools.ToolResult {
	panic("boom")
}

type longOutputTool struct{}

func (longOutputTool) Name() string                 { return "test.long_output" }
func (longOutputTool) Description() string          { return "Returns long content." }
func (longOutputTool) Parameters() tools.ToolSchema { return tools.ToolSchema{"type": "object"} }
func (longOutputTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (longOutputTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSafeRead }
func (longOutputTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	content := strings.Repeat("x", maxToolContentForLLM+50)
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
	}
}

type countingDangerousTool struct {
	executions *int
}

func (countingDangerousTool) Name() string                 { return "danger.count" }
func (countingDangerousTool) Description() string          { return "Dangerous counting tool." }
func (countingDangerousTool) Parameters() tools.ToolSchema { return tools.ToolSchema{"type": "object"} }
func (countingDangerousTool) Capability() tools.Capability { return tools.CapabilityMutating }
func (countingDangerousTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelExternalWrite }
func (t countingDangerousTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	(*t.executions)++
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "danger executed",
		ContentForUser: "danger executed",
	}
}

func fixedTestTime() time.Time {
	return time.Date(2026, 5, 29, 9, 0, 0, 0, time.FixedZone("ICT", 7*60*60))
}

func (m *mockLLM) Chat(_ context.Context, messages []Message, availableTools []tools.ToolDefinition) (LLMResponse, error) {
	m.calls = append(m.calls, mockLLMCall{
		messages: cloneMessages(messages),
		tools:    append([]tools.ToolDefinition(nil), availableTools...),
	})

	if len(m.responses) == 0 {
		return LLMResponse{Content: "fallback final"}, nil
	}

	response := m.responses[0]
	m.responses = m.responses[1:]
	return response, nil
}

func TestAgentLoopReturnsFinalAnswerWithoutTool(t *testing.T) {
	llm := &mockLLM{responses: []LLMResponse{{Content: "Xin chào!"}}}
	registry := tools.NewToolRegistry()
	loop := NewAgentLoop(llm, registry)

	result := loop.Run(context.Background(), RunRequest{
		UserMessage:   "Chào bạn",
		SessionID:     "sess_001",
		MaxIterations: 3,
	})

	if result.Status != RunStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if result.FinalContent != "Xin chào!" {
		t.Fatalf("expected final answer, got %q", result.FinalContent)
	}
	if result.Error != "" {
		t.Fatalf("expected empty error on completed result, got %q", result.Error)
	}
	if result.Iterations != 1 {
		t.Fatalf("expected 1 iteration, got %d", result.Iterations)
	}
	if result.ToolCallsCount != 0 {
		t.Fatalf("expected no tool calls, got %d", result.ToolCallsCount)
	}
	if len(result.Messages) != 2 || result.Messages[1].Role != MessageRoleAssistant {
		t.Fatalf("expected result messages to include final assistant answer, got %#v", result.Messages)
	}
	if len(llm.calls) != 1 {
		t.Fatalf("expected 1 llm call, got %d", len(llm.calls))
	}
	if len(llm.calls[0].messages) != 1 || llm.calls[0].messages[0].Role != MessageRoleUser {
		t.Fatalf("expected first llm call to contain user message, got %#v", llm.calls[0].messages)
	}
}

func TestAgentLoopExecutesToolAndContinuesToFinalAnswer(t *testing.T) {
	llm := &mockLLM{responses: []LLMResponse{
		{
			Content: "Tôi sẽ kiểm tra thời gian.",
			ToolCalls: []tools.ToolCall{
				{ID: "call_time", Name: "get_current_time"},
			},
		},
		{Content: "Bây giờ là 2026-05-29T09:00:00+07:00."},
	}}

	registry := tools.NewToolRegistry()
	if err := registry.Register(tools.NewCurrentTimeToolWithClock(fixedTestTime)); err != nil {
		t.Fatalf("register current time tool: %v", err)
	}

	loop := NewAgentLoop(llm, registry)
	result := loop.Run(context.Background(), RunRequest{
		UserMessage:   "Bây giờ là mấy giờ?",
		SessionID:     "sess_001",
		MaxIterations: 3,
	})

	if result.Status != RunStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if result.FinalContent != "Bây giờ là 2026-05-29T09:00:00+07:00." {
		t.Fatalf("unexpected final answer: %q", result.FinalContent)
	}
	if result.Iterations != 2 {
		t.Fatalf("expected 2 iterations, got %d", result.Iterations)
	}
	if result.ToolCallsCount != 1 {
		t.Fatalf("expected 1 tool call, got %d", result.ToolCallsCount)
	}
	if len(llm.calls) != 2 {
		t.Fatalf("expected 2 llm calls, got %d", len(llm.calls))
	}
	if len(llm.calls[0].tools) != 1 || llm.calls[0].tools[0].Name != "get_current_time" {
		t.Fatalf("expected llm to receive available tool schema, got %#v", llm.calls[0].tools)
	}

	secondCallMessages := llm.calls[1].messages
	if len(secondCallMessages) != 3 {
		t.Fatalf("expected user, assistant tool-call, and tool result messages, got %#v", secondCallMessages)
	}
	if secondCallMessages[1].Role != MessageRoleAssistant || len(secondCallMessages[1].ToolCalls) != 1 {
		t.Fatalf("expected assistant tool call message, got %#v", secondCallMessages[1])
	}
	if secondCallMessages[2].Role != MessageRoleTool {
		t.Fatalf("expected tool result message, got %#v", secondCallMessages[2])
	}
	if secondCallMessages[2].ToolCallID != "call_time" {
		t.Fatalf("expected tool call id call_time, got %q", secondCallMessages[2].ToolCallID)
	}
	if secondCallMessages[2].Content != "Current time: 2026-05-29T09:00:00+07:00" {
		t.Fatalf("unexpected tool result content: %q", secondCallMessages[2].Content)
	}
}

func TestAgentLoopStopsAtMaxIterations(t *testing.T) {
	llm := &mockLLM{responses: []LLMResponse{
		{ToolCalls: []tools.ToolCall{{ID: "call_1", Name: "calculator", Arguments: map[string]any{"operation": "add", "a": 1, "b": 1}}}},
		{ToolCalls: []tools.ToolCall{{ID: "call_2", Name: "calculator", Arguments: map[string]any{"operation": "add", "a": 2, "b": 2}}}},
	}}

	registry := tools.NewToolRegistry()
	if err := registry.Register(tools.NewCalculatorTool()); err != nil {
		t.Fatalf("register calculator tool: %v", err)
	}

	loop := NewAgentLoop(llm, registry)
	result := loop.Run(context.Background(), RunRequest{
		UserMessage:   "Cứ tính tiếp",
		SessionID:     "sess_001",
		MaxIterations: 2,
	})

	if result.Status != RunStatusMaxIterationsReached {
		t.Fatalf("expected max iterations reached, got %s", result.Status)
	}
	if result.Error != tools.ErrorMaxIterationsReached {
		t.Fatalf("expected max iteration error code, got %q", result.Error)
	}
	if result.Iterations != 2 {
		t.Fatalf("expected 2 iterations, got %d", result.Iterations)
	}
	if result.ToolCallsCount != 2 {
		t.Fatalf("expected 2 tool calls, got %d", result.ToolCallsCount)
	}
	if len(llm.calls) != 2 {
		t.Fatalf("expected 2 llm calls, got %d", len(llm.calls))
	}
}

func TestAgentLoopExecutesMultipleSafeToolsInParallelAndAppendsStableOrder(t *testing.T) {
	llm := &mockLLM{responses: []LLMResponse{
		{ToolCalls: []tools.ToolCall{
			{ID: "call_slow", Name: "test.slow"},
			{ID: "call_fast", Name: "test.fast"},
		}},
		{Content: "Đã chạy hai tool."},
	}}
	registry := tools.NewToolRegistry()
	started := make(chan string, 2)
	release := make(chan struct{})
	once := &sync.Once{}
	if err := registry.Register(blockingSafeTool{name: "test.slow", started: started, release: release, once: once}); err != nil {
		t.Fatalf("register slow tool: %v", err)
	}
	if err := registry.Register(blockingSafeTool{name: "test.fast", started: started, release: release, once: once}); err != nil {
		t.Fatalf("register fast tool: %v", err)
	}

	loop := NewAgentLoop(llm, registry)
	result := loop.Run(context.Background(), RunRequest{
		UserMessage:   "Chạy hai tool",
		SessionID:     "sess_001",
		MaxIterations: 3,
	})

	if result.Status != RunStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if result.ToolCallsCount != 2 {
		t.Fatalf("expected 2 tool calls, got %d", result.ToolCallsCount)
	}

	startedNames := map[string]bool{<-started: true, <-started: true}
	if !startedNames["test.slow"] || !startedNames["test.fast"] {
		t.Fatalf("expected both safe tools to start, got %#v", startedNames)
	}

	secondCallMessages := llm.calls[1].messages
	if len(secondCallMessages) != 4 {
		t.Fatalf("expected 4 messages in second llm call, got %#v", secondCallMessages)
	}
	if secondCallMessages[2].ToolCallID != "call_slow" || secondCallMessages[2].Content != "result from test.slow" {
		t.Fatalf("expected first tool result to match original call order, got %#v", secondCallMessages[2])
	}
	if secondCallMessages[3].ToolCallID != "call_fast" || secondCallMessages[3].Content != "result from test.fast" {
		t.Fatalf("expected second tool result to match original call order, got %#v", secondCallMessages[3])
	}
}

func TestAgentLoopDoesNotRunDangerousToolInParallel(t *testing.T) {
	executions := 0
	llm := &mockLLM{responses: []LLMResponse{
		{ToolCalls: []tools.ToolCall{
			{ID: "call_safe", Name: "calculator", Arguments: map[string]any{"operation": "add", "a": 1, "b": 2}},
			{ID: "call_danger", Name: "danger.count"},
		}},
		{Content: "Done."},
	}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(tools.NewCalculatorTool()); err != nil {
		t.Fatalf("register calculator: %v", err)
	}
	if err := registry.Register(countingDangerousTool{executions: &executions}); err != nil {
		t.Fatalf("register dangerous counting tool: %v", err)
	}

	loop := NewAgentLoop(llm, registry)
	result := loop.Run(context.Background(), RunRequest{
		UserMessage:   "Chạy mixed tools",
		SessionID:     "sess_001",
		MaxIterations: 3,
	})

	if result.Status != RunStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if executions != 0 {
		t.Fatalf("dangerous tool should be blocked, executed %d times", executions)
	}
	secondCallMessages := llm.calls[1].messages
	if len(secondCallMessages) != 4 {
		t.Fatalf("expected 4 messages, got %#v", secondCallMessages)
	}
	if secondCallMessages[2].ToolCallID != "call_safe" {
		t.Fatalf("expected safe result first, got %#v", secondCallMessages[2])
	}
	if secondCallMessages[3].ToolCallID != "call_danger" {
		t.Fatalf("expected dangerous denied result second, got %#v", secondCallMessages[3])
	}
	if secondCallMessages[3].Content != "Permission denied for tool: danger.count" {
		t.Fatalf("expected permission denied for dangerous tool, got %q", secondCallMessages[3].Content)
	}
}

func TestAgentLoopHidesDangerousToolFromLLM(t *testing.T) {
	llm := &mockLLM{responses: []LLMResponse{{Content: "Không cần tool."}}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(tools.NewCalculatorTool()); err != nil {
		t.Fatalf("register calculator tool: %v", err)
	}
	if err := registry.Register(dangerousTool{}); err != nil {
		t.Fatalf("register dangerous tool: %v", err)
	}

	loop := NewAgentLoop(llm, registry)
	result := loop.Run(context.Background(), RunRequest{
		UserMessage:   "Có tool nào?",
		SessionID:     "sess_001",
		MaxIterations: 1,
	})

	if result.Status != RunStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if len(llm.calls) != 1 {
		t.Fatalf("expected 1 llm call, got %d", len(llm.calls))
	}
	if len(llm.calls[0].tools) != 1 {
		t.Fatalf("expected only safe tool exposed, got %#v", llm.calls[0].tools)
	}
	if llm.calls[0].tools[0].Name != "calculator" {
		t.Fatalf("expected calculator to be exposed, got %#v", llm.calls[0].tools)
	}
}

func TestAgentLoopBlocksDeniedToolBeforeExecution(t *testing.T) {
	llm := &mockLLM{responses: []LLMResponse{
		{ToolCalls: []tools.ToolCall{{ID: "call_danger", Name: "sandbox.runShell"}}},
		{Content: "Tool bị chặn bởi policy."},
	}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(dangerousTool{}); err != nil {
		t.Fatalf("register dangerous tool: %v", err)
	}

	loop := NewAgentLoop(llm, registry)
	result := loop.Run(context.Background(), RunRequest{
		UserMessage:   "Chạy shell",
		SessionID:     "sess_001",
		MaxIterations: 3,
	})

	if result.Status != RunStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if len(llm.calls) != 2 {
		t.Fatalf("expected 2 llm calls, got %d", len(llm.calls))
	}
	secondCallMessages := llm.calls[1].messages
	if len(secondCallMessages) != 3 {
		t.Fatalf("expected 3 messages in second call, got %#v", secondCallMessages)
	}
	if secondCallMessages[2].Content != "Permission denied for tool: sandbox.runShell" {
		t.Fatalf("expected permission denied result, got %q", secondCallMessages[2].Content)
	}
	if result.Messages[2].Content == "dangerous tool executed" {
		t.Fatal("dangerous tool should not execute")
	}
}

func TestAgentLoopAppendsMissingToolResult(t *testing.T) {
	llm := &mockLLM{responses: []LLMResponse{
		{ToolCalls: []tools.ToolCall{{ID: "call_missing", Name: "missing.tool"}}},
		{Content: "Tool không tồn tại."},
	}}

	loop := NewAgentLoop(llm, tools.NewToolRegistry())
	result := loop.Run(context.Background(), RunRequest{
		UserMessage:   "Dùng tool không có",
		SessionID:     "sess_001",
		MaxIterations: 3,
	})

	if result.Status != RunStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if len(llm.calls) != 2 {
		t.Fatalf("expected 2 llm calls, got %d", len(llm.calls))
	}

	secondCallMessages := llm.calls[1].messages
	if len(secondCallMessages) != 3 {
		t.Fatalf("expected 3 messages in second call, got %#v", secondCallMessages)
	}
	if secondCallMessages[2].Role != MessageRoleTool {
		t.Fatalf("expected tool message, got %#v", secondCallMessages[2])
	}
	if secondCallMessages[2].ToolCallID != "call_missing" {
		t.Fatalf("expected call_missing, got %q", secondCallMessages[2].ToolCallID)
	}
	if secondCallMessages[2].Content != "Tool not found: missing.tool" {
		t.Fatalf("unexpected missing tool content: %q", secondCallMessages[2].Content)
	}
}

func TestAgentLoopAppendsInvalidToolArgumentsResult(t *testing.T) {
	llm := &mockLLM{responses: []LLMResponse{
		{ToolCalls: []tools.ToolCall{{ID: "call_invalid", Name: "calculator", Arguments: map[string]any{"operation": "divide", "a": 10, "b": 0}}}},
		{Content: "Không thể chia cho 0."},
	}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(tools.NewCalculatorTool()); err != nil {
		t.Fatalf("register calculator: %v", err)
	}

	loop := NewAgentLoop(llm, registry)
	result := loop.Run(context.Background(), RunRequest{
		UserMessage:   "Tính 10 chia 0",
		SessionID:     "sess_001",
		MaxIterations: 3,
	})

	if result.Status != RunStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if result.FinalContent != "Không thể chia cho 0." {
		t.Fatalf("unexpected final content: %q", result.FinalContent)
	}
	if result.ToolCallsCount != 1 {
		t.Fatalf("expected 1 tool call, got %d", result.ToolCallsCount)
	}

	secondCallMessages := llm.calls[1].messages
	if len(secondCallMessages) != 3 {
		t.Fatalf("expected 3 messages in second call, got %#v", secondCallMessages)
	}
	if secondCallMessages[2].ToolCallID != "call_invalid" {
		t.Fatalf("expected call_invalid, got %q", secondCallMessages[2].ToolCallID)
	}
	if secondCallMessages[2].Content != "Invalid tool arguments: b must not be zero for divide" {
		t.Fatalf("unexpected invalid arguments content: %q", secondCallMessages[2].Content)
	}
}

func TestAgentLoopConvertsToolPanicToExecutionErrorResult(t *testing.T) {
	llm := &mockLLM{responses: []LLMResponse{
		{ToolCalls: []tools.ToolCall{{ID: "call_panic", Name: "test.panic"}}},
		{Content: "Tool lỗi khi chạy."},
	}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(panicTool{}); err != nil {
		t.Fatalf("register panic tool: %v", err)
	}

	loop := NewAgentLoop(llm, registry)
	result := loop.Run(context.Background(), RunRequest{
		UserMessage:   "Chạy tool lỗi",
		SessionID:     "sess_001",
		MaxIterations: 3,
	})

	if result.Status != RunStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if len(llm.calls) != 2 {
		t.Fatalf("expected 2 llm calls, got %d", len(llm.calls))
	}

	secondCallMessages := llm.calls[1].messages
	if len(secondCallMessages) != 3 {
		t.Fatalf("expected 3 messages in second call, got %#v", secondCallMessages)
	}
	if secondCallMessages[2].ToolCallID != "call_panic" {
		t.Fatalf("expected call_panic, got %q", secondCallMessages[2].ToolCallID)
	}
	if secondCallMessages[2].Content != "Tool execution error for test.panic: panic: boom" {
		t.Fatalf("unexpected execution error content: %q", secondCallMessages[2].Content)
	}
}

func TestAgentLoopTruncatesLongToolOutputBeforeNextLLMCall(t *testing.T) {
	llm := &mockLLM{responses: []LLMResponse{
		{ToolCalls: []tools.ToolCall{{ID: "call_long", Name: "test.long_output"}}},
		{Content: "Đã tóm tắt output dài."},
	}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(longOutputTool{}); err != nil {
		t.Fatalf("register long output tool: %v", err)
	}

	loop := NewAgentLoop(llm, registry)
	result := loop.Run(context.Background(), RunRequest{
		UserMessage:   "Chạy tool output dài",
		SessionID:     "sess_001",
		MaxIterations: 3,
	})

	if result.Status != RunStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if result.FinalContent != "Đã tóm tắt output dài." {
		t.Fatalf("unexpected final content: %q", result.FinalContent)
	}
	if result.Iterations != 2 {
		t.Fatalf("expected 2 iterations, got %d", result.Iterations)
	}
	if result.ToolCallsCount != 1 {
		t.Fatalf("expected 1 tool call, got %d", result.ToolCallsCount)
	}

	secondCallMessages := llm.calls[1].messages
	if len(secondCallMessages) != 3 {
		t.Fatalf("expected 3 messages in second call, got %#v", secondCallMessages)
	}
	toolContent := secondCallMessages[2].Content
	if !strings.Contains(toolContent, "...[truncated 50 bytes]") {
		t.Fatalf("expected truncation marker, got %q", toolContent)
	}
	if len(toolContent) <= maxToolContentForLLM || len(toolContent) >= maxToolContentForLLM+50 {
		t.Fatalf("expected bounded tool content length, got %d", len(toolContent))
	}
}
