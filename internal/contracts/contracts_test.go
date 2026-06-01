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
