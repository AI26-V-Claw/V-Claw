package policies

import (
	"context"
	"testing"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/tools"
)

type parallelSafeReadTool struct{}

func (parallelSafeReadTool) Name() string                 { return "safe.read" }
func (parallelSafeReadTool) Description() string          { return "Safe read tool." }
func (parallelSafeReadTool) Parameters() tools.ToolSchema { return tools.ToolSchema{"type": "object"} }
func (parallelSafeReadTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (parallelSafeReadTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSafeRead }
func (parallelSafeReadTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "safe read result",
		ContentForUser: "safe read result",
	}
}

type parallelSafeComputeTool struct{}

func (parallelSafeComputeTool) Name() string        { return "safe.compute" }
func (parallelSafeComputeTool) Description() string { return "Safe compute tool." }
func (parallelSafeComputeTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{"type": "object"}
}
func (parallelSafeComputeTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (parallelSafeComputeTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSafeCompute }
func (parallelSafeComputeTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "safe compute result",
		ContentForUser: "safe compute result",
	}
}

type parallelWriteTool struct{}

func (parallelWriteTool) Name() string                 { return "write.tool" }
func (parallelWriteTool) Description() string          { return "Write tool." }
func (parallelWriteTool) Parameters() tools.ToolSchema { return tools.ToolSchema{"type": "object"} }
func (parallelWriteTool) Capability() tools.Capability { return tools.CapabilityMutating }
func (parallelWriteTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelExternalWrite }
func (parallelWriteTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "write result",
		ContentForUser: "write result",
	}
}

type parallelDestructiveTool struct{}

func (parallelDestructiveTool) Name() string        { return "destructive.tool" }
func (parallelDestructiveTool) Description() string { return "Destructive tool." }
func (parallelDestructiveTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{"type": "object"}
}
func (parallelDestructiveTool) Capability() tools.Capability { return tools.CapabilityMutating }
func (parallelDestructiveTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelDestructive }
func (parallelDestructiveTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "destructive result",
		ContentForUser: "destructive result",
	}
}

type parallelCodeExecutionTool struct{}

func (parallelCodeExecutionTool) Name() string        { return "code.exec" }
func (parallelCodeExecutionTool) Description() string { return "Code execution tool." }
func (parallelCodeExecutionTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{"type": "object"}
}
func (parallelCodeExecutionTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (parallelCodeExecutionTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelCodeExecution }
func (parallelCodeExecutionTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "code execution result",
		ContentForUser: "code execution result",
	}
}

type readOnlyCodeExecRiskTool struct{}

func (readOnlyCodeExecRiskTool) Name() string { return "readonly.code.exec.risk" }
func (readOnlyCodeExecRiskTool) Description() string {
	return "Read-only tool with unsafe code execution risk."
}
func (readOnlyCodeExecRiskTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{"type": "object"}
}
func (readOnlyCodeExecRiskTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (readOnlyCodeExecRiskTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelCodeExecution }
func (readOnlyCodeExecRiskTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "readonly unsafe risk result",
		ContentForUser: "readonly unsafe risk result",
	}
}

func TestToolPolicyCanRunInParallelAllowsSafeRead(t *testing.T) {
	policy := NewToolPolicy()
	if !policy.CanRunInParallel(parallelSafeReadTool{}) {
		t.Fatal("expected safe read tool to be parallel-safe")
	}
}

func TestToolPolicyCanRunInParallelAllowsSafeCompute(t *testing.T) {
	policy := NewToolPolicy()
	if !policy.CanRunInParallel(parallelSafeComputeTool{}) {
		t.Fatal("expected safe compute tool to be parallel-safe")
	}
}

func TestToolPolicyCanRunInParallelRejectsWriteTool(t *testing.T) {
	policy := NewToolPolicy()
	if policy.CanRunInParallel(parallelWriteTool{}) {
		t.Fatal("expected write tool to be rejected")
	}
}

func TestToolPolicyCanRunInParallelRejectsDestructiveTool(t *testing.T) {
	policy := NewToolPolicy()
	if policy.CanRunInParallel(parallelDestructiveTool{}) {
		t.Fatal("expected destructive tool to be rejected")
	}
}

func TestToolPolicyCanRunInParallelRejectsCodeExecutionTool(t *testing.T) {
	policy := NewToolPolicy()
	if policy.CanRunInParallel(parallelCodeExecutionTool{}) {
		t.Fatal("expected code execution tool to be rejected")
	}
}

// RequiresApproval is enforced at the scheduler layer, not by CanRunInParallel.
func TestToolPolicyCanRunInParallelRejectsReadOnlyWithUnsafeRisk(t *testing.T) {
	policy := NewToolPolicy()
	if policy.CanRunInParallel(readOnlyCodeExecRiskTool{}) {
		t.Fatal("expected read-only tool with unsafe risk to be rejected")
	}
}

func TestToolPolicyCanRunInParallelRejectsNilTool(t *testing.T) {
	policy := NewToolPolicy()
	var tool tools.Tool
	if policy.CanRunInParallel(tool) {
		t.Fatal("expected nil tool to be rejected")
	}
}

func TestToolPolicyCanExecuteMatchesSafeReadAndComputeTools(t *testing.T) {
	policy := NewToolPolicy()
	if !policy.CanExecute(parallelSafeReadTool{}) {
		t.Fatal("expected safe read tool to be executable")
	}
	if !policy.CanExecute(parallelSafeComputeTool{}) {
		t.Fatal("expected safe compute tool to be executable")
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
	if unknown.RiskLevel != contracts.RiskLevelDestructive {
		t.Fatalf("expected unknown tool riskLevel=destructive, got %#v", unknown)
	}
}

func TestToolPolicyDecideToolCallHonorsUserPolicyConfig(t *testing.T) {
	policy := NewToolPolicyWithConfig(UserPolicyConfig{
		AutoAllow:       []contracts.RiskLevel{contracts.RiskLevelExternalWrite},
		RequireApproval: []contracts.RiskLevel{contracts.RiskLevelSafeRead},
		AlwaysBlock:     []contracts.RiskLevel{contracts.RiskLevelLocalWrite},
	})

	allow := policy.DecideToolCall("call_allow", tools.ToolDefinition{
		Name:             "calendar.createEvent",
		Capability:       tools.CapabilityMutating,
		RiskLevel:        tools.RiskLevelExternalWrite,
		RequiresApproval: true,
		Enabled:          true,
	}, true, time.Now())
	if allow.Decision != contracts.RiskDecisionAllow {
		t.Fatalf("expected auto-allow, got %#v", allow)
	}
	if allow.RequiresApproval {
		t.Fatalf("auto-allow should skip approval, got %#v", allow)
	}

	block := policy.DecideToolCall("call_block", tools.ToolDefinition{
		Name:       "file.write",
		Capability: tools.CapabilityReadOnly,
		RiskLevel:  tools.RiskLevelLocalWrite,
		Enabled:    true,
	}, true, time.Now())
	if block.Decision != contracts.RiskDecisionBlock {
		t.Fatalf("expected always_block, got %#v", block)
	}

	approve := policy.DecideToolCall("call_approval", tools.ToolDefinition{
		Name:       "gmail.listEmails",
		Capability: tools.CapabilityReadOnly,
		RiskLevel:  tools.RiskLevelSafeRead,
		Enabled:    true,
	}, true, time.Now())
	if approve.Decision != contracts.RiskDecisionRequiresApproval {
		t.Fatalf("expected require_approval override, got %#v", approve)
	}
}
