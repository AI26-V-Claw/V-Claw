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

func TestToolResultSerializesContentAndReferences(t *testing.T) {
	result := ToolResult{
		ToolCallID:     "call_001",
		ToolName:       "drive.getFileMetadata",
		Success:        true,
		ContentForLLM:  `{"id":"file_1"}`,
		ContentForUser: "File: Report",
		Data:           map[string]any{"payload": map[string]any{"id": "file_1"}},
		ArtifactRef:    &ArtifactRef{Kind: "drive_file", ID: "file_1", Label: "Report", URI: "https://drive.google.com/file/d/file_1"},
		SourceRefs:     []SourceRef{{Kind: "drive_file", ID: "file_1", Label: "Report"}},
		Metadata:       map[string]any{"provider": "google_drive"},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal tool result: %v", err)
	}
	var decoded ToolResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}
	if decoded.ContentForLLM == "" || decoded.ContentForUser == "" {
		t.Fatalf("expected content fields to round-trip, got %#v", decoded)
	}
	if decoded.ArtifactRef == nil || decoded.ArtifactRef.ID != "file_1" {
		t.Fatalf("expected artifact ref to round-trip, got %#v", decoded.ArtifactRef)
	}
	if len(decoded.SourceRefs) != 1 || decoded.SourceRefs[0].ID != "file_1" {
		t.Fatalf("expected source refs to round-trip, got %#v", decoded.SourceRefs)
	}
}
