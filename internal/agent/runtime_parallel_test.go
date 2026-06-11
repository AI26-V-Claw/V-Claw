package agent

import (
	"context"
	"testing"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

func TestRuntimeParallelBatchNormalizesRelativeDateAndSerializesProgress(t *testing.T) {
	started := make(chan string, 2)
	calls := make(chan tools.ToolCall, 2)
	release := make(chan struct{})
	calendarTool := parallelRuntimeTool{
		name: "calendar.listEvents",
		parameters: tools.ToolSchema{
			"type":       "object",
			"properties": map[string]any{},
			"required":   []string{"timeMin", "timeMax"},
		},
		started: started,
		calls:   calls,
		release: release,
	}
	gmailTool := parallelRuntimeTool{
		name:    "gmail.listEmails",
		started: started,
		calls:   calls,
		release: release,
	}
	registry := tools.NewToolRegistry()
	if err := registry.Register(calendarTool); err != nil {
		t.Fatalf("register calendar tool: %v", err)
	}
	if err := registry.Register(gmailTool); err != nil {
		t.Fatalf("register gmail tool: %v", err)
	}
	provider := &fakeProvider{responses: []providers.ChatResponse{
		{Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{
				{ID: "call_calendar", Name: calendarTool.Name(), Arguments: map[string]any{}},
				{ID: "call_gmail", Name: gmailTool.Name(), Arguments: map[string]any{}},
			},
		}},
		{Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "done"}},
	}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:                   provider,
		Registry:                   registry,
		ParallelExecutionEnabled:   true,
		ParallelMaxWorkers:         2,
		ParallelToolTimeoutDefault: time.Second,
		Now:                        func() time.Time { return runtimeTestMessage().Timestamp },
	})

	events := make([]ProgressEvent, 0, 8)
	ctx := WithProgressSink(context.Background(), func(_ context.Context, event ProgressEvent) {
		events = append(events, event)
	})
	message := runtimeTestMessage()
	message.Text = "Liệt kê lịch và email hôm nay."
	done := make(chan contracts.AgentResponse, 1)
	go func() {
		response, _ := runtime.Run(ctx, message)
		done <- response
	}()

	firstStarted := <-started
	secondStarted := <-started
	if firstStarted == secondStarted {
		t.Fatalf("expected both calls to start, got %q twice", firstStarted)
	}
	close(release)

	response := <-done
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed response, got %#v", response)
	}
	if len(response.ToolResults) != 2 {
		t.Fatalf("expected two tool results, got %#v", response.ToolResults)
	}

	normalized := map[string]tools.ToolCall{}
	normalizedCall := <-calls
	normalized[normalizedCall.Name] = normalizedCall
	normalizedCall = <-calls
	normalized[normalizedCall.Name] = normalizedCall
	calendarArgs := normalized[calendarTool.Name()].Arguments
	if calendarArgs["timeMin"] == "" || calendarArgs["timeMax"] == "" {
		t.Fatalf("expected normalized calendar range, got %#v", calendarArgs)
	}
	gmailArgs := normalized[gmailTool.Name()].Arguments
	if gmailArgs["after"] != "2026-05-29" || gmailArgs["before"] != "2026-05-30" {
		t.Fatalf("expected normalized Gmail date range, got %#v", gmailArgs)
	}
	if !hasProgressEvent(events, ProgressStageToolCompleted, calendarTool.Name()) ||
		!hasProgressEvent(events, ProgressStageToolCompleted, gmailTool.Name()) {
		t.Fatalf("expected serialized completion progress, got %#v", events)
	}
}

func TestRuntimeParallelProgressWaitsForWorkerSlot(t *testing.T) {
	release := make(chan struct{})
	first := parallelRuntimeTool{name: "safe.first", release: release}
	second := parallelRuntimeTool{name: "safe.second", release: release}
	registry := tools.NewToolRegistry()
	if err := registry.Register(first); err != nil {
		t.Fatalf("register first tool: %v", err)
	}
	if err := registry.Register(second); err != nil {
		t.Fatalf("register second tool: %v", err)
	}
	provider := &fakeProvider{responses: []providers.ChatResponse{
		{Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{
				{ID: "call_first", Name: first.Name()},
				{ID: "call_second", Name: second.Name()},
			},
		}},
		{Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "done"}},
	}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:                   provider,
		Registry:                   registry,
		ParallelExecutionEnabled:   true,
		ParallelMaxWorkers:         1,
		ParallelToolTimeoutDefault: time.Second,
	})

	startEvents := make(chan string, 2)
	ctx := WithProgressSink(context.Background(), func(_ context.Context, event ProgressEvent) {
		if event.Stage == ProgressStageToolStarted {
			startEvents <- event.ToolCallID
		}
	})
	done := make(chan contracts.AgentResponse, 1)
	go func() {
		response, _ := runtime.Run(ctx, runtimeTestMessage())
		done <- response
	}()

	<-startEvents
	select {
	case callID := <-startEvents:
		t.Fatalf("queued call %q reported started before a worker slot was available", callID)
	case <-time.After(30 * time.Millisecond):
	}

	close(release)
	<-startEvents
	response := <-done
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed response, got %#v", response)
	}
}

func TestRuntimeParallelBatchFallsBackForMissingRequiredField(t *testing.T) {
	executed := make(chan tools.ToolCall, 2)
	requiredTool := parallelRuntimeTool{
		name: "safe.required",
		parameters: tools.ToolSchema{
			"type":       "object",
			"properties": map[string]any{},
			"required":   []string{"query"},
		},
		calls: executed,
	}
	otherTool := parallelRuntimeTool{name: "safe.other", calls: executed}
	registry := tools.NewToolRegistry()
	if err := registry.Register(requiredTool); err != nil {
		t.Fatalf("register required tool: %v", err)
	}
	if err := registry.Register(otherTool); err != nil {
		t.Fatalf("register other tool: %v", err)
	}
	provider := &fakeProvider{responses: []providers.ChatResponse{{Message: providers.Message{
		Role: providers.MessageRoleAssistant,
		ToolCalls: []providers.ToolCall{
			{ID: "call_required", Name: requiredTool.Name(), Arguments: map[string]any{}},
			{ID: "call_other", Name: otherTool.Name(), Arguments: map[string]any{}},
		},
	}}}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:                 provider,
		Registry:                 registry,
		ParallelExecutionEnabled: true,
		ParallelMaxWorkers:       2,
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusNeedClarification {
		t.Fatalf("expected clarification fallback, got %#v", response)
	}
	select {
	case call := <-executed:
		t.Fatalf("expected no tool execution before clarification, got %#v", call)
	default:
	}
}

func TestRuntimeParallelBatchFallsBackForMixedSafeAndWriteCalls(t *testing.T) {
	executed := make(chan tools.ToolCall, 2)
	writeTool := parallelRuntimeTool{
		name:       "write.external",
		capability: tools.CapabilityMutating,
		risk:       tools.RiskLevelExternalWrite,
		calls:      executed,
	}
	safeTool := parallelRuntimeTool{name: "safe.read", calls: executed}
	registry := tools.NewToolRegistry()
	if err := registry.Register(writeTool); err != nil {
		t.Fatalf("register write tool: %v", err)
	}
	if err := registry.Register(safeTool); err != nil {
		t.Fatalf("register safe tool: %v", err)
	}
	provider := &fakeProvider{responses: []providers.ChatResponse{{Message: providers.Message{
		Role: providers.MessageRoleAssistant,
		ToolCalls: []providers.ToolCall{
			{ID: "call_write", Name: writeTool.Name(), Arguments: map[string]any{}},
			{ID: "call_safe", Name: safeTool.Name(), Arguments: map[string]any{}},
		},
	}}}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:                 provider,
		Registry:                 registry,
		ParallelExecutionEnabled: true,
		ParallelMaxWorkers:       2,
		Now:                      func() time.Time { return runtimeTestMessage().Timestamp },
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusApprovalRequired {
		t.Fatalf("expected approval fallback, got %#v", response)
	}
	select {
	case call := <-executed:
		t.Fatalf("expected no execution before approval, got %#v", call)
	default:
	}
}

func TestRuntimeParallelBatchUsesParallelTimeoutDefault(t *testing.T) {
	first := parallelRuntimeTool{name: "slow.first", delay: 250 * time.Millisecond}
	second := parallelRuntimeTool{name: "slow.second", delay: 250 * time.Millisecond}
	registry := tools.NewToolRegistry()
	if err := registry.Register(first); err != nil {
		t.Fatalf("register first tool: %v", err)
	}
	if err := registry.Register(second); err != nil {
		t.Fatalf("register second tool: %v", err)
	}
	provider := &fakeProvider{responses: []providers.ChatResponse{{Message: providers.Message{
		Role: providers.MessageRoleAssistant,
		ToolCalls: []providers.ToolCall{
			{ID: "call_first", Name: first.Name()},
			{ID: "call_second", Name: second.Name()},
		},
	}}}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:                   provider,
		Registry:                   registry,
		ToolTimeout:                time.Second,
		ParallelExecutionEnabled:   true,
		ParallelMaxWorkers:         2,
		ParallelToolTimeoutDefault: 20 * time.Millisecond,
	})

	startedAt := time.Now()
	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 150*time.Millisecond {
		t.Fatalf("expected parallel timeout to bound runtime, took %s", elapsed)
	}
	if response.Status != contracts.AgentStatusFailed ||
		response.Error == nil ||
		response.Error.Code != contracts.ErrorProviderTimeout {
		t.Fatalf("expected provider timeout response, got %#v", response)
	}
}

func TestRuntimeParallelBatchCancellationStopsRun(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	first := parallelRuntimeTool{name: "cancel.first", started: started, release: release}
	second := parallelRuntimeTool{name: "cancel.second", started: started, release: release}
	registry := tools.NewToolRegistry()
	if err := registry.Register(first); err != nil {
		t.Fatalf("register first tool: %v", err)
	}
	if err := registry.Register(second); err != nil {
		t.Fatalf("register second tool: %v", err)
	}
	provider := &fakeProvider{responses: []providers.ChatResponse{{Message: providers.Message{
		Role: providers.MessageRoleAssistant,
		ToolCalls: []providers.ToolCall{
			{ID: "call_first", Name: first.Name()},
			{ID: "call_second", Name: second.Name()},
		},
	}}}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:                   provider,
		Registry:                   registry,
		ParallelExecutionEnabled:   true,
		ParallelMaxWorkers:         2,
		ParallelToolTimeoutDefault: time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan contracts.AgentResponse, 1)
	go func() {
		response, _ := runtime.Run(ctx, runtimeTestMessage())
		done <- response
	}()
	<-started
	<-started
	cancel()

	select {
	case response := <-done:
		if response.Status != contracts.AgentStatusFailed {
			t.Fatalf("expected canceled run to fail, got %#v", response)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected canceled parallel run to stop promptly")
	}
	close(release)
}

func TestRuntimeParallelBatchRecoversPanicAndContinuesPartialSuccess(t *testing.T) {
	panicTool := parallelRuntimeTool{name: "panic.read", panicNow: true}
	okTool := parallelRuntimeTool{name: "ok.read"}
	registry := tools.NewToolRegistry()
	if err := registry.Register(panicTool); err != nil {
		t.Fatalf("register panic tool: %v", err)
	}
	if err := registry.Register(okTool); err != nil {
		t.Fatalf("register ok tool: %v", err)
	}
	provider := &fakeProvider{responses: []providers.ChatResponse{
		{Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{
				{ID: "call_panic", Name: panicTool.Name()},
				{ID: "call_ok", Name: okTool.Name()},
			},
		}},
		{Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "done"}},
	}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:                   provider,
		Registry:                   registry,
		ParallelExecutionEnabled:   true,
		ParallelMaxWorkers:         2,
		ParallelToolTimeoutDefault: time.Second,
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected partial success to continue, got %#v", response)
	}
	if len(response.ToolResults) != 2 {
		t.Fatalf("expected both results to reach provider, got %#v", response.ToolResults)
	}
}
