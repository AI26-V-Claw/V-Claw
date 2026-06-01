package policies

import (
	"testing"

	"vclaw/internal/tools"
)

func TestToolPolicyFilterToolsAllowsOnlySafeReadOnlyTools(t *testing.T) {
	policy := NewToolPolicy()
	definitions := []tools.ToolDefinition{
		{Name: "safe.read", Capability: tools.CapabilityReadOnly, RiskLevel: tools.RiskLevelSafeRead},
		{Name: "safe.compute", Capability: tools.CapabilityReadOnly, RiskLevel: tools.RiskLevelSafeCompute},
		{Name: "external.write", Capability: tools.CapabilityMutating, RiskLevel: tools.RiskLevelExternalWrite},
		{Name: "code.exec", Capability: tools.CapabilityReadOnly, RiskLevel: tools.RiskLevelCodeExecution},
		{Name: "destructive", Capability: tools.CapabilityMutating, RiskLevel: tools.RiskLevelDestructive},
	}

	allowed := policy.FilterTools(definitions)

	if len(allowed) != 2 {
		t.Fatalf("expected 2 allowed tools, got %d: %#v", len(allowed), allowed)
	}
	if allowed[0].Name != "safe.read" || allowed[1].Name != "safe.compute" {
		t.Fatalf("unexpected allowed tools: %#v", allowed)
	}
}
