package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
)

type fakeProvider struct {
	responses []providers.ChatResponse
	err       error
	calls     []providers.ChatRequest
}

type blockingRuntimeTool struct {
	release chan struct{}
}

func (blockingRuntimeTool) Name() string                 { return "test.blocking" }
func (blockingRuntimeTool) Description() string          { return "Blocks until released." }
func (blockingRuntimeTool) Parameters() tools.ToolSchema { return tools.ToolSchema{"type": "object"} }
func (blockingRuntimeTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (blockingRuntimeTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSafeCompute }
func (t blockingRuntimeTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	<-t.release
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "released",
		ContentForUser: "released",
	}
}

func (p *fakeProvider) Chat(_ context.Context, request providers.ChatRequest) (providers.ChatResponse, error) {
	p.calls = append(p.calls, request)
	if p.err != nil {
		return providers.ChatResponse{}, p.err
	}
	if len(p.responses) == 0 {
		return providers.ChatResponse{Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "fallback"}}, nil
	}
	response := p.responses[0]
	p.responses = p.responses[1:]
	return response, nil
}

func (p *fakeProvider) Generate(ctx context.Context, req *providers.GenerateRequest) (*providers.GenerateResponse, error) {
	resp, err := p.Chat(ctx, providers.ChatRequest{
		Model: req.Model,
		Messages: []providers.Message{
			{Role: providers.MessageRoleSystem, Content: req.SystemPrompt},
			{Role: providers.MessageRoleUser, Content: req.UserPrompt},
		},
	})
	if err != nil {
		return nil, err
	}
	return &providers.GenerateResponse{Text: resp.Message.Content, Model: req.Model}, nil
}

func (p *fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) Close() error { return nil }

func runtimeTestMessage() contracts.UserMessage {
	return contracts.UserMessage{
		RequestID: "req_001",
		SessionID: "sess_001",
		Channel:   "dev",
		Text:      "hello",
		Timestamp: time.Date(2026, 5, 29, 9, 0, 0, 0, time.FixedZone("ICT", 7*60*60)),
	}
}

func TestRuntimeCompletesNormalChat(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "Xin chào!"},
	}}}
	registry := tools.NewToolRegistry()
	runtime := NewRuntime(RuntimeConfig{
		Provider: provider,
		Registry: registry,
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed, got %#v", response)
	}
	if response.Message != "Xin chào!" {
		t.Fatalf("unexpected message: %q", response.Message)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected 1 provider call, got %d", len(provider.calls))
	}
}

func TestRuntimeExecutesReadOnlyToolAndContinuesToFinalAnswer(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{
		{Message: providers.Message{
			Role:    providers.MessageRoleAssistant,
			Content: "checking time",
			ToolCalls: []providers.ToolCall{{
				ID:   "call_time",
				Name: "get_current_time",
			}},
		}},
		{Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "done"}},
	}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(tools.NewCurrentTimeToolWithClock(fixedTestTime)); err != nil {
		t.Fatalf("register current time: %v", err)
	}
	store := sessions.NewInMemoryStore()
	runtime := NewRuntime(RuntimeConfig{
		Provider:     provider,
		Registry:     registry,
		SessionStore: store,
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed, got %#v", response)
	}
	if len(response.ToolResults) != 1 || !response.ToolResults[0].Success {
		t.Fatalf("expected successful tool result, got %#v", response.ToolResults)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(provider.calls))
	}
	secondMessages := provider.calls[1].Messages
	if len(secondMessages) != 4 {
		t.Fatalf("expected system, user, assistant tool call, tool result; got %#v", secondMessages)
	}
	if secondMessages[0].Role != providers.MessageRoleSystem {
		t.Fatalf("expected system prompt first, got %#v", secondMessages[0])
	}
	if secondMessages[3].Role != providers.MessageRoleTool || secondMessages[3].ToolCallID != "call_time" {
		t.Fatalf("unexpected tool observation message: %#v", secondMessages[3])
	}
}

func TestRuntimeEmitsProgressForToolExecution(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{
		{Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{
				ID:   "call_time",
				Name: "get_current_time",
			}},
		}},
		{Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "done"}},
	}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(tools.NewCurrentTimeToolWithClock(fixedTestTime)); err != nil {
		t.Fatalf("register current time: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{Provider: provider, Registry: registry})

	events := []ProgressEvent{}
	ctx := WithProgressSink(context.Background(), func(_ context.Context, event ProgressEvent) {
		events = append(events, event)
	})
	response, err := runtime.Run(ctx, runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed, got %#v", response)
	}

	if !hasProgressEvent(events, ProgressStageToolStarted, "get_current_time") {
		t.Fatalf("missing tool started progress event: %#v", events)
	}
	if !hasProgressEvent(events, ProgressStageToolCompleted, "get_current_time") {
		t.Fatalf("missing tool completed progress event: %#v", events)
	}
}

func hasProgressEvent(events []ProgressEvent, stage ProgressStage, toolName string) bool {
	for _, event := range events {
		if event.Stage == stage && event.ToolName == toolName {
			return true
		}
	}
	return false
}

func TestRuntimeReturnsApprovalRequiredForSideEffectTool(t *testing.T) {
	executions := 0
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{
				ID:        "call_write",
				Name:      "danger.count",
				Arguments: map[string]any{"value": "x"},
			}},
		},
	}}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(countingDangerousTool{executions: &executions}); err != nil {
		t.Fatalf("register dangerous tool: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider: provider,
		Registry: registry,
		Now:      func() time.Time { return runtimeTestMessage().Timestamp },
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusApprovalRequired {
		t.Fatalf("expected approval_required, got %#v", response)
	}
	if response.ApprovalRequest == nil {
		t.Fatalf("expected approval request")
	}
	if response.ApprovalRequest.ToolCall.ToolName != "danger.count" {
		t.Fatalf("unexpected approval tool call: %#v", response.ApprovalRequest.ToolCall)
	}
	if executions != 0 {
		t.Fatalf("side-effect tool must not execute before approval, executions=%d", executions)
	}
}

func TestRuntimeStoresToolObservationForApprovalRequiredTool(t *testing.T) {
	executions := 0
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{
				ID:        "call_write",
				Name:      "danger.count",
				Arguments: map[string]any{"value": "x"},
			}},
		},
	}}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(countingDangerousTool{executions: &executions}); err != nil {
		t.Fatalf("register dangerous tool: %v", err)
	}
	store := sessions.NewInMemoryStore()
	runtime := NewRuntime(RuntimeConfig{
		Provider:     provider,
		Registry:     registry,
		SessionStore: store,
		Now:          func() time.Time { return runtimeTestMessage().Timestamp },
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusApprovalRequired {
		t.Fatalf("expected approval_required, got %#v", response)
	}

	transcript, err := store.LoadTranscript(context.Background(), runtimeTestMessage().SessionID)
	if err != nil {
		t.Fatalf("load transcript: %v", err)
	}
	if len(transcript) != 3 {
		t.Fatalf("expected user, assistant tool call, policy tool observation; got %#v", transcript)
	}
	if transcript[2].Role != providers.MessageRoleTool || transcript[2].ToolCallID != "call_write" {
		t.Fatalf("expected tool observation for approval-required call, got %#v", transcript[2])
	}
}

func TestRuntimeStoresSkippedObservationsForRemainingToolCalls(t *testing.T) {
	executions := 0
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{
				{ID: "call_write", Name: "danger.count"},
				{ID: "call_time", Name: "get_current_time"},
			},
		},
	}}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(countingDangerousTool{executions: &executions}); err != nil {
		t.Fatalf("register dangerous tool: %v", err)
	}
	if err := registry.Register(tools.NewCurrentTimeToolWithClock(fixedTestTime)); err != nil {
		t.Fatalf("register time tool: %v", err)
	}
	store := sessions.NewInMemoryStore()
	runtime := NewRuntime(RuntimeConfig{
		Provider:     provider,
		Registry:     registry,
		SessionStore: store,
		Now:          func() time.Time { return runtimeTestMessage().Timestamp },
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusApprovalRequired {
		t.Fatalf("expected approval_required, got %#v", response)
	}

	transcript, err := store.LoadTranscript(context.Background(), runtimeTestMessage().SessionID)
	if err != nil {
		t.Fatalf("load transcript: %v", err)
	}
	if len(transcript) != 4 {
		t.Fatalf("expected user, assistant, and two tool observations; got %#v", transcript)
	}
	if transcript[2].ToolCallID != "call_write" || transcript[3].ToolCallID != "call_time" {
		t.Fatalf("missing tool observations for all tool calls: %#v", transcript)
	}
}

func TestRuntimeProviderErrorReturnsFailedErrorShape(t *testing.T) {
	provider := &fakeProvider{err: fmt.Errorf("network down")}
	runtime := NewRuntime(RuntimeConfig{
		Provider: provider,
		Registry: tools.NewToolRegistry(),
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusFailed {
		t.Fatalf("expected failed, got %#v", response)
	}
	if response.Error == nil || response.Error.Code != contracts.ErrorProviderError {
		t.Fatalf("expected provider error shape, got %#v", response.Error)
	}
}

func TestRuntimeRetryableProviderErrorReturnsUnavailableShape(t *testing.T) {
	provider := &fakeProvider{err: providers.NewRetryableError(fmt.Errorf("connection reset"))}
	runtime := NewRuntime(RuntimeConfig{
		Provider: provider,
		Registry: tools.NewToolRegistry(),
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("unexpected runtime error: %v", err)
	}
	if response.Status != contracts.AgentStatusFailed {
		t.Fatalf("expected failed, got %#v", response)
	}
	if response.Error == nil || response.Error.Code != contracts.ErrorProviderUnavailable || !response.Error.Retryable {
		t.Fatalf("expected retryable provider unavailable error shape, got %#v", response.Error)
	}
}

func TestRuntimeToolErrorReturnsFailedErrorShape(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{
				ID:        "call_invalid",
				Name:      "calculator",
				Arguments: map[string]any{"operation": "divide", "a": 10, "b": 0},
			}},
		},
	}}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(tools.NewCalculatorTool()); err != nil {
		t.Fatalf("register calculator: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider: provider,
		Registry: registry,
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusFailed {
		t.Fatalf("expected failed, got %#v", response)
	}
	if response.Error == nil || response.Error.Code != contracts.ErrorToolInputInvalid {
		t.Fatalf("expected tool input error, got %#v", response.Error)
	}
}

func TestRuntimeToolTimeoutReturnsFailedErrorShape(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role:      providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{ID: "call_block", Name: "test.blocking"}},
		},
	}}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(blockingRuntimeTool{release: make(chan struct{})}); err != nil {
		t.Fatalf("register blocking tool: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:    provider,
		Registry:    registry,
		ToolTimeout: time.Millisecond,
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusFailed {
		t.Fatalf("expected failed, got %#v", response)
	}
	if response.Error == nil || response.Error.Code != contracts.ErrorProviderTimeout {
		t.Fatalf("expected timeout error, got %#v", response.Error)
	}
}

func TestRuntimeStopsAtMaxIterations(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{
		{Message: providers.Message{Role: providers.MessageRoleAssistant, ToolCalls: []providers.ToolCall{{ID: "call_1", Name: "calculator", Arguments: map[string]any{"operation": "add", "a": 1, "b": 1}}}}},
		{Message: providers.Message{Role: providers.MessageRoleAssistant, ToolCalls: []providers.ToolCall{{ID: "call_2", Name: "calculator", Arguments: map[string]any{"operation": "add", "a": 2, "b": 2}}}}},
	}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(tools.NewCalculatorTool()); err != nil {
		t.Fatalf("register calculator: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:      provider,
		Registry:      registry,
		MaxIterations: 2,
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusFailed {
		t.Fatalf("expected failed, got %#v", response)
	}
	if response.Error == nil || response.Error.Code != contracts.ErrorMaxIterationsExceeded {
		t.Fatalf("expected max iteration error, got %#v", response.Error)
	}
}
