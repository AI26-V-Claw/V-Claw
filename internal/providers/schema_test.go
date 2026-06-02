package providers

import (
	"testing"

	"vclaw/internal/tools"
)

func TestToolDefinitionsFromRegistryConvertsEnabledTools(t *testing.T) {
	definitions := []tools.ToolDefinition{
		{
			Name:        "calculator",
			Description: "Calculate.",
			Parameters:  tools.ToolSchema{"type": "object"},
			Enabled:     true,
		},
		{
			Name:        "disabled.tool",
			Description: "Disabled.",
			Parameters:  tools.ToolSchema{"type": "object"},
			Enabled:     false,
		},
	}

	converted := ToolDefinitionsFromRegistry(definitions)

	if len(converted) != 1 {
		t.Fatalf("expected 1 converted tool, got %d", len(converted))
	}
	if converted[0].Name != "calculator" {
		t.Fatalf("expected calculator, got %q", converted[0].Name)
	}
	if converted[0].Parameters["type"] != "object" {
		t.Fatalf("unexpected parameters: %#v", converted[0].Parameters)
	}
}
