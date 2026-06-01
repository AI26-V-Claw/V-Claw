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
	if len(secondMessages) != 3 {
		t.Fatalf("expected user, assistant tool call, tool result; got %#v", secondMessages)
	}
	if secondMessages[2].Role != providers.MessageRoleTool || secondMessages[2].ToolCallID != "call_time" {
		t.Fatalf("unexpected tool observation message: %#v", secondMessages[2])
	}
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
