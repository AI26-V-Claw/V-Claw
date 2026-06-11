package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"vclaw/internal/agent"
	"vclaw/internal/contracts"
	"vclaw/internal/policies"
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

func TestSlackTextFromBlockedByPolicyResponseShowsPolicyMessage(t *testing.T) {
	text := slackTextFromResponse(contracts.AgentResponse{
		Status: contracts.AgentStatusFailed,
		Error: &contracts.ErrorShape{
			Code:      contracts.ErrorActionBlockedByPolicy,
			Message:   "tool blocked by policy: gmail.trashMessage",
			Source:    contracts.ErrorSourcePolicy,
			Retryable: false,
		},
	})
	if text != "Hành động này không được phép thực hiện do chính sách bảo mật hiện tại." {
		t.Fatalf("unexpected policy block text: %q", text)
	}
}

func TestSlackTextFromApprovalExpiredResponseShowsExpiredMessage(t *testing.T) {
	text := slackTextFromResponse(contracts.AgentResponse{
		Status: contracts.AgentStatusFailed,
		Error: &contracts.ErrorShape{
			Code:      contracts.ErrorApprovalExpired,
			Message:   "approval expired",
			Source:    contracts.ErrorSourcePolicy,
			Retryable: false,
		},
	})
	if text != "Yêu cầu xác nhận đã hết hạn. Vui lòng thử lại." {
		t.Fatalf("unexpected approval expired text: %q", text)
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
	for _, forbidden := range []string{"Cần bạn xác nhận trước khi thực hiện.", "Hành động:"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("slack approval text should omit %q: %q", forbidden, text)
		}
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
	if !strings.Contains(text, "```") {
		t.Fatalf("expected sandbox code block markup, got %q", text)
	}
}

func TestSlackTextFromResponsePreservesMultilineFormatting(t *testing.T) {
	message := "Đây là một bộ khung mục lục:\n\nCHƯƠNG 2\n2.1 Cơ sở lý thuyết\n  2.1.1 Khái niệm hệ thống thông tin\n  2.1.2 Khái niệm cơ sở dữ liệu\n\n- Mục 1\n- Mục 2"

	text := slackTextFromResponse(contracts.AgentResponse{
		Status:  contracts.AgentStatusCompleted,
		Message: message,
	})

	if text != message {
		t.Fatalf("expected response formatting to be preserved, got %q want %q", text, message)
	}
}

func TestSlackApprovalTextShowsEmailDraftDetails(t *testing.T) {
	body := "Chào bạn,\n\nMời bạn tham dự cuộc họp chiều nay.\n\nThân mến,\nV-Claw"
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
					"textBody": body,
				},
			},
		},
	})

	for _, want := range []string{"*Người nhận:*", "`vmkqa2@gmail.com`", "*Tiêu đề:*", "`Mời họp chiều nay`", "Mời bạn tham dự cuộc họp chiều nay.", "Thân mến,", "```"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected email approval text to contain %q, got %q", want, text)
		}
	}
	if strings.Contains(text, "Nội dung email:") {
		t.Fatalf("expected email approval text to omit body label, got %q", text)
	}
}

func TestSlackApprovalTextShowsCalendarEventDetails(t *testing.T) {
	text := slackTextFromResponse(contracts.AgentResponse{
		Status: contracts.AgentStatusApprovalRequired,
		ApprovalRequest: &contracts.ApprovalRequest{
			ApprovalID: "appr_calendar",
			Summary:    "Tôi cần bạn xác nhận trước khi tạo sự kiện Calendar.",
			ToolCall: contracts.ToolCall{
				ToolName: "calendar.createEvent",
				Input: map[string]any{
					"title":       "Họp",
					"start":       "2026-06-10T08:00:00+07:00",
					"end":         "2026-06-10T09:30:00+07:00",
					"attendees":   []any{"a@test.com", "b@test.com"},
					"location":    "Phòng A",
					"description": "Chuẩn bị số liệu bán hàng.",
				},
			},
		},
	})

	for _, want := range []string{
		"*Tiêu đề:* Họp",
		"*Bắt đầu:* 10/06/2026, 08:00 (+07:00)",
		"*Kết thúc:* 10/06/2026, 09:30 (+07:00)",
		"*Thời lượng:* 1 giờ 30 phút",
		"*Người tham gia:* a@test.com, b@test.com",
		"*Địa điểm:* Phòng A",
		"Ghi chú:", "Chuẩn bị số liệu bán hàng.", "```",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected calendar approval text to contain %q, got %q", want, text)
		}
	}
	for _, forbidden := range []string{"Start: 2026-06-10T08:00:00+07:00", "End: 2026-06-10T09:30:00+07:00"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("expected calendar approval text to avoid raw time field %q, got %q", forbidden, text)
		}
	}
}

func TestSlackApprovalTextShowsChatMessageDetails(t *testing.T) {
	text := slackTextFromResponse(contracts.AgentResponse{
		Status: contracts.AgentStatusApprovalRequired,
		ApprovalRequest: &contracts.ApprovalRequest{
			ApprovalID: "appr_chat",
			Summary:    "Tôi cần bạn xác nhận trước khi gửi tin nhắn Google Chat.",
			ToolCall: contracts.ToolCall{
				ToolName: "chat.sendMessage",
				Input: map[string]any{
					"space": "spaces/87bFdyAAAAE",
					"text":  "Mọi người vui lòng tăng ca đến 10h đêm nay nhé. Cảm ơn mọi người.",
				},
			},
		},
	})

	for _, want := range []string{"Mọi người vui lòng tăng ca đến 10h đêm nay nhé. Cảm ơn mọi người.", "```"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected chat approval text to contain %q, got %q", want, text)
		}
	}
	if strings.Contains(text, "Nội dung:") || strings.Contains(text, "Space:") || strings.Contains(text, "spaces/87bFdyAAAAE") {
		t.Fatalf("expected chat approval text to omit raw space identifier, got %q", text)
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

func TestSlackRenderTextConvertsRawFencedCodeBlock(t *testing.T) {
	rendered := slackRenderText("Vi du:\n```python\nif True:\n    print('hello')\n```")

	if strings.Contains(rendered, "```python") {
		t.Fatalf("expected language marker to be normalized away, got %q", rendered)
	}
	if !strings.Contains(rendered, "```\nif True:\n    print('hello')\n```") {
		t.Fatalf("expected fenced code block to be preserved as a Slack code block, got %q", rendered)
	}
}

func TestSlackRenderTextFormatsMarkdownHeading(t *testing.T) {
	rendered := slackRenderText("## Giới thiệu")

	if rendered != "*GIỚI THIỆU*" {
		t.Fatalf("expected markdown heading to render as bold uppercase, got %q", rendered)
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

func TestSlackPolicySettingsActionValueRoundTrip(t *testing.T) {
	raw := slackPolicySettingsActionValue(slackPolicySettingsOpenAction)
	action, ok := parseSlackPolicySettingsActionValue(raw)
	if !ok {
		t.Fatalf("expected action value to parse: %q", raw)
	}
	if action != slackPolicySettingsOpenAction {
		t.Fatalf("unexpected action: %q", action)
	}
}

func TestSlackPolicySettingsTriggerTextMatchesStandaloneKeywords(t *testing.T) {
	for _, input := range []string{
		"settings",
		"cài đặt",
		"policy",
		"please settings",
		"nhờ mở cài đặt giúp",
		"mở policy nhé",
		"(settings)",
	} {
		if !isSlackPolicySettingsTriggerText(input) {
			t.Fatalf("expected %q to match policy settings trigger", input)
		}
	}
	for _, input := range []string{
		"settings-based",
		"policy-based",
		"deployment policying",
		"cài_đặt",
		"mysettings",
	} {
		if isSlackPolicySettingsTriggerText(input) {
			t.Fatalf("expected %q to not match policy settings trigger", input)
		}
	}
}

func TestSlackPolicySettingsTriggerMessagePostsSingleButtonPrompt(t *testing.T) {
	var requestBody struct {
		Channel  string           `json:"channel"`
		Text     string           `json:"text"`
		ThreadTS string           `json:"thread_ts"`
		Blocks   []map[string]any `json:"blocks"`
	}

	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/chat.postMessage" {
				t.Fatalf("unexpected slack api path: %s", r.URL.Path)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read request body: %v", err)
			}
			values, err := url.ParseQuery(string(body))
			if err != nil {
				t.Fatalf("parse request body: %v", err)
			}
			requestBody.Channel = values.Get("channel")
			requestBody.Text = values.Get("text")
			requestBody.ThreadTS = values.Get("thread_ts")
			if err := json.Unmarshal([]byte(values.Get("blocks")), &requestBody.Blocks); err != nil {
				t.Fatalf("decode blocks: %v", err)
			}
			return jsonResponse(http.StatusOK, `{"ok":true,"channel":"C123","ts":"123.45"}`), nil
		}),
	}

	bot := &Bot{
		api:         slack.New("test-token", slack.OptionAPIURL("http://slack.local/"), slack.OptionHTTPClient(client)),
		ownerUserID: "U123",
		botUserID:   "B123",
	}

	err := bot.handleEvent(context.Background(), socketmode.Event{
		Type: socketmode.EventTypeEventsAPI,
		Data: slackevents.EventsAPIEvent{
			Type: slackevents.CallbackEvent,
			InnerEvent: slackevents.EventsAPIInnerEvent{
				Type: "message",
				Data: &slackevents.MessageEvent{
					User:            "U123",
					Text:            "policy",
					TimeStamp:       "123.45",
					ThreadTimeStamp: "987.65",
					Channel:         "C123",
					ChannelType:     "channel",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("handleEvent() error = %v", err)
	}
	if requestBody.Channel != "C123" {
		t.Fatalf("unexpected channel: %q", requestBody.Channel)
	}
	if requestBody.ThreadTS != "987.65" {
		t.Fatalf("unexpected thread ts: %q", requestBody.ThreadTS)
	}
	if len(requestBody.Blocks) != 2 {
		t.Fatalf("expected two blocks, got %d", len(requestBody.Blocks))
	}
	actionBlock, ok := requestBody.Blocks[1]["elements"].([]any)
	if !ok {
		t.Fatalf("expected action elements, got %#v", requestBody.Blocks[1]["elements"])
	}
	if len(actionBlock) != 1 {
		t.Fatalf("expected one button, got %d", len(actionBlock))
	}
	button, ok := actionBlock[0].(map[string]any)
	if !ok {
		t.Fatalf("expected button object, got %#v", actionBlock[0])
	}
	text, ok := button["text"].(map[string]any)
	if !ok {
		t.Fatalf("expected button text object, got %#v", button["text"])
	}
	if got := text["text"]; got != "⚙️ Cài đặt chính sách" {
		t.Fatalf("unexpected button label: %v", got)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func jsonResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestSlackPolicySettingsModalBlocksContainAllRiskLevels(t *testing.T) {
	blocks := slackPolicySettingsModalBlocks(policies.EffectivePolicyConfig(policies.UserPolicyConfig{}))
	if len(blocks) != 8 {
		t.Fatalf("expected section plus seven input blocks, got %d", len(blocks))
	}
	seen := map[string]bool{}
	for _, block := range blocks[1:] {
		input, ok := block.(*slack.InputBlock)
		if !ok {
			t.Fatalf("expected input block, got %T", block)
		}
		seen[input.BlockID] = true
	}
	for _, level := range []string{
		"vclaw_policy_safe_read",
		"vclaw_policy_safe_compute",
		"vclaw_policy_sensitive_read",
		"vclaw_policy_external_write",
		"vclaw_policy_local_write",
		"vclaw_policy_code_execution",
		"vclaw_policy_destructive",
	} {
		if !seen[level] {
			t.Fatalf("missing policy block %q", level)
		}
	}
	for _, block := range blocks[1:] {
		input := block.(*slack.InputBlock)
		if strings.Contains(input.Label.Text, "safe_") || strings.Contains(input.Label.Text, "auto_") {
			t.Fatalf("expected translated label, got %q", input.Label.Text)
		}
	}
	for _, want := range []string{
		"Đọc email, lịch họp, tin nhắn",
		"Tóm tắt nội dung, dịch văn bản",
		"Mở và đọc chi tiết email, tài liệu",
		"Gửi email, đặt lịch họp, nhắn tin",
		"Tải file đính kèm, lưu tài liệu",
		"Thực thi script hoặc lệnh hệ thống",
		"Xóa email, file, lịch họp",
	} {
		found := false
		for _, block := range blocks[1:] {
			input := block.(*slack.InputBlock)
			if input.Label.Text == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing translated label %q", want)
		}
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
