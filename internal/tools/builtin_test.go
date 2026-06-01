package tools

import (
	"context"
	"testing"
	"time"
)

func TestRegisterBuiltInToolsListsMetadata(t *testing.T) {
	registry := NewToolRegistry()
	if err := RegisterBuiltInTools(registry); err != nil {
		t.Fatalf("register built-in tools: %v", err)
	}

	defs := registry.ListTools()
	if len(defs) != 2 {
		t.Fatalf("expected 2 built-in tools, got %d", len(defs))
	}

	if defs[0].Name != "calculator" {
		t.Fatalf("expected calculator to sort first, got %q", defs[0].Name)
	}
	if defs[0].Capability != CapabilityReadOnly {
		t.Fatalf("expected calculator read-only capability, got %q", defs[0].Capability)
	}
	if defs[0].RiskLevel != RiskLevelSafeCompute {
		t.Fatalf("expected calculator safe compute risk, got %q", defs[0].RiskLevel)
	}
	if defs[0].Parameters["type"] != "object" {
		t.Fatalf("expected calculator object schema, got %#v", defs[0].Parameters)
	}

	if defs[1].Name != "get_current_time" {
		t.Fatalf("expected get_current_time to sort second, got %q", defs[1].Name)
	}
	if defs[1].Capability != CapabilityReadOnly {
		t.Fatalf("expected current time read-only capability, got %q", defs[1].Capability)
	}
	if defs[1].RiskLevel != RiskLevelSafeRead {
		t.Fatalf("expected current time safe read risk, got %q", defs[1].RiskLevel)
	}
	if defs[1].Parameters["type"] != "object" {
		t.Fatalf("expected current time object schema, got %#v", defs[1].Parameters)
	}
}

func TestCurrentTimeToolExecuteViaRegistry(t *testing.T) {
	registry := NewToolRegistry()
	fixedTime := time.Date(2026, 5, 29, 9, 0, 0, 0, time.FixedZone("ICT", 7*60*60))
	if err := registry.Register(NewCurrentTimeToolWithClock(func() time.Time { return fixedTime })); err != nil {
		t.Fatalf("register current time tool: %v", err)
	}

	result := registry.Execute(context.Background(), ToolCall{
		ID:   "call_time",
		Name: "get_current_time",
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %#v", result.Error)
	}
	if result.ToolCallID != "call_time" {
		t.Fatalf("expected call_time, got %q", result.ToolCallID)
	}
	if result.ToolName != "get_current_time" {
		t.Fatalf("expected get_current_time, got %q", result.ToolName)
	}
	if result.ContentForUser != "2026-05-29T09:00:00+07:00" {
		t.Fatalf("unexpected time result: %q", result.ContentForUser)
	}
}

func TestCalculatorToolExecuteViaRegistry(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(NewCalculatorTool()); err != nil {
		t.Fatalf("register calculator tool: %v", err)
	}

	result := registry.Execute(context.Background(), ToolCall{
		ID:   "call_calc",
		Name: "calculator",
		Arguments: map[string]any{
			"operation": "multiply",
			"a":         6,
			"b":         7,
		},
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %#v", result.Error)
	}
	if result.ToolCallID != "call_calc" {
		t.Fatalf("expected call_calc, got %q", result.ToolCallID)
	}
	if result.ToolName != "calculator" {
		t.Fatalf("expected calculator, got %q", result.ToolName)
	}
	if result.ContentForLLM != "multiply(6, 7) = 42" {
		t.Fatalf("unexpected calculator result: %q", result.ContentForLLM)
	}
	if result.ContentForUser != result.ContentForLLM {
		t.Fatalf("expected same user and LLM content, got %q vs %q", result.ContentForUser, result.ContentForLLM)
	}
}

func TestCalculatorToolRejectsInvalidArguments(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(NewCalculatorTool()); err != nil {
		t.Fatalf("register calculator tool: %v", err)
	}

	result := registry.Execute(context.Background(), ToolCall{
		ID:   "call_invalid",
		Name: "calculator",
		Arguments: map[string]any{
			"operation": "divide",
			"a":         10,
			"b":         0,
		},
	})

	if result.Success {
		t.Fatal("expected invalid calculator arguments to fail")
	}
	if result.Error == nil {
		t.Fatal("expected error for invalid calculator arguments")
	}
	if result.Error.Code != ErrorInvalidArgument {
		t.Fatalf("expected %s, got %s", ErrorInvalidArgument, result.Error.Code)
	}
	if result.Error.Code != "TOOL_INPUT_INVALID" {
		t.Fatalf("expected standardized TOOL_INPUT_INVALID code, got %s", result.Error.Code)
	}
	if result.ToolCallID != "call_invalid" {
		t.Fatalf("expected call_invalid, got %q", result.ToolCallID)
	}
	if result.ToolName != "calculator" {
		t.Fatalf("expected calculator, got %q", result.ToolName)
	}
}
