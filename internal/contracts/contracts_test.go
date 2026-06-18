package contracts

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAgentResponseSerializesStatusAndErrorShape(t *testing.T) {
	response := AgentResponse{
		RequestID: "req_001",
		SessionID: "sess_001",
		Status:    AgentStatusApprovalRequired,
		Error: &ErrorShape{
			Code:      ErrorActionRequiresApproval,
			Message:   "approval required",
			Source:    ErrorSourcePolicy,
			Retryable: false,
		},
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var decoded AgentResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if decoded.Status != AgentStatusApprovalRequired {
		t.Fatalf("expected approval_required, got %q", decoded.Status)
	}
	if decoded.Error == nil || decoded.Error.Code != ErrorActionRequiresApproval {
		t.Fatalf("expected error shape to round-trip, got %#v", decoded.Error)
	}
}

func TestRiskDecisionSerializesCheckedAt(t *testing.T) {
	checkedAt := time.Date(2026, 5, 29, 9, 0, 0, 0, time.FixedZone("ICT", 7*60*60))
	decision := RiskDecision{
		ToolCallID:       "call_001",
		ToolName:         "calendar.createEvent",
		RiskLevel:        RiskLevelExternalWrite,
		Decision:         RiskDecisionRequiresApproval,
		RequiresApproval: true,
		CheckedAt:        checkedAt,
	}

	data, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}

	var decoded RiskDecision
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal decision: %v", err)
	}
	if decoded.CheckedAt.Format(time.RFC3339) != "2026-05-29T09:00:00+07:00" {
		t.Fatalf("unexpected checkedAt: %s", decoded.CheckedAt.Format(time.RFC3339))
	}
}

// ─── ToolResult contract tests ────────────────────────────────────────────────

func TestToolResultRoundTrip(t *testing.T) {
	original := ToolResult{
		ToolCallID: "call_rt_001",
		ToolName:   "filesystem.readFile",
		Success:    true,
		Data:       map[string]any{"content": "hello world", "lines": 1},
		ArtifactRef: &ArtifactRef{
			Kind:  "file",
			Label: "readme.txt",
			URI:   "/workspace/readme.txt",
		},
		Metadata:  map[string]any{"total_lines": 1, "size_bytes": 11},
		Truncated: false,
		Redacted:  false,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal ToolResult: %v", err)
	}

	var decoded ToolResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal ToolResult: %v", err)
	}

	if decoded.ToolCallID != original.ToolCallID {
		t.Errorf("ToolCallID mismatch: got %q", decoded.ToolCallID)
	}
	if decoded.ToolName != original.ToolName {
		t.Errorf("ToolName mismatch: got %q", decoded.ToolName)
	}
	if decoded.Success != original.Success {
		t.Errorf("Success mismatch: got %v", decoded.Success)
	}
	if decoded.ArtifactRef == nil {
		t.Fatal("ArtifactRef must survive round-trip")
	}
	if decoded.ArtifactRef.Kind != "file" || decoded.ArtifactRef.URI != "/workspace/readme.txt" {
		t.Errorf("ArtifactRef fields mismatch: got %#v", decoded.ArtifactRef)
	}
	if decoded.Truncated {
		t.Error("Truncated should be false after round-trip")
	}
	if decoded.Redacted {
		t.Error("Redacted should be false after round-trip")
	}
}

func TestToolResultWithArtifactRefAndFlags(t *testing.T) {
	original := ToolResult{
		ToolCallID:  "call_rt_002",
		ToolName:    "filesystem.readFile",
		Success:     true,
		Truncated:   true,
		Redacted:    true,
		ArtifactRef: &ArtifactRef{Kind: "file", URI: "/workspace/big.log", Label: "big.log"},
		Metadata:    map[string]any{"total_lines": 50000, "size_bytes": 1048576},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ToolResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.Truncated {
		t.Error("Truncated=true must survive round-trip")
	}
	if !decoded.Redacted {
		t.Error("Redacted=true must survive round-trip")
	}
	totalLines, ok := decoded.Metadata["total_lines"]
	if !ok {
		t.Fatal("Metadata must survive round-trip")
	}
	// JSON numbers unmarshal as float64
	if totalLines.(float64) != 50000 {
		t.Errorf("Metadata total_lines = %v, want 50000", totalLines)
	}
}

// ─── ValidateToolResult tests ─────────────────────────────────────────────────

func TestValidateToolResultAcceptsValidSuccess(t *testing.T) {
	r := ToolResult{
		ToolCallID: "call_v_001",
		ToolName:   "calculator",
		Success:    true,
	}
	if err := ValidateToolResult(r); err != nil {
		t.Errorf("expected nil error for valid success result, got: %v", err)
	}
}

func TestValidateToolResultAcceptsValidFailure(t *testing.T) {
	r := ToolResult{
		ToolCallID: "call_v_002",
		ToolName:   "calculator",
		Success:    false,
		Error:      &ErrorShape{Code: ErrorInternal, Message: "crash"},
	}
	if err := ValidateToolResult(r); err != nil {
		t.Errorf("expected nil error for valid failure result, got: %v", err)
	}
}

func TestValidateToolResultRejectsEmptyToolCallID(t *testing.T) {
	r := ToolResult{ToolCallID: "", ToolName: "calculator", Success: true}
	if err := ValidateToolResult(r); err == nil {
		t.Error("expected error for empty ToolCallID")
	}
}

func TestValidateToolResultRejectsEmptyToolName(t *testing.T) {
	r := ToolResult{ToolCallID: "call_v_003", ToolName: "", Success: true}
	if err := ValidateToolResult(r); err == nil {
		t.Error("expected error for empty ToolName")
	}
}

func TestValidateToolResultRejectsSuccessWithError(t *testing.T) {
	r := ToolResult{
		ToolCallID: "call_v_004",
		ToolName:   "calculator",
		Success:    true,
		Error:      &ErrorShape{Code: ErrorInternal, Message: "should not be here"},
	}
	if err := ValidateToolResult(r); err == nil {
		t.Error("expected error when Success=true but Error is non-nil")
	}
}

func TestValidateToolResultRejectsFailureWithoutError(t *testing.T) {
	r := ToolResult{
		ToolCallID: "call_v_005",
		ToolName:   "calculator",
		Success:    false,
		Error:      nil,
	}
	if err := ValidateToolResult(r); err == nil {
		t.Error("expected error when Success=false but Error is nil")
	}
}

func TestApprovalRequestSerializesParentApprovalAndRevisedStatus(t *testing.T) {
	request := ApprovalRequest{
		ApprovalID:       "appr_001",
		ParentApprovalID: "appr_root",
		RequestID:        "req_001",
		SessionID:        "sess_001",
		ToolCallID:       "call_001",
		Status:           ApprovalStatusRevised,
		RiskLevel:        RiskLevelExternalWrite,
		Summary:          "Revise approval request",
		ToolCall: ToolCall{
			ToolName: "calendar.createEvent",
		},
		CreatedAt: time.Date(2026, 5, 29, 9, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 5, 29, 9, 10, 0, 0, time.UTC),
	}

	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var decoded ApprovalRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if decoded.Status != ApprovalStatusRevised {
		t.Fatalf("expected revised status, got %q", decoded.Status)
	}
	if decoded.ParentApprovalID != "appr_root" {
		t.Fatalf("expected parentApprovalId to round-trip, got %#v", decoded.ParentApprovalID)
	}
}

// ─── GovernanceMetadata tests ─────────────────────────────────────────────────

func TestGovernanceMetadataRoundTripOnToolCall(t *testing.T) {
	original := ToolCall{
		ToolCallID: "call_g_001",
		ToolName:   "calendar.createEvent",
		Input:      map[string]any{"title": "Họp"},
		Governance: &GovernanceMetadata{
			Model:             "claude-opus-4-8",
			PromptVersion:     "abc12345",
			ToolSchemaVersion: "deadbeef",
			PolicyDecisionRef: "policy:run_x:call_g_001:1781524800",
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ToolCall
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Governance == nil {
		t.Fatal("Governance must survive round-trip")
	}
	if decoded.Governance.Model != "claude-opus-4-8" {
		t.Errorf("Model mismatch: got %q", decoded.Governance.Model)
	}
	if decoded.Governance.PromptVersion != "abc12345" {
		t.Errorf("PromptVersion mismatch: got %q", decoded.Governance.PromptVersion)
	}
	if decoded.Governance.ToolSchemaVersion != "deadbeef" {
		t.Errorf("ToolSchemaVersion mismatch: got %q", decoded.Governance.ToolSchemaVersion)
	}
	if decoded.Governance.PolicyDecisionRef != "policy:run_x:call_g_001:1781524800" {
		t.Errorf("PolicyDecisionRef mismatch: got %q", decoded.Governance.PolicyDecisionRef)
	}
}

func TestGovernanceMetadataOmittedWhenAbsent(t *testing.T) {
	// Existing producers that don't yet populate governance must round-trip
	// without an empty object hanging around in JSON.
	tc := ToolCall{ToolCallID: "call_g_002", ToolName: "calculator"}
	data, _ := json.Marshal(tc)
	if strings.Contains(string(data), "governance") {
		t.Errorf("governance field must be omitted when nil, got: %s", string(data))
	}
}

func TestGovernanceMetadataOnRiskDecisionAndApproval(t *testing.T) {
	// PolicyDecisionRef on RiskDecision and Governance on ApprovalRequest must
	// both round-trip cleanly so consumers that read either record see the
	// same provenance.
	rd := RiskDecision{
		ToolCallID:        "call_g_003",
		ToolName:          "gmail.sendDraft",
		RiskLevel:         RiskLevelExternalWrite,
		Decision:          RiskDecisionRequiresApproval,
		RequiresApproval:  true,
		CheckedAt:         time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
		PolicyDecisionRef: "policy:run_y:call_g_003:1781524800",
	}
	rdJSON, _ := json.Marshal(rd)
	var rdDecoded RiskDecision
	if err := json.Unmarshal(rdJSON, &rdDecoded); err != nil {
		t.Fatalf("RiskDecision unmarshal: %v", err)
	}
	if rdDecoded.PolicyDecisionRef != rd.PolicyDecisionRef {
		t.Errorf("PolicyDecisionRef mismatch: got %q", rdDecoded.PolicyDecisionRef)
	}

	approval := ApprovalRequest{
		ApprovalID: "appr_g_001",
		RequestID:  "req_g_001",
		SessionID:  "sess_g_001",
		ToolCallID: "call_g_003",
		Status:     ApprovalStatusPending,
		RiskLevel:  RiskLevelExternalWrite,
		Summary:    "Send draft",
		ToolCall:   ToolCall{ToolName: "gmail.sendDraft"},
		CreatedAt:  time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
		ExpiresAt:  time.Date(2026, 6, 15, 12, 10, 0, 0, time.UTC),
		Governance: &GovernanceMetadata{
			Model:             "claude-opus-4-8",
			PromptVersion:     "abc12345",
			ToolSchemaVersion: "feedface",
			PolicyDecisionRef: rd.PolicyDecisionRef,
		},
	}
	apJSON, _ := json.Marshal(approval)
	var apDecoded ApprovalRequest
	if err := json.Unmarshal(apJSON, &apDecoded); err != nil {
		t.Fatalf("ApprovalRequest unmarshal: %v", err)
	}
	if apDecoded.Governance == nil || apDecoded.Governance.PolicyDecisionRef != rd.PolicyDecisionRef {
		t.Errorf("ApprovalRequest.Governance.PolicyDecisionRef mismatch: %#v", apDecoded.Governance)
	}
}

func TestToolResultGovernanceAndSourceRoundTrip(t *testing.T) {
	original := ToolResult{
		ToolCallID: "call_g_004",
		ToolName:   "gmail.listEmails",
		Success:    true,
		Source:     "tool:google_workspace",
		Governance: &GovernanceMetadata{
			Model:             "claude-opus-4-8",
			PromptVersion:     "abc12345",
			ToolSchemaVersion: "cafef00d",
			PolicyDecisionRef: "policy:run_z:call_g_004:1781524800",
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ToolResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Source != "tool:google_workspace" {
		t.Errorf("Source mismatch: got %q", decoded.Source)
	}
	if decoded.Governance == nil || decoded.Governance.Model != "claude-opus-4-8" {
		t.Errorf("Governance mismatch: %#v", decoded.Governance)
	}
}
