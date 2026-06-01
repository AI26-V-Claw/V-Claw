package tools

import (
	"context"
	"testing"
)

type sampleTool struct{}

func (sampleTool) Name() string {
	return "test.echo"
}

func (sampleTool) Description() string {
	return "Echoes the provided text."
}

func (sampleTool) Parameters() ToolSchema {
	return ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{"type": "string"},
		},
	}
}

func (sampleTool) Capability() Capability {
	return CapabilityReadOnly
}

func (sampleTool) RiskLevel() RiskLevel {
	return RiskLevelSafeCompute
}

func (sampleTool) Execute(_ context.Context, call ToolCall) ToolResult {
	text, _ := call.Arguments["text"].(string)
	return ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  text,
		ContentForUser: text,
	}
}

func TestToolRegistryExecuteSampleTool(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(sampleTool{}); err != nil {
		t.Fatalf("register sample tool: %v", err)
	}

	result := registry.Execute(context.Background(), ToolCall{
		ID:   "call_001",
		Name: "test.echo",
		Arguments: map[string]any{
			"text": "hello",
		},
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %#v", result.Error)
	}
	if result.ToolCallID != "call_001" {
		t.Fatalf("expected tool call id call_001, got %q", result.ToolCallID)
	}
	if result.ToolName != "test.echo" {
		t.Fatalf("expected tool name test.echo, got %q", result.ToolName)
	}
	if result.ContentForLLM != "hello" {
		t.Fatalf("expected echoed content, got %q", result.ContentForLLM)
	}
}

func TestToolRegistryListTools(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(sampleTool{}); err != nil {
		t.Fatalf("register sample tool: %v", err)
	}

	tools := registry.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "test.echo" {
		t.Fatalf("expected test.echo, got %q", tools[0].Name)
	}
	if tools[0].Capability != CapabilityReadOnly {
		t.Fatalf("expected read-only capability, got %q", tools[0].Capability)
	}
	if tools[0].RiskLevel != RiskLevelSafeCompute {
		t.Fatalf("expected safe compute risk, got %q", tools[0].RiskLevel)
	}
}

func TestToolRegistryExecuteMissingTool(t *testing.T) {
	registry := NewToolRegistry()

	result := registry.Execute(context.Background(), ToolCall{
		ID:   "call_missing",
		Name: "missing.tool",
	})

	if result.Success {
		t.Fatal("expected missing tool result to fail")
	}
	if result.Error == nil {
		t.Fatal("expected missing tool error")
	}
	if result.Error.Code != ErrorToolNotFound {
		t.Fatalf("expected %s, got %s", ErrorToolNotFound, result.Error.Code)
	}
	if result.Error.Code != "TOOL_NOT_FOUND" {
		t.Fatalf("expected standardized TOOL_NOT_FOUND code, got %s", result.Error.Code)
	}
	if result.ToolCallID != "call_missing" {
		t.Fatalf("expected tool call id call_missing, got %q", result.ToolCallID)
	}
	if result.ToolName != "missing.tool" {
		t.Fatalf("expected missing.tool, got %q", result.ToolName)
	}
}

func TestToolRegistryRejectsDuplicateTool(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(sampleTool{}); err != nil {
		t.Fatalf("register sample tool: %v", err)
	}
	if err := registry.Register(sampleTool{}); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}
