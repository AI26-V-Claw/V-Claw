package agent

import (
	"strings"
	"testing"
	"time"

	"vclaw/internal/contracts"
)

func TestRenderAgentResponseFormatsApprovalForChat(t *testing.T) {
	response := contracts.AgentResponse{
		Status: contracts.AgentStatusApprovalRequired,
		ApprovalRequest: &contracts.ApprovalRequest{
			ApprovalID: "appr_1",
			RequestID:  "req_1",
			SessionID:  "sess_1",
			ToolCallID: "tool_1",
			Status:     contracts.ApprovalStatusPending,
			RiskLevel:  contracts.RiskLevelExternalWrite,
			Summary:    "Send a Google Chat message.",
			Details:    "This changes external Google Chat data.",
			ToolCall: contracts.ToolCall{
				ToolName: "chat.sendMessage",
				Input: map[string]any{
					"space": "spaces/AAAA",
					"text":  "Hello",
				},
			},
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(time.Minute),
		},
	}

	got := renderAgentResponse(response)
	for _, want := range []string{
		"Cần xác nhận trước khi thực hiện.",
		"Tóm tắt: Send a Google Chat message.",
		"Tool: chat.sendMessage",
		"Risk: external_write",
		"Approval ID: appr_1",
		"approve",
		"reject",
		"revise <nội dung muốn chỉnh>",
		"Input:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected rendered approval to contain %q, got:\n%s", want, got)
		}
	}
}

func TestRenderAgentResponseCleansAndLimitsMessage(t *testing.T) {
	longLine := strings.Repeat("x", maxOutboundTextRunes+20)
	response := contracts.AgentResponse{
		Status:  contracts.AgentStatusCompleted,
		Message: "\n\n### Kết quả\n\n\n" + longLine,
	}

	got := renderAgentResponse(response)
	if strings.Contains(got, "###") {
		t.Fatalf("expected heading markers to be removed, got %q", got)
	}
	if !strings.HasPrefix(got, "Kết quả") {
		t.Fatalf("expected cleaned message prefix, got %q", got[:20])
	}
	if !strings.Contains(got, "...[đã rút gọn]") {
		t.Fatalf("expected long message to be truncated, got length %d", len([]rune(got)))
	}
}

func TestRenderAgentResponseFormatsToolFallback(t *testing.T) {
	response := contracts.AgentResponse{
		Status: contracts.AgentStatusCompleted,
		ToolResults: []contracts.ToolResult{
			{
				ToolName: "gmail.listEmails",
				Success:  true,
				Data: map[string]any{
					"contentForUser": "\n{\"messages\":[]}\n",
				},
			},
		},
	}

	got := renderAgentResponse(response)
	if !strings.HasPrefix(got, "Kết quả từ gmail.listEmails") {
		t.Fatalf("expected tool fallback title, got %q", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Fatalf("expected compact spacing, got %q", got)
	}
}

func TestRenderAgentResponseStripsInlineMarkdownMarkers(t *testing.T) {
	response := contracts.AgentResponse{
		Status: contracts.AgentStatusCompleted,
		Message: strings.Join([]string{
			"Here are your recent emails:",
			"",
			"- **From:** Google",
			"  **Subject:** Welcome",
			"  `Date:` 29 May 2026",
		}, "\n"),
	}

	got := renderAgentResponse(response)
	for _, marker := range []string{"**", "`"} {
		if strings.Contains(got, marker) {
			t.Fatalf("expected markdown marker %q to be removed, got %q", marker, got)
		}
	}
	for _, want := range []string{"From: Google", "Subject: Welcome", "Date: 29 May 2026"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected readable label %q, got %q", want, got)
		}
	}
}

func TestParseApprovalCommandApprovesPendingRequest(t *testing.T) {
	command, ok := parseApprovalCommand("đồng ý", true)
	if !ok {
		t.Fatal("expected approval command")
	}
	if command.decision != contracts.ApprovalDecisionApproved {
		t.Fatalf("expected approved, got %s", command.decision)
	}
}

func TestParseApprovalCommandIgnoresNaturalAckWithoutPendingRequest(t *testing.T) {
	if command, ok := parseApprovalCommand("ok", false); ok {
		t.Fatalf("expected natural ack without pending approval to be ignored, got %#v", command)
	}
}

func TestParseApprovalCommandRejectsWithRevisionComment(t *testing.T) {
	command, ok := parseApprovalCommand("revise đổi giờ sang 10:00", true)
	if !ok {
		t.Fatal("expected revise command")
	}
	if command.decision != contracts.ApprovalDecisionRejected {
		t.Fatalf("expected rejected decision, got %s", command.decision)
	}
	if command.comment != "đổi giờ sang 10:00" {
		t.Fatalf("unexpected comment: %q", command.comment)
	}
}
