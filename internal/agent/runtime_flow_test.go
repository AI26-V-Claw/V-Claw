package agent

import (
	"context"
	"testing"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

// ---------- Flow integration tests ----------
// These tests verify multi-step workspace flows using fakeProvider,
// covering the three main task flows:
//   1. Gmail → Calendar → Chat
//   2. Web Search → Docs
//   3. Mixed batch (read + write in same batch)

// flowToolRegistry returns a ToolRegistry populated with the tools needed
// for flow integration tests.
func flowToolRegistry() *tools.ToolRegistry {
	registry := tools.NewToolRegistry()
	stubs := []struct {
		name string
		cap  tools.Capability
		risk tools.RiskLevel
	}{
		{"gmail.listEmails", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		{"gmail.getEmail", tools.CapabilityReadOnly, tools.RiskLevelSensitiveRead},
		{"calendar.listEvents", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		{"calendar.getEvent", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		{"calendar.createEvent", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"chat.listSpaces", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		{"chat.sendMessage", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"web.search", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		{"web.fetch", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		{"docs.createDocument", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"drive.listFiles", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		{"drive.shareFile", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"drive.trashFile", tools.CapabilityMutating, tools.RiskLevelDestructive},
		{"sheets.createSpreadsheet", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"docs.deleteContent", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"sheets.clearValues", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
	}
	for _, s := range stubs {
		_ = registry.Register(flowStubTool{name: s.name, cap: s.cap, risk: s.risk})
	}
	return registry
}

type flowStubTool struct {
	name string
	cap  tools.Capability
	risk tools.RiskLevel
}

func (f flowStubTool) Name() string               { return f.name }
func (f flowStubTool) Description() string         { return f.name }
func (f flowStubTool) Parameters() tools.ToolSchema { return tools.ToolSchema{} }
func (f flowStubTool) Capability() tools.Capability { return f.cap }
func (f flowStubTool) RiskLevel() tools.RiskLevel   { return f.risk }
func (f flowStubTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true, ContentForLLM: f.name + " ok"}
}

// TestFlowReadBeforeWriteBlocksWriteOnly verifies that a pure write
// without prior read is nudged, and fails on second attempt.
func TestFlowReadBeforeWriteBlocksWriteOnly(t *testing.T) {
	registry := flowToolRegistry()
	provider := &fakeProvider{responses: []providers.ChatResponse{
		// Iteration 1: LLM proposes write without read → nudge.
		{Message: providers.Message{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{ID: "c1", Name: "calendar.createEvent", Arguments: map[string]any{"eventId": "ev1"}},
			},
		}},
		// Iteration 2: LLM still proposes write → should fail.
		{Message: providers.Message{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{ID: "c2", Name: "calendar.createEvent", Arguments: map[string]any{"eventId": "ev2"}},
			},
		}},
	}}

	runtime := NewRuntime(RuntimeConfig{
		Provider: provider,
		Registry: registry,
	})
	message := runtimeTestMessage()
	message.Text = "update event"

	response, err := runtime.Run(context.Background(), message)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if response.Status != contracts.AgentStatusFailed {
		t.Fatalf("expected failed status after 2nd write violation, got %v", response.Status)
	}
	if response.Error == nil || response.Error.Code != "WRITE_BEFORE_READ" {
		t.Fatalf("expected WRITE_BEFORE_READ error, got %+v", response.Error)
	}
}

// TestFlowCreateFromScratchAllowedWithoutRead verifies that a
// create-from-scratch write (no resource ID, create verb) passes
// without requiring a prior read.
func TestFlowCreateFromScratchAllowedWithoutRead(t *testing.T) {
	registry := flowToolRegistry()
	provider := &fakeProvider{responses: []providers.ChatResponse{
		// LLM proposes create event with all user input.
		{Message: providers.Message{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{ID: "c1", Name: "calendar.createEvent", Arguments: map[string]any{
					"title": "Sprint Demo", "start": "2026-06-28T10:00:00+07:00", "end": "2026-06-28T11:00:00+07:00",
				}},
			},
		}},
		// After tool exec, LLM responds.
		{Message: providers.Message{Role: "assistant", Content: "Đã tạo lịch Sprint Demo"}},
	}}

	runtime := NewRuntime(RuntimeConfig{
		Provider: provider,
		Registry: registry,
	})
	message := runtimeTestMessage()
	message.Text = "Tạo lịch Sprint Demo 10h-11h ngày mai"

	response, err := runtime.Run(context.Background(), message)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if response.Status == contracts.AgentStatusFailed {
		t.Fatalf("create-from-scratch should not fail, got %+v", response)
	}
}

// TestFlowBatchReadWriteAllowed verifies that a batch containing
// read + write (read before write) passes validation.
func TestFlowBatchReadWriteAllowed(t *testing.T) {
	registry := flowToolRegistry()
	provider := &fakeProvider{responses: []providers.ChatResponse{
		// Batch: web.search + docs.createDocument.
		{Message: providers.Message{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{ID: "c1", Name: "web.search", Arguments: map[string]any{"query": "Go testing"}},
				{ID: "c2", Name: "docs.createDocument", Arguments: map[string]any{"title": "Research"}},
			},
		}},
		{Message: providers.Message{Role: "assistant", Content: "Đã tạo tài liệu research"}},
	}}

	runtime := NewRuntime(RuntimeConfig{
		Provider: provider,
		Registry: registry,
	})
	message := runtimeTestMessage()
	message.Text = "Tìm kiếm về Go testing rồi tạo docs"

	response, err := runtime.Run(context.Background(), message)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if response.Status == contracts.AgentStatusFailed {
		t.Fatalf("batch with read before write should pass, got %+v", response)
	}
}

// TestFlowWriteMissingToolsFromRegistryCoverage verifies that write
// tools previously missing from the hard-coded list (drive.shareFile,
// docs.deleteContent, sheets.clearValues, drive.trashFile) are now
// detected as workspace writes by the registry-driven guard.
func TestFlowWriteMissingToolsFromRegistryCoverage(t *testing.T) {
	registry := flowToolRegistry()

	previouslyMissing := []string{
		"drive.shareFile",
		"drive.trashFile",
		"docs.deleteContent",
		"sheets.clearValues",
	}

	for _, toolName := range previouslyMissing {
		t.Run(toolName, func(t *testing.T) {
			if !IsWorkspaceWriteTool(toolName, registry) {
				t.Errorf("expected %q to be detected as workspace write tool", toolName)
			}
		})
	}
}

// TestFlowReadMissingToolsFromRegistryCoverage verifies that read
// tools previously missing from the hard-coded list are now covered.
func TestFlowReadMissingToolsFromRegistryCoverage(t *testing.T) {
	registry := flowToolRegistry()

	previouslyMissing := []string{
		"web.fetch",
		"calendar.getEvent",
	}

	for _, toolName := range previouslyMissing {
		t.Run(toolName, func(t *testing.T) {
			if !IsWorkspaceReadTool(toolName, registry) {
				t.Errorf("expected %q to be detected as workspace read tool", toolName)
			}
		})
	}
}
