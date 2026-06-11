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
		"Send a Google Chat message.",
		"Tin nhắn: Hello",
		"approve",
		"reject",
		"revise <nội dung muốn chỉnh>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected rendered approval to contain %q, got:\n%s", want, got)
		}
	}
	for _, notWant := range []string{
		"Tool:", "Risk:", "Approval ID:", "Input:", "spaces/AAAA",
	} {
		if strings.Contains(got, notWant) {
			t.Fatalf("expected rendered approval NOT to contain %q, got:\n%s", notWant, got)
		}
	}
}

// func TestRenderAgentResponseCleansAndLimitsMessage(t *testing.T) {
// 	longLine := strings.Repeat("x", maxOutboundTextRunes+20)
// 	response := contracts.AgentResponse{
// 		Status:  contracts.AgentStatusCompleted,
// 		Message: "\n\n### Kết quả\n\n\n" + longLine,
// 	}

// 	got := renderAgentResponse(response)
// 	if strings.Contains(got, "###") {
// 		t.Fatalf("expected heading markers to be removed, got %q", got)
// 	}
// 	if !strings.HasPrefix(got, "Kết quả") {
// 		t.Fatalf("expected cleaned message prefix, got %q", got[:20])
// 	}
// 	if !strings.Contains(got, "...[đã rút gọn]") {
// 		t.Fatalf("expected long message to be truncated, got length %d", len([]rune(got)))
// 	}
// }

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

func TestRenderAgentResponsePrefersFinalAssistantMessageOverDriveFallback(t *testing.T) {
	response := contracts.AgentResponse{
		Status:  contracts.AgentStatusCompleted,
		Message: "Dưới đây là các thư mục Google Drive bạn đang có:\n1. **Vclaw** - Link: [Mở thư mục](https://drive.google.com/drive/folders/folder_1)",
		ToolResults: []contracts.ToolResult{{
			ToolName: "drive.listFiles",
			Success:  true,
			Data: map[string]any{
				"contentForUser": `{"Files":[{"ID":"folder_1","Name":"Vclaw","MimeType":"application/vnd.google-apps.folder","WebViewLink":"https://drive.google.com/drive/folders/folder_1","ModifiedTime":"2026-06-11T10:00:00.000Z"}]}`,
			},
		}},
	}

	got := renderAgentResponse(response)
	if !strings.Contains(got, "Dưới đây là các thư mục Google Drive") {
		t.Fatalf("expected final assistant message to win, got %q", got)
	}
	if strings.HasPrefix(got, "Kết quả") {
		t.Fatalf("expected not to fall back to raw tool result, got %q", got)
	}
}

func TestRenderAgentResponseFormatsDriveFallbackWithLinks(t *testing.T) {
	response := contracts.AgentResponse{
		Status: contracts.AgentStatusCompleted,
		ToolResults: []contracts.ToolResult{{
			ToolName: "drive.listFiles",
			Success:  true,
			Data: map[string]any{
				"contentForUser": `{"Files":[{"ID":"folder_1","Name":"Vclaw","MimeType":"application/vnd.google-apps.folder","WebViewLink":"https://drive.google.com/drive/folders/folder_1","ModifiedTime":"2026-06-11T10:00:00.000Z"}]}`,
			},
		}},
	}

	got := renderAgentResponse(response)
	for _, want := range []string{"Danh sách Google Drive", "Vclaw", "https://drive.google.com/drive/folders/folder_1", "2026-06-11T10:00:00.000Z"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in rendered fallback, got %q", want, got)
		}
	}
	if strings.Contains(got, "Files: Vclaw") {
		t.Fatalf("expected Drive fallback not to collapse to Files: name-only output, got %q", got)
	}
}

func TestRenderAgentResponseFormatsRawGmailDraftJSON(t *testing.T) {
	response := contracts.AgentResponse{
		Status:  contracts.AgentStatusCompleted,
		Message: `{"Draft":{"ID":"r2926357301250964084","MessageID":"19e91b51f8b1fa70","ThreadID":"19e91b51f8b1fa70"}}`,
		ToolResults: []contracts.ToolResult{
			{
				ToolName: "gmail.createDraft",
				Success:  true,
				Data: map[string]any{
					"contentForUser": `{"Draft":{"ID":"r2926357301250964084","MessageID":"19e91b51f8b1fa70","ThreadID":"19e91b51f8b1fa70"}}`,
				},
			},
		},
	}

	got := renderAgentResponse(response)
	if strings.Contains(got, `{"Draft"`) {
		t.Fatalf("expected friendly draft output, got %q", got)
	}
	for _, want := range []string{"Đã tạo bản nháp email.", "Draft ID: r2926357301250964084", "Message ID: 19e91b51f8b1fa70"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in rendered output, got %q", want, got)
		}
	}
}

func TestRenderAgentResponseFormatsRawGmailSentMessageJSON(t *testing.T) {
	response := contracts.AgentResponse{
		Status:  contracts.AgentStatusCompleted,
		Message: `{"Message":{"ID":"msg_1","ThreadID":"thread_1","To":"baolnc@vclaw.site","Subject":"Test HITL"}}`,
		ToolResults: []contracts.ToolResult{
			{
				ToolName: "gmail.sendDraft",
				Success:  true,
				Data: map[string]any{
					"contentForUser": `{"Message":{"ID":"msg_1","ThreadID":"thread_1","To":"baolnc@vclaw.site","Subject":"Test HITL"}}`,
				},
			},
		},
	}

	got := renderAgentResponse(response)
	if strings.Contains(got, `{"Message"`) {
		t.Fatalf("expected friendly message output, got %q", got)
	}
	for _, want := range []string{"Email đã được chuyển cho Gmail để gửi.", "Message ID: msg_1", "Người nhận: baolnc@vclaw.site", "Chủ đề: Test HITL"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in rendered output, got %q", want, got)
		}
	}
}

func TestRenderAgentResponseSanitizesGmailSendDeliveryClaim(t *testing.T) {
	response := contracts.AgentResponse{
		Status:  contracts.AgentStatusCompleted,
		Message: "Email m\u1eddi tham d\u1ef1 cu\u1ed9c h\u1ecdp \u0111\u00e3 \u0111\u01b0\u1ee3c g\u1eedi th\u00e0nh c\u00f4ng \u0111\u1ebfn baolnc@gmail.com.",
		ToolResults: []contracts.ToolResult{
			{
				ToolName: "gmail.sendDraft",
				Success:  true,
				Data: map[string]any{
					"contentForUser": `{"Message":{"ID":"msg_1","ThreadID":"thread_1","To":"baolnc@gmail.com","Subject":"Test"}}`,
				},
			},
		},
	}

	got := renderAgentResponse(response)
	if strings.Contains(got, "\u0111\u00e3 \u0111\u01b0\u1ee3c g\u1eedi th\u00e0nh c\u00f4ng") {
		t.Fatalf("expected delivery success claim to be sanitized, got %q", got)
	}
	if !strings.Contains(got, "chuy\u1ec3n cho Gmail \u0111\u1ec3 g\u1eedi") {
		t.Fatalf("expected Gmail handoff wording, got %q", got)
	}
}

func TestRenderAgentResponseFormatsCalendarAndChatJSON(t *testing.T) {
	calendar := renderAgentResponse(contracts.AgentResponse{
		Status:  contracts.AgentStatusCompleted,
		Message: `Event created: {"ID":"evt_1","Title":"Sprint Review","StartTime":"2026-06-05T09:30:00+07:00","EndTime":"2026-06-05T10:30:00+07:00"}`,
	})
	if !strings.Contains(calendar, "Đã tạo sự kiện Calendar.") || strings.Contains(calendar, "{") {
		t.Fatalf("expected friendly calendar output, got %q", calendar)
	}

	chat := renderAgentResponse(contracts.AgentResponse{
		Status:  contracts.AgentStatusCompleted,
		Message: `{"Message":{"Name":"spaces/AAAA/messages/msg_1","Text":"Hello everyone"}}`,
		ToolResults: []contracts.ToolResult{{
			ToolName: "chat.sendMessage",
			Success:  true,
			Data:     map[string]any{"contentForUser": `{"Message":{"Name":"spaces/AAAA/messages/msg_1","Text":"Hello everyone"}}`},
		}},
	})
	if !strings.Contains(chat, "Đã gửi tin nhắn Google Chat.") || strings.Contains(chat, `{"Message"`) {
		t.Fatalf("expected friendly chat output, got %q", chat)
	}
}

// func TestRenderAgentResponseStripsInlineMarkdownMarkers(t *testing.T) {
// 	response := contracts.AgentResponse{
// 		Status: contracts.AgentStatusCompleted,
// 		Message: strings.Join([]string{
// 			"Here are your recent emails:",
// 			"",
// 			"- **From:** Google",
// 			"  **Subject:** Welcome",
// 			"  `Date:` 29 May 2026",
// 		}, "\n"),
// 	}

// 	got := renderAgentResponse(response)
// 	for _, marker := range []string{"**", "`"} {
// 		if strings.Contains(got, marker) {
// 			t.Fatalf("expected markdown marker %q to be removed, got %q", marker, got)
// 		}
// 	}
// 	for _, want := range []string{"From: Google", "Subject: Welcome", "Date: 29 May 2026"} {
// 		if !strings.Contains(got, want) {
// 			t.Fatalf("expected readable label %q, got %q", want, got)
// 		}
// 	}
// }

func TestRenderUserOutputCoversAcceptanceCases(t *testing.T) {
	t.Run("greeting", func(t *testing.T) {
		output := renderUserOutput(contracts.AgentResponse{
			Status:  contracts.AgentStatusCompleted,
			Message: "Chao ban! Toi co the giup gi cho ban hom nay?",
		})
		if output == nil || output.Kind != contracts.UserOutputKindSuccess {
			t.Fatalf("expected success output, got %#v", output)
		}
		if !strings.Contains(output.Text, "Chao ban!") {
			t.Fatalf("unexpected greeting text: %#v", output)
		}
	})

	t.Run("success_with_artifact", func(t *testing.T) {
		output := renderUserOutput(contracts.AgentResponse{
			Status: contracts.AgentStatusCompleted,
			ToolResults: []contracts.ToolResult{{
				ToolName: "gmail.sendDraft",
				Success:  true,
				Data: map[string]any{
					"contentForUser": `{"Message":{"ID":"msg_1","ThreadID":"thread_1","To":"baolnc@vclaw.site","Subject":"Test HITL"}}`,
				},
			}},
		})
		if output == nil || output.ArtifactRef == nil {
			t.Fatalf("expected artifact ref, got %#v", output)
		}
		if output.ArtifactRef.ID != "msg_1" || output.ArtifactRef.URI != "https://mail.google.com/mail/u/0/#sent/msg_1" {
			t.Fatalf("unexpected artifact ref: %#v", output.ArtifactRef)
		}
	})

	t.Run("calendar_artifact_accepts_json_tags", func(t *testing.T) {
		output := renderUserOutput(contracts.AgentResponse{
			Status: contracts.AgentStatusCompleted,
			ToolResults: []contracts.ToolResult{{
				ToolName: "calendar.createEvent",
				Success:  true,
				Data: map[string]any{
					"contentForUser": `{"Event":{"id":"event_1","summary":"Test HITL","meetLink":"https://meet.google.com/abc-defg-hij"}}`,
				},
			}},
		})
		if output == nil || output.ArtifactRef == nil {
			t.Fatalf("expected calendar artifact ref, got %#v", output)
		}
		if output.ArtifactRef.ID != "event_1" || output.ArtifactRef.URI != "https://calendar.google.com/calendar/r/eventedit/event_1" {
			t.Fatalf("unexpected calendar artifact ref: %#v", output.ArtifactRef)
		}
		if got := output.ArtifactRef.Meta["meetLink"]; got != "https://meet.google.com/abc-defg-hij" {
			t.Fatalf("expected meetLink meta, got %#v", got)
		}
	})

	t.Run("success_without_artifact", func(t *testing.T) {
		output := renderUserOutput(contracts.AgentResponse{
			Status:  contracts.AgentStatusCompleted,
			Message: "Da hoan thanh.",
		})
		if output == nil || output.ArtifactRef != nil {
			t.Fatalf("expected no artifact ref, got %#v", output)
		}
		if output.Kind != contracts.UserOutputKindSuccess {
			t.Fatalf("expected success kind, got %#v", output)
		}
	})

	t.Run("clarify", func(t *testing.T) {
		output := renderUserOutput(contracts.AgentResponse{
			Status:  contracts.AgentStatusNeedClarification,
			Message: "Ban co the noi ro hon khong?",
		})
		if output == nil || output.Kind != contracts.UserOutputKindClarify {
			t.Fatalf("expected clarify output, got %#v", output)
		}
	})

	t.Run("approval", func(t *testing.T) {
		output := renderUserOutput(contracts.AgentResponse{
			Status: contracts.AgentStatusApprovalRequired,
			ApprovalRequest: &contracts.ApprovalRequest{
				ApprovalID: "appr_1",
				Status:     contracts.ApprovalStatusPending,
				RiskLevel:  contracts.RiskLevelExternalWrite,
				Summary:    "Can xac nhan.",
				ToolCall: contracts.ToolCall{
					ToolName: "chat.sendMessage",
				},
				ExpiresAt: time.Date(2026, 6, 5, 10, 22, 53, 0, time.FixedZone("ICT", 7*60*60)),
			},
		})
		if output == nil || output.Kind != contracts.UserOutputKindApproval {
			t.Fatalf("expected approval output, got %#v", output)
		}
		if got := output.Meta["approvalId"]; got != "appr_1" {
			t.Fatalf("expected approvalId meta, got %#v", got)
		}
	})

	t.Run("expired", func(t *testing.T) {
		output := renderUserOutput(contracts.AgentResponse{
			Status: contracts.AgentStatusFailed,
			Error: &contracts.ErrorShape{
				Code:      "APPROVAL_EXPIRED",
				Message:   "approval expired",
				Source:    contracts.ErrorSourceAgent,
				Retryable: false,
			},
		})
		if output == nil || output.Kind != contracts.UserOutputKindExpired {
			t.Fatalf("expected expired output, got %#v", output)
		}
	})
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

func TestParseApprovalCommandIgnoresNaturalEditRequestWithoutPendingApproval(t *testing.T) {
	for _, text := range []string{
		"Chỉnh lại lịch Test HITL vào ngày mai thành 9h30 - 10h30",
		"sửa lịch Test HITL thành 9h30",
		"revise thêm người tham gia",
	} {
		if command, ok := parseApprovalCommand(text, false); ok {
			t.Fatalf("expected %q without pending approval to be ignored, got %#v", text, command)
		}
	}
}

func TestParseApprovalCommandRejectsWithRevisionComment(t *testing.T) {
	command, ok := parseApprovalCommand("revise đổi giờ sang 10:00", true)
	if !ok {
		t.Fatal("expected revise command")
	}
	if command.decision != contracts.ApprovalDecisionRevised {
		t.Fatalf("expected revised, got %s", command.decision)
	}
	if command.comment != "đổi giờ sang 10:00" {
		t.Fatalf("unexpected comment: %q", command.comment)
	}
}

func TestGmailBounceHelpers(t *testing.T) {
	sent, ok := sentGmailMessageFromContent(`{"Message":{"ID":"msg_1","To":"Obama <obama@gmail.com>, alice@example.com","Subject":"Hello","Date":"Mon, 8 Jun 2026 20:10:58 -0700"}}`)
	if !ok {
		t.Fatal("expected sent Gmail message to parse")
	}
	if sent.MessageID != "msg_1" || sent.Subject != "Hello" {
		t.Fatalf("unexpected sent message: %#v", sent)
	}
	if sent.SentAt.IsZero() || sent.SentAt.Format(time.RFC3339) != "2026-06-09T03:10:58Z" {
		t.Fatalf("expected Gmail Date to be parsed, got %s", sent.SentAt.Format(time.RFC3339))
	}
	if len(sent.To) != 2 || sent.To[0] != "obama@gmail.com" || sent.To[1] != "alice@example.com" {
		t.Fatalf("unexpected recipients: %#v", sent.To)
	}

	bounceText := "Address not found. Your message wasn't delivered to obama@gmail.com because the address couldn't be found, or is unable to receive mail."
	if !gmailBounceMentionsRecipient(bounceText, "obama@gmail.com") {
		t.Fatal("expected bounce text to match recipient")
	}
	if gmailBounceMentionsRecipient(bounceText, "alice@example.com") {
		t.Fatal("did not expect unrelated recipient to match")
	}

	candidates := gmailBounceCandidates(`{"Messages":[{"ID":"bounce_1","Snippet":"Address not found","InternalDate":1780974657000}]}`, sent.SentAt)
	if len(candidates) != 1 || candidates[0].MessageID != "bounce_1" {
		t.Fatalf("expected nearby bounce candidate to survive skew filter, got %#v", candidates)
	}
}
