package sandbox

import (
	"testing"

	"vclaw/internal/tools"
)

func TestRegisterToolsMetadataRequiresApproval(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry); err != nil {
		t.Fatalf("RegisterTools() error = %v", err)
	}

	for _, name := range []string{ToolNameRunPython, ToolNameRunShell} {
		definition, ok := registry.GetDefinition(name)
		if !ok {
			t.Fatalf("expected tool definition for %s", name)
		}
		if definition.RiskLevel != tools.RiskLevelCodeExecution {
			t.Fatalf("expected code_execution risk for %s, got %q", name, definition.RiskLevel)
		}
		if !definition.RequiresApproval {
			t.Fatalf("expected %s to require approval", name)
		}
	}
}
