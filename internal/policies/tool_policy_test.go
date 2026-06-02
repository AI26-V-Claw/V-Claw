package policies

import (
	"testing"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/tools"
)

func TestToolPolicyFilterToolsAllowsOnlySafeReadOnlyTools(t *testing.T) {
	policy := NewToolPolicy()
	definitions := []tools.ToolDefinition{
		{Name: "safe.read", Capability: tools.CapabilityReadOnly, RiskLevel: tools.RiskLevelSafeRead, Enabled: true},
		{Name: "safe.compute", Capability: tools.CapabilityReadOnly, RiskLevel: tools.RiskLevelSafeCompute, Enabled: true},
		{Name: "external.write", Capability: tools.CapabilityMutating, RiskLevel: tools.RiskLevelExternalWrite, Enabled: true},
		{Name: "code.exec", Capability: tools.CapabilityReadOnly, RiskLevel: tools.RiskLevelCodeExecution, Enabled: true},
		{Name: "destructive", Capability: tools.CapabilityMutating, RiskLevel: tools.RiskLevelDestructive, Enabled: true},
	}

	allowed := policy.FilterTools(definitions)

	if len(allowed) != 2 {
		t.Fatalf("expected 2 allowed tools, got %d: %#v", len(allowed), allowed)
	}
	if allowed[0].Name != "safe.read" || allowed[1].Name != "safe.compute" {
		t.Fatalf("unexpected allowed tools: %#v", allowed)
	}
}

func TestToolPolicyDecideToolCallAllowsSafeReadOnlyTool(t *testing.T) {
	policy := NewToolPolicy()
	decision := policy.DecideToolCall("call_safe", tools.ToolDefinition{
		Name:       "gmail.listEmails",
		Capability: tools.CapabilityReadOnly,
		RiskLevel:  tools.RiskLevelSafeRead,
		Enabled:    true,
	}, true, time.Now())

	if decision.Decision != contracts.RiskDecisionAllow {
		t.Fatalf("expected allow, got %#v", decision)
	}
	if decision.RequiresApproval {
		t.Fatalf("safe read-only tool should not require approval")
	}
}

func TestToolPolicyDecideToolCallRequiresApprovalForSideEffectTool(t *testing.T) {
	policy := NewToolPolicy()
	decision := policy.DecideToolCall("call_write", tools.ToolDefinition{
		Name:             "calendar.createEvent",
		Capability:       tools.CapabilityMutating,
		RiskLevel:        tools.RiskLevelExternalWrite,
		RequiresApproval: true,
		Enabled:          true,
	}, true, time.Now())

	if decision.Decision != contracts.RiskDecisionRequiresApproval {
		t.Fatalf("expected requires_approval, got %#v", decision)
	}
	if !decision.RequiresApproval {
		t.Fatalf("expected RequiresApproval=true")
	}
}

func TestToolPolicyDecideToolCallBlocksDisabledOrUnknownTool(t *testing.T) {
	policy := NewToolPolicy()

	disabled := policy.DecideToolCall("call_disabled", tools.ToolDefinition{
		Name:       "disabled.tool",
		Capability: tools.CapabilityReadOnly,
		RiskLevel:  tools.RiskLevelSafeRead,
		Enabled:    false,
	}, true, time.Now())
	if disabled.Decision != contracts.RiskDecisionBlock {
		t.Fatalf("expected disabled tool to block, got %#v", disabled)
	}

	unknown := policy.DecideToolCall("call_unknown", tools.ToolDefinition{Name: "missing.tool"}, false, time.Now())
	if unknown.Decision != contracts.RiskDecisionBlock {
		t.Fatalf("expected unknown tool to block, got %#v", unknown)
	}
}
