package contracts

import (
	"encoding/json"
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
