package slack

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/slack-go/slack"

	"vclaw/internal/agent"
	"vclaw/internal/contracts"
)

type fakeSlackHandler struct {
	calls     int
	received  contracts.UserMessage
	outbound  contracts.AgentResponse
	handleErr error
}

func (f *fakeSlackHandler) HandleMessage(_ context.Context, msg contracts.UserMessage) (contracts.AgentResponse, error) {
	f.calls++
	f.received = msg
	return f.outbound, f.handleErr
}

func (f *fakeSlackHandler) FinalizeAudit(_ contracts.UserMessage, _ error) {}

func (f *fakeSlackHandler) RecordIgnored(_ contracts.UserMessage, _ string) {}

func TestHandleSlackMessageIgnoresBotOwnMessages(t *testing.T) {
	bot := &Bot{botUserID: "U123"}

	if err := bot.handleSlackMessage(context.Background(), "C123", "U123", "hello", "123.45", "", "im"); err != nil {
		t.Fatalf("handleSlackMessage() returned error: %v", err)
	}
}

func TestIsAllowedRequiresSingleOwner(t *testing.T) {
	bot := &Bot{ownerUserID: "U1"}

	if !bot.isAllowed("C1", "U1") {
		t.Fatal("expected owner to be allowed")
	}
	if bot.isAllowed("C1", "U2") {
		t.Fatal("expected non-owner to be blocked")
	}
}

func TestIsAllowedRestrictsChannelsWhenConfigured(t *testing.T) {
	bot := &Bot{
		ownerUserID:     "U1",
		allowedChannels: makeAllowSet([]string{"C1"}),
	}

	if !bot.isAllowed("C1", "U1") {
		t.Fatal("expected owner in allowed channel to be allowed")
	}
	if bot.isAllowed("C2", "U1") {
		t.Fatal("expected owner in unlisted channel to be blocked")
	}
}

func TestSlackProgressTextMapsKnownTools(t *testing.T) {
	text := slackProgressText(agent.ProgressEvent{
		Stage:    agent.ProgressStageToolStarted,
		ToolName: "gmail.listEmails",
	})
	if !strings.Contains(text, "Gmail") {
		t.Fatalf("unexpected progress text: %q", text)
	}
}

func TestSlackProgressTextHidesInternalRoutingStages(t *testing.T) {
	for _, stage := range []agent.ProgressStage{
		agent.ProgressStageClassifying,
		agent.ProgressStageClassified,
		agent.ProgressStagePlanning,
		agent.ProgressStagePlanned,
		agent.ProgressStageThinking,
		agent.ProgressStageFinalizing,
	} {
		if got := slackProgressText(agent.ProgressEvent{Stage: stage}); got != "" {
			t.Fatalf("expected stage %s to be hidden, got %q", stage, got)
		}
	}
}

func TestSlackTextFromFailedResponseHidesDetails(t *testing.T) {
	text := slackTextFromResponse(contracts.AgentResponse{
		Status:  contracts.AgentStatusFailed,
		Message: "provider chat failed: secret stack trace",
	})
	if strings.Contains(text, "provider chat failed") {
		t.Fatalf("failed response leaked detail: %q", text)
	}
	if text == "" {
		t.Fatal("expected generic error text")
	}
}

func TestSlackTextShowsFriendlyCancelMessage(t *testing.T) {
	text := slackTextFromResponse(contracts.AgentResponse{
		Status:  contracts.AgentStatusBlocked,
		Message: "Đã hủy thao tác. Tôi chưa thực hiện tool nào.",
		Error: &contracts.ErrorShape{
			Message: "approval rejected",
		},
	})
	if !strings.Contains(text, "Đã hủy theo yêu cầu") {
		t.Fatalf("expected friendly cancel text, got %q", text)
	}
}

func TestSlackApprovalValueRoundTrip(t *testing.T) {
	value := slackApprovalValue("approve", "appr_123", "slack_channel_C1")
	action, approvalID, sessionID, ok := parseSlackApprovalValue(value)
	if !ok {
		t.Fatalf("expected approval value to parse: %q", value)
	}
	if action != "approve" || approvalID != "appr_123" || sessionID != "slack_channel_C1" {
		t.Fatalf("unexpected parse result action=%q approvalID=%q sessionID=%q", action, approvalID, sessionID)
	}
}

func TestSlackApprovalBlocksContainMultipleChoiceButtons(t *testing.T) {
	blocks := slackApprovalBlocks("Approval required", "appr_123", "sess_1")
	if len(blocks) != 2 {
		t.Fatalf("expected section and actions blocks, got %#v", blocks)
	}
	actionBlock, ok := blocks[1].(*slack.ActionBlock)
	if !ok {
		t.Fatalf("expected action block, got %T", blocks[1])
	}
	if len(actionBlock.Elements.ElementSet) != 2 {
		t.Fatalf("expected two approval buttons, got %#v", actionBlock.Elements.ElementSet)
	}
	labels := []string{}
	for _, element := range actionBlock.Elements.ElementSet {
		button, ok := element.(*slack.ButtonBlockElement)
		if !ok {
			t.Fatalf("expected button element, got %T", element)
		}
		labels = append(labels, button.Text.Text)
	}
	for _, want := range []string{"Xác nhận", "Hủy"} {
		if !containsString(labels, want) {
			t.Fatalf("expected labels to contain %q, got %#v", want, labels)
		}
	}
}

func TestSlackApprovalTextOmitsTechnicalFields(t *testing.T) {
	text := slackTextFromResponse(contracts.AgentResponse{
		Status: contracts.AgentStatusApprovalRequired,
		ApprovalRequest: &contracts.ApprovalRequest{
			ApprovalID: "appr_1",
			Summary:    "Tôi cần bạn xác nhận trước khi gửi email.",
			RiskLevel:  contracts.RiskLevelExternalWrite,
			ToolCall: contracts.ToolCall{
				ToolName: "gmail.sendDraft",
				Input: map[string]any{
					"draftId": "draft-1",
				},
			},
		},
	})

	for _, forbidden := range []string{"Approval ID", "Input:", "draft-1", "Risk:", "Tool:"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("slack approval text leaked %q: %q", forbidden, text)
		}
	}
	if !strings.Contains(text, "Hành động: Gửi email") {
		t.Fatalf("expected human-friendly approval action, got %q", text)
	}
}

func TestSlackApprovalTextShowsSandboxPythonCode(t *testing.T) {
	text := slackTextFromResponse(contracts.AgentResponse{
		Status: contracts.AgentStatusApprovalRequired,
		ApprovalRequest: &contracts.ApprovalRequest{
			ApprovalID: "appr_py",
			Summary:    "Tôi cần bạn xác nhận trước khi chạy code trong sandbox.",
			ToolCall: contracts.ToolCall{
				ToolName: "sandbox.runPython",
				Input: map[string]any{
					"code": "print('hello')\nprint('world')",
				},
			},
		},
	})

	if !strings.Contains(text, "Mã Python sẽ chạy:") {
		t.Fatalf("expected sandbox code heading, got %q", text)
	}
	if !strings.Contains(text, "print('hello')") || !strings.Contains(text, "print('world')") {
		t.Fatalf("expected full sandbox code in approval text, got %q", text)
	}
}

func TestSlackApprovalTextShowsEmailDraftDetails(t *testing.T) {
	text := slackTextFromResponse(contracts.AgentResponse{
		Status: contracts.AgentStatusApprovalRequired,
		ApprovalRequest: &contracts.ApprovalRequest{
			ApprovalID: "appr_mail",
			Summary:    "Tôi cần bạn xác nhận trước khi tạo Gmail draft.",
			ToolCall: contracts.ToolCall{
				ToolName: "gmail.createDraft",
				Input: map[string]any{
					"to":       []any{"vmkqa2@gmail.com"},
					"subject":  "Mời họp chiều nay",
					"textBody": "Chào bạn,\nMời bạn tham dự cuộc họp chiều nay.",
				},
			},
		},
	})

	for _, want := range []string{"Người nhận:", "vmkqa2@gmail.com", "Tiêu đề:", "Mời họp chiều nay", "Nội dung email:", "Mời bạn tham dự cuộc họp chiều nay."} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected email approval text to contain %q, got %q", want, text)
		}
	}
}

func TestSlackApprovalTextUsesGenericFallbackForUnknownTool(t *testing.T) {
	text := slackTextFromResponse(contracts.AgentResponse{
		Status: contracts.AgentStatusApprovalRequired,
		ApprovalRequest: &contracts.ApprovalRequest{
			ApprovalID: "appr_generic",
			Summary:    "Cần bạn xác nhận trước khi tạo task.",
			ToolCall: contracts.ToolCall{
				ToolName: "tasks.createTask",
				Input: map[string]any{
					"title":       "Chuẩn bị báo cáo tuần",
					"description": "Tổng hợp số liệu bán hàng",
					"dueDate":     "2026-06-09",
					"taskId":      "task-123",
				},
			},
		},
	})

	for _, want := range []string{"Tiêu đề: Chuẩn bị báo cáo tuần", "Nội dung:", "Tổng hợp số liệu bán hàng", "Kết thúc: 2026-06-09"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected generic approval text to contain %q, got %q", want, text)
		}
	}
	if strings.Contains(text, "task-123") {
		t.Fatalf("expected generic fallback to avoid leaking raw ids, got %q", text)
	}
}

func TestHandleSlackInteractionRejectsMismatchedApprovalContext(t *testing.T) {
	handler := &fakeSlackHandler{}
	bot := &Bot{
		orchestrator: handler,
		ownerUserID:  "U1",
		state:        newSlackChannelState(),
	}
	bot.state.registerApproval(slackApprovalContext{
		ApprovalID: "appr_123",
		SessionID:  "slack_channel_C1",
		ChannelID:  "C1",
		MessageTS:  "123.45",
	})

	callback := slack.InteractionCallback{
		Type: slack.InteractionTypeBlockActions,
		User: slack.User{ID: "U1"},
		Container: slack.Container{
			ChannelID: "C1",
			MessageTs: "999.00",
		},
		ActionCallback: slack.ActionCallbacks{
			BlockActions: []*slack.BlockAction{
				{
					Value: slackApprovalValue("approve", "appr_123", "slack_channel_C1"),
				},
			},
		},
	}

	if err := bot.handleSlackInteraction(context.Background(), callback); err != nil {
		t.Fatalf("handleSlackInteraction() error = %v", err)
	}
	if handler.calls != 0 {
		t.Fatalf("mismatched interaction should not call handler, got %d calls", handler.calls)
	}
}

func TestSlackRevisionPromptIncludesPendingContext(t *testing.T) {
	prompt := slackRevisionPrompt(slackApprovalContext{
		ToolName:   "sandbox.runPython",
		PromptText: "Hành động: Chạy mã Python trong sandbox\n\nMã Python sẽ chạy:\n\nprint('hello')",
	})

	if !strings.Contains(prompt, "Nội dung đang chờ xác nhận") {
		t.Fatalf("expected prompt to include pending context, got %q", prompt)
	}
	if !strings.Contains(prompt, "print('hello')") {
		t.Fatalf("expected prompt to include pending code, got %q", prompt)
	}
}

func TestSlackReviseCommentReadsModalState(t *testing.T) {
	var callback slack.InteractionCallback
	raw := `{
		"type":"view_submission",
		"view":{
			"state":{
				"values":{
					"vclaw_approval_comment":{
						"comment":{"type":"plain_text_input","value":"đổi giờ sang 10:00"}
					}
				}
			}
		}
	}`
	if err := json.Unmarshal([]byte(raw), &callback); err != nil {
		t.Fatalf("unmarshal callback: %v", err)
	}
	if got := slackReviseComment(callback); got != "đổi giờ sang 10:00" {
		t.Fatalf("unexpected revise comment: %q", got)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
