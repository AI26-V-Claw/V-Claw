package agent

import (
	"context"
	"testing"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

// Tests in this file pin the contract from docs/03-contracts.md §3.11:
// Governance metadata MUST be stamped on contracts.ToolCall before the call
// crosses any Agent-Core boundary, AND on contracts.ApprovalRequest so the
// approval record is self-contained for audit/N4 without joining tool_calls.

// stampingTestTool is a no-op tool that exposes a non-trivial parameter
// schema so the runtime can compute a real ToolSchemaVersion hash.
type stampingTestTool struct{ name string }

func (t stampingTestTool) Name() string        { return t.name }
func (t stampingTestTool) Description() string { return "stamping test tool" }
func (t stampingTestTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{"type": "string"},
		},
	}
}
func (t stampingTestTool) Capability() tools.Capability { return tools.CapabilityMutating }
func (t stampingTestTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelExternalWrite }
func (t stampingTestTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "ok",
		ContentForUser: "ok",
	}
}

func TestApprovalRequestStampsGovernanceOnToolCallAndRequest(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := registry.Register(stampingTestTool{name: "chat.sendMessage"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Registry: registry,
		Model:    "claude-opus-4-8",
		Now:      fixedTestTime,
	})

	message := contracts.UserMessage{
		RequestID: "req_g_1",
		SessionID: "sess_g_1",
		Channel:   "telegram",
		Text:      "send chat",
		Timestamp: fixedTestTime(),
	}
	providerCall := providers.ToolCall{
		ID:        "call_g_1",
		Name:      "chat.sendMessage",
		Arguments: map[string]any{"text": "hi"},
	}
	decision := contracts.RiskDecision{
		ToolCallID:        providerCall.ID,
		ToolName:          providerCall.Name,
		RiskLevel:         contracts.RiskLevelExternalWrite,
		Decision:          contracts.RiskDecisionRequiresApproval,
		RequiresApproval:  true,
		CheckedAt:         fixedTestTime(),
		PolicyDecisionRef: "policy:run_g_1:call_g_1:1781524800",
	}

	approval := runtime.approvalRequest(message, providerCall, decision, nil)

	// ApprovalRequest must carry the bundle.
	if approval.Governance == nil {
		t.Fatal("ApprovalRequest.Governance is nil — approval record is not self-contained")
	}
	if approval.Governance.Model != "claude-opus-4-8" {
		t.Errorf("ApprovalRequest.Governance.Model = %q, want %q", approval.Governance.Model, "claude-opus-4-8")
	}
	if approval.Governance.PromptVersion == "" {
		t.Error("ApprovalRequest.Governance.PromptVersion is empty — runtime did not compute promptVersion")
	}
	if approval.Governance.ToolSchemaVersion == "" {
		t.Error("ApprovalRequest.Governance.ToolSchemaVersion is empty — runtime did not hash tool schema")
	}
	if approval.Governance.PolicyDecisionRef != decision.PolicyDecisionRef {
		t.Errorf("ApprovalRequest.Governance.PolicyDecisionRef = %q, want %q",
			approval.Governance.PolicyDecisionRef, decision.PolicyDecisionRef)
	}

	// ToolCall on the ApprovalRequest must carry the same bundle (the boundary
	// the contract pins — channel adapters and pg store both read from here).
	if approval.ToolCall.Governance == nil {
		t.Fatal("ApprovalRequest.ToolCall.Governance is nil — contract §3.11 says Stamped by Runtime before the call leaves Agent Core")
	}
	if got, want := *approval.ToolCall.Governance, *approval.Governance; got != want {
		t.Errorf("ToolCall.Governance differs from ApprovalRequest.Governance: %+v vs %+v", got, want)
	}
}

func TestLegacyApprovalRequestStampsGovernance(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := registry.Register(stampingTestTool{name: "sandbox.runPython"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Registry: registry,
		Model:    "claude-haiku-4-5-20251001",
		Now:      fixedTestTime,
	})

	approval := runtime.legacyApprovalRequest(
		contracts.UserMessage{RequestID: "req_l", SessionID: "sess_l", Timestamp: fixedTestTime()},
		providers.ToolCall{ID: "call_l", Name: "sandbox.runPython", Arguments: map[string]any{"code": "print(1)"}},
		contracts.RiskDecision{
			RiskLevel:         contracts.RiskLevelCodeExecution,
			CheckedAt:         fixedTestTime(),
			PolicyDecisionRef: "policy:run_l:call_l:1700000000",
		},
	)

	if approval.Governance == nil || approval.ToolCall.Governance == nil {
		t.Fatal("legacy approval path lost governance bundle")
	}
	if approval.Governance.Model != "claude-haiku-4-5-20251001" {
		t.Errorf("legacy approval model = %q", approval.Governance.Model)
	}
	if approval.ToolCall.Governance.PolicyDecisionRef != "policy:run_l:call_l:1700000000" {
		t.Errorf("legacy approval policy ref = %q", approval.ToolCall.Governance.PolicyDecisionRef)
	}
}

func TestGovernanceFromActionRecordRebuildsBundle(t *testing.T) {
	rec := ActionRecord{
		Model:             "claude-sonnet-4-6",
		PromptVersion:     "feedface",
		ToolSchemaVersion: "cafebabe",
		PolicyDecisionRef: "policy:run_a:call_a:1700000001",
	}
	gm := GovernanceFromActionRecord(rec)
	if gm == nil {
		t.Fatal("expected non-nil governance for populated record")
	}
	if gm.Model != rec.Model || gm.PromptVersion != rec.PromptVersion ||
		gm.ToolSchemaVersion != rec.ToolSchemaVersion || gm.PolicyDecisionRef != rec.PolicyDecisionRef {
		t.Errorf("GovernanceFromActionRecord did not copy all fields: %+v", gm)
	}

	if got := GovernanceFromActionRecord(ActionRecord{}); got != nil {
		t.Errorf("empty record should produce nil bundle, got %+v", got)
	}
}

func TestContractToolResultMergesRuntimeGovernanceAndPolicyRef(t *testing.T) {
	// The tool layer's PolicyDecisionRef on result must override the runtime's
	// pre-stamped value when both are present (live execution authoritative).
	result := tools.ToolResult{
		ToolCallID:        "call_m",
		ToolName:          "test.merge",
		Success:           true,
		ContentForLLM:     "ok",
		ContentForUser:    "ok",
		PolicyDecisionRef: "policy:from-result",
	}
	base := &contracts.GovernanceMetadata{
		Model:             "m1",
		PromptVersion:     "p1",
		ToolSchemaVersion: "s1",
		PolicyDecisionRef: "policy:from-runtime",
	}
	merged := contractToolResult(result, base)
	if merged.Governance == nil {
		t.Fatal("expected merged governance, got nil")
	}
	if merged.Governance.PolicyDecisionRef != "policy:from-result" {
		t.Errorf("expected result PolicyDecisionRef to win, got %q", merged.Governance.PolicyDecisionRef)
	}
	if merged.Governance.Model != "m1" || merged.Governance.PromptVersion != "p1" || merged.Governance.ToolSchemaVersion != "s1" {
		t.Errorf("expected runtime model/prompt/schema fields preserved, got %+v", merged.Governance)
	}

	// Empty result + nil base → no governance object at all.
	empty := contractToolResult(tools.ToolResult{ToolCallID: "x", ToolName: "y", Success: true}, nil)
	if empty.Governance != nil {
		t.Errorf("expected nil governance for empty inputs, got %+v", empty.Governance)
	}
}
