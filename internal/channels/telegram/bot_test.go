package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"vclaw/internal/agent"
	"vclaw/internal/contracts"
)

type fakeHandler struct {
	calls        int
	ignored      int
	finalized    int
	received     contracts.UserMessage
	outbound     contracts.AgentResponse
	progress     []agent.ProgressEvent
	handleErr    error
	finalizedErr error
}

func TestTelegramApproveFlowKeepsOriginalApprovalMessage(t *testing.T) {
	handler := &fakeHandler{
		outbound: contracts.AgentResponse{
			Status:  contracts.AgentStatusCompleted,
			Message: "ÄÃ£ cháº¡y xong.",
		},
	}

	type telegramCall struct {
		path    string
		payload map[string]any
	}

	var calls []telegramCall
	bot := New("token", 123, t.TempDir(), handler, nil)
	bot.state.registerApproval(telegramApprovalContext{
		ApprovalID: "appr_py",
		SessionID:  "telegram_chat_55",
		ChatID:     55,
		MessageID:  42,
		ToolName:   "sandbox.runPython",
		PromptText: "HÃ nh Ä‘á»™ng: Cháº¡y mÃ£ Python trong sandbox\n\nMÃ£ Python sáº½ cháº¡y:\n\nprint('hello')",
	})
	bot.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
		}
		calls = append(calls, telegramCall{path: r.URL.Path, payload: payload})
		switch {
		case strings.HasSuffix(r.URL.Path, "/answerCallbackQuery"):
			return jsonResponse(http.StatusOK, `{"ok":true}`), nil
		case strings.HasSuffix(r.URL.Path, "/editMessageReplyMarkup"):
			return jsonResponse(http.StatusOK, `{"ok":true}`), nil
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":88}}`), nil
		case strings.HasSuffix(r.URL.Path, "/editMessageText"):
			return jsonResponse(http.StatusOK, `{"ok":true}`), nil
		default:
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
			return nil, nil
		}
	})}

	processed, err := bot.processCallbackQuery(context.Background(), telegramUpdate{
		UpdateID: 12,
		CallbackQuery: &telegramCallbackQuery{
			ID:   "cb3",
			From: &telegramUser{ID: 123},
			Data: telegramApprovalCallbackData("approve", "appr_py"),
			Message: &telegramMessage{
				MessageID: 42,
				Chat:      telegramChat{ID: 55},
			},
		},
	})
	if err != nil {
		t.Fatalf("processCallbackQuery() error = %v", err)
	}
	if !processed {
		t.Fatal("expected callback to be processed")
	}
	if handler.received.Text != "approve appr_py" {
		t.Fatalf("unexpected approval command: %q", handler.received.Text)
	}
	if len(calls) < 4 {
		t.Fatalf("expected keyboard dismissal, callback answer, a new processing message, and a final update, got %#v", calls)
	}
	if !strings.HasSuffix(calls[0].path, "/editMessageReplyMarkup") {
		t.Fatalf("expected approval keyboard to be dismissed first, got %#v", calls)
	}
	if got := fmt.Sprint(calls[0].payload["message_id"]); got != "42" {
		t.Fatalf("expected keyboard dismissal to target the approval message, got message_id=%q payload=%#v", got, calls[0].payload)
	}
	if !strings.HasSuffix(calls[2].path, "/sendMessage") {
		t.Fatalf("expected a new processing message after approval, got %#v", calls)
	}
	if got := fmt.Sprint(calls[2].payload["text"]); strings.TrimSpace(got) == "" {
		t.Fatalf("expected processing message text, got %q", got)
	}
	last := calls[len(calls)-1]
	if !strings.HasSuffix(last.path, "/editMessageText") {
		t.Fatalf("expected final result to update the new processing message, got %#v", last)
	}
	if got := fmt.Sprint(last.payload["message_id"]); got != "88" {
		t.Fatalf("expected final edit to target the new processing message, got message_id=%q payload=%#v", got, last.payload)
	}
}

func (f *fakeHandler) HandleMessage(ctx context.Context, message contracts.UserMessage) (contracts.AgentResponse, error) {
	f.calls++
	f.received = message
	for _, event := range f.progress {
		agent.ReportProgress(ctx, event)
	}
	return f.outbound, f.handleErr
}

func (f *fakeHandler) FinalizeAudit(_ contracts.UserMessage, err error) {
	f.finalized++
	f.finalizedErr = err
}

func (f *fakeHandler) RecordIgnored(_ contracts.UserMessage, _ string) {
	f.ignored++
}

func TestProcessUpdateRoutesTelegramMessageToAgentRuntime(t *testing.T) {
	handler := &fakeHandler{
		outbound: contracts.AgentResponse{
			Status:  contracts.AgentStatusCompleted,
			Message: "hello from runtime",
		},
		progress: []agent.ProgressEvent{
			{Stage: agent.ProgressStageThinking},
			{Stage: agent.ProgressStageToolStarted, ToolName: "chat.listMessages"},
			{Stage: agent.ProgressStageFinalizing},
		},
	}

	var calls []struct {
		path    string
		payload map[string]any
	}
	botTransport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") && !strings.HasSuffix(r.URL.Path, "/editMessageText") {
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		calls = append(calls, struct {
			path    string
			payload map[string]any
		}{path: r.URL.Path, payload: payload})
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":42}}`), nil
		}
		return jsonResponse(http.StatusOK, `{"ok":true}`), nil
	})

	bot := New("token", 123, t.TempDir(), handler, nil)
	bot.client = &http.Client{Transport: botTransport}

	processed, err := bot.processUpdate(context.Background(), telegramUpdate{
		UpdateID: 7,
		Message: &telegramMessage{
			From: &telegramUser{ID: 123},
			Chat: telegramChat{ID: 55},
			Text: "what time is it?",
		},
	})
	if err != nil {
		t.Fatalf("processUpdate() error = %v", err)
	}
	if !processed {
		t.Fatal("expected update to be processed")
	}
	if handler.calls != 1 {
		t.Fatalf("expected one handler call, got %d", handler.calls)
	}
	if handler.received.RequestID != "telegram_update_7" {
		t.Fatalf("unexpected request id: %q", handler.received.RequestID)
	}
	if handler.received.SessionID != "telegram_chat_55" {
		t.Fatalf("unexpected session id: %q", handler.received.SessionID)
	}
	if handler.received.Channel != "telegram" {
		t.Fatalf("unexpected channel: %q", handler.received.Channel)
	}
	if handler.finalized != 1 || handler.finalizedErr != nil {
		t.Fatalf("expected successful audit finalization, got count=%d err=%v", handler.finalized, handler.finalizedErr)
	}
	if len(calls) < 3 {
		t.Fatalf("expected processing, progress, and final edits, got %#v", calls)
	}
	if !strings.HasSuffix(calls[0].path, "/sendMessage") || calls[0].payload["text"] != "Đang xử lý..." {
		t.Fatalf("unexpected initial telegram call: %#v", calls[0])
	}
	seenMessageProgress := false
	for _, call := range calls {
		if strings.Contains(fmt.Sprint(call.payload["text"]), "Đang lấy tin nhắn") {
			seenMessageProgress = true
		}
	}
	if !seenMessageProgress {
		t.Fatalf("expected chat message progress edit, got %#v", calls)
	}
	for _, call := range calls {
		text := fmt.Sprint(call.payload["text"])
		if strings.Contains(text, "phân loại") || strings.Contains(text, "lập kế hoạch") || strings.Contains(text, "phân tích yêu cầu") || strings.Contains(text, "tổng hợp") {
			t.Fatalf("internal progress should not be exposed to Telegram, got %#v", call)
		}
	}
	last := calls[len(calls)-1]
	if !strings.HasSuffix(last.path, "/editMessageText") || last.payload["text"] != "hello from runtime" {
		t.Fatalf("unexpected final telegram call: %#v", last)
	}
}

func TestTelegramProgressTextHidesInternalRoutingStages(t *testing.T) {
	for _, stage := range []agent.ProgressStage{
		agent.ProgressStageClassifying,
		agent.ProgressStageClassified,
		agent.ProgressStagePlanning,
		agent.ProgressStagePlanned,
		agent.ProgressStageThinking,
		agent.ProgressStageFinalizing,
	} {
		if got := telegramProgressText(agent.ProgressEvent{Stage: stage}); got != "" {
			t.Fatalf("expected stage %s to be hidden, got %q", stage, got)
		}
	}
}

func TestProcessUpdateDownloadsPhotoAttachmentAndPassesMetadata(t *testing.T) {
	handler := &fakeHandler{
		outbound: contracts.AgentResponse{
			Status:  contracts.AgentStatusCompleted,
			Message: "ok",
		},
	}

	botTransport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			return jsonResponse(http.StatusOK, `{"ok":true,"result":{"file_path":"photos/demo.jpg"}}`), nil
		case strings.Contains(r.URL.Path, "/file/bottoken/photos/demo.jpg"):
			return jsonResponse(http.StatusOK, `demo-image-bytes`), nil
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":42}}`), nil
		case strings.HasSuffix(r.URL.Path, "/editMessageText"):
			return jsonResponse(http.StatusOK, `{"ok":true}`), nil
		default:
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
			return nil, nil
		}
	})

	dataDir := t.TempDir()
	bot := New("token", 123, dataDir, handler, nil)
	bot.client = &http.Client{Transport: botTransport}

	processed, err := bot.processUpdate(context.Background(), telegramUpdate{
		UpdateID: 9,
		Message: &telegramMessage{
			MessageID: 88,
			From:      &telegramUser{ID: 123},
			Chat:      telegramChat{ID: 55},
			Caption:   "gửi file này vào nhóm VClaw",
			Photo: []telegramPhotoSize{
				{FileID: "small", FileUniqueID: "small", Width: 10, Height: 10},
				{FileID: "large", FileUniqueID: "large", Width: 100, Height: 100},
			},
		},
	})
	if err != nil {
		t.Fatalf("processUpdate() error = %v", err)
	}
	if !processed {
		t.Fatal("expected update to be processed")
	}
	if handler.calls != 1 {
		t.Fatalf("expected one handler call, got %d", handler.calls)
	}
	if handler.received.Text != "gửi file này vào nhóm VClaw" {
		t.Fatalf("expected caption text, got %q", handler.received.Text)
	}
	paths, ok := handler.received.Metadata["attachmentPaths"].([]string)
	if !ok || len(paths) != 1 {
		t.Fatalf("expected attachment path metadata, got %#v", handler.received.Metadata)
	}
	if !strings.Contains(paths[0], "telegram_attachments") {
		t.Fatalf("expected local attachment path, got %q", paths[0])
	}
	bytes, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("read downloaded attachment: %v", err)
	}
	if string(bytes) != "demo-image-bytes" {
		t.Fatalf("unexpected downloaded bytes: %q", string(bytes))
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

func TestProcessUpdateIgnoresUnauthorizedUser(t *testing.T) {
	handler := &fakeHandler{}
	bot := New("token", 123, t.TempDir(), handler, nil)

	processed, err := bot.processUpdate(context.Background(), telegramUpdate{
		UpdateID: 8,
		Message: &telegramMessage{
			From: &telegramUser{ID: 456},
			Chat: telegramChat{ID: 55},
			Text: "hello",
		},
	})
	if err != nil {
		t.Fatalf("processUpdate() error = %v", err)
	}
	if !processed {
		t.Fatal("expected unauthorized update to be marked processed")
	}
	if handler.calls != 0 {
		t.Fatalf("unauthorized message should not call handler, got %d calls", handler.calls)
	}
	if handler.ignored != 1 {
		t.Fatalf("expected ignored audit, got %d", handler.ignored)
	}
}

func TestTelegramTextHidesDetailedFailedErrors(t *testing.T) {
	text := telegramTextFromResponse(contracts.AgentResponse{
		Status:  contracts.AgentStatusFailed,
		Message: "provider chat failed: openai chat failed: token leaked detail",
	})

	if strings.Contains(text, "openai") || strings.Contains(text, "token leaked detail") {
		t.Fatalf("telegram text should hide detailed errors, got %q", text)
	}
	if !strings.Contains(text, "terminal local") {
		t.Fatalf("telegram text should point to local terminal, got %q", text)
	}
}

func TestTelegramTextShowsFriendlyCancelMessage(t *testing.T) {
	text := telegramTextFromResponse(contracts.AgentResponse{
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

func TestRedactTelegramToken(t *testing.T) {
	got := redactTelegramToken("Post https://api.telegram.org/botabc/sendMessage failed", "abc")
	if strings.Contains(got, "abc") {
		t.Fatalf("token was not redacted: %q", got)
	}
}

func TestTelegramApprovalCallbackRoundTrip(t *testing.T) {
	data := telegramApprovalCallbackData("approve", "appr_123")
	action, approvalID, ok := parseTelegramApprovalCallback(data)
	if !ok {
		t.Fatalf("expected callback to parse: %q", data)
	}
	if action != "approve" || approvalID != "appr_123" {
		t.Fatalf("unexpected callback parse result action=%q approvalID=%q", action, approvalID)
	}
}

func TestTelegramApprovalKeyboardContainsMultipleChoiceButtons(t *testing.T) {
	keyboard := telegramApprovalKeyboard("appr_123")
	rows, ok := keyboard["inline_keyboard"].([][]map[string]string)
	if !ok || len(rows) != 1 || len(rows[0]) != 2 {
		t.Fatalf("unexpected keyboard shape: %#v", keyboard)
	}
	for index, want := range []string{"Xác nhận", "Hủy"} {
		if rows[0][index]["text"] != want {
			t.Fatalf("expected button %d to be %q, got %#v", index, want, rows[0][index])
		}
	}
}

func TestTelegramApprovalTextOmitsTechnicalFields(t *testing.T) {
	text := telegramTextFromResponse(contracts.AgentResponse{
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
			t.Fatalf("telegram approval text leaked %q: %q", forbidden, text)
		}
	}
	for _, forbidden := range []string{"Cần bạn xác nhận trước khi thực hiện.", "Hành động:"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("telegram approval text should omit %q: %q", forbidden, text)
		}
	}
}

func TestTelegramApprovalTextShowsSandboxPythonCode(t *testing.T) {
	text := telegramTextFromResponse(contracts.AgentResponse{
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

func TestTelegramApprovalTextShowsEmailDraftDetails(t *testing.T) {
	body := "Chào bạn,\n\nMời bạn tham dự cuộc họp chiều nay.\n\nThân mến,\nV-Claw"
	text := telegramTextFromResponse(contracts.AgentResponse{
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

	for _, want := range []string{"Người nhận:", "vmkqa2@gmail.com", "Tiêu đề:", "Mời họp chiều nay", "Nội dung email:", "Mời bạn tham dự cuộc họp chiều nay.", "Thân mến,", telegramPreBlockOpen, telegramPreBlockClose, telegramFieldOpen, telegramFieldClose} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected email approval text to contain %q, got %q", want, text)
		}
	}
}

func TestTelegramApprovalTextUsesGenericFallbackForUnknownTool(t *testing.T) {
	text := telegramTextFromResponse(contracts.AgentResponse{
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

func TestTelegramProcessCallbackQueryRejectsMismatchedApprovalContext(t *testing.T) {
	handler := &fakeHandler{}
	var callbackAnswer string
	var calls []string
	bot := New("token", 123, t.TempDir(), handler, nil)
	bot.state.registerApproval(telegramApprovalContext{
		ApprovalID: "appr_123",
		SessionID:  "telegram_chat_55",
		ChatID:     55,
		MessageID:  42,
	})
	bot.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.Path)
		if !strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") && !strings.HasSuffix(r.URL.Path, "/editMessageReplyMarkup") {
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			callbackAnswer = fmt.Sprint(payload["text"])
		}
		return jsonResponse(http.StatusOK, `{"ok":true}`), nil
	})}

	processed, err := bot.processCallbackQuery(context.Background(), telegramUpdate{
		CallbackQuery: &telegramCallbackQuery{
			ID:   "cb1",
			From: &telegramUser{ID: 123},
			Data: telegramApprovalCallbackData("approve", "appr_123"),
			Message: &telegramMessage{
				MessageID: 99,
				Chat:      telegramChat{ID: 55},
			},
		},
	})
	if err != nil {
		t.Fatalf("processCallbackQuery() error = %v", err)
	}
	if !processed {
		t.Fatal("expected callback to be processed")
	}
	if handler.calls != 0 {
		t.Fatalf("mismatched callback should not call handler, got %d calls", handler.calls)
	}
	if len(calls) < 2 || !strings.HasSuffix(calls[0], "/editMessageReplyMarkup") {
		t.Fatalf("expected stale approval to dismiss keyboard before answering, got %#v", calls)
	}
	if !strings.Contains(callbackAnswer, "không còn hợp lệ") {
		t.Fatalf("expected invalid approval feedback, got %q", callbackAnswer)
	}
}

func TestTelegramRevisionReplyUsesStoredApprovalContext(t *testing.T) {
	handler := &fakeHandler{
		outbound: contracts.AgentResponse{
			Status:  contracts.AgentStatusCompleted,
			Message: "Đã ghi nhận chỉnh sửa.",
		},
	}

	var calls []map[string]any
	bot := New("token", 123, t.TempDir(), handler, nil)
	bot.state.revisions[55] = telegramRevisionContext{
		ApprovalID: "appr_123",
		SessionID:  "telegram_chat_55",
		ChatID:     55,
	}
	bot.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		calls = append(calls, payload)
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":42}}`), nil
		}
		return jsonResponse(http.StatusOK, `{"ok":true}`), nil
	})}

	processed, err := bot.processUpdate(context.Background(), telegramUpdate{
		UpdateID: 11,
		Message: &telegramMessage{
			From: &telegramUser{ID: 123},
			Chat: telegramChat{ID: 55},
			Text: "đổi tiêu đề thành Chào buổi sáng",
		},
	})
	if err != nil {
		t.Fatalf("processUpdate() error = %v", err)
	}
	if !processed {
		t.Fatal("expected update to be processed")
	}
	if handler.received.Text != "revise đổi tiêu đề thành Chào buổi sáng" {
		t.Fatalf("unexpected revision command: %q", handler.received.Text)
	}
	if handler.received.SessionID != "telegram_chat_55" {
		t.Fatalf("unexpected revision session: %q", handler.received.SessionID)
	}
	if len(calls) < 2 {
		t.Fatalf("expected send and edit calls, got %#v", calls)
	}
}

func TestTelegramNewMessageDismissesExistingApprovalKeyboard(t *testing.T) {
	handler := &fakeHandler{
		outbound: contracts.AgentResponse{
			Status:  contracts.AgentStatusCompleted,
			Message: "ok",
		},
	}

	type telegramCall struct {
		path    string
		payload map[string]any
	}

	var calls []telegramCall
	bot := New("token", 123, t.TempDir(), handler, nil)
	bot.state.registerApproval(telegramApprovalContext{
		ApprovalID: "appr_existing",
		SessionID:  "telegram_chat_55",
		ChatID:     55,
		MessageID:  42,
		ToolName:   "sandbox.runPython",
		PromptText: "pending approval",
	})
	bot.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
		}
		calls = append(calls, telegramCall{path: r.URL.Path, payload: payload})
		switch {
		case strings.HasSuffix(r.URL.Path, "/editMessageReplyMarkup"):
			return jsonResponse(http.StatusOK, `{"ok":true}`), nil
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":90}}`), nil
		case strings.HasSuffix(r.URL.Path, "/editMessageText"):
			return jsonResponse(http.StatusOK, `{"ok":true}`), nil
		default:
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
			return nil, nil
		}
	})}

	processed, err := bot.processUpdate(context.Background(), telegramUpdate{
		UpdateID: 13,
		Message: &telegramMessage{
			From: &telegramUser{ID: 123},
			Chat: telegramChat{ID: 55},
			Text: "chạy tiếp nhé",
		},
	})
	if err != nil {
		t.Fatalf("processUpdate() error = %v", err)
	}
	if !processed {
		t.Fatal("expected update to be processed")
	}
	if handler.received.Text != "revise chạy tiếp nhé" {
		t.Fatalf("unexpected user text routed to handler: %q", handler.received.Text)
	}
	if _, ok := bot.state.lookupApproval("appr_existing", 55, 42); ok {
		t.Fatal("expected stale approval to be removed after the user sent a new message")
	}
	if len(calls) < 3 {
		t.Fatalf("expected keyboard dismissal, processing message, and final update, got %#v", calls)
	}
	if !strings.HasSuffix(calls[0].path, "/editMessageReplyMarkup") {
		t.Fatalf("expected existing approval keyboard to be dismissed first, got %#v", calls)
	}
	if got := fmt.Sprint(calls[0].payload["message_id"]); got != "42" {
		t.Fatalf("expected dismissal to target the old approval message, got message_id=%q payload=%#v", got, calls[0].payload)
	}
}

func TestTelegramRevisionLikeMessageDismissesExistingApprovalKeyboard(t *testing.T) {
	handler := &fakeHandler{
		outbound: contracts.AgentResponse{
			Status:  contracts.AgentStatusCompleted,
			Message: "ok",
		},
	}

	type telegramCall struct {
		path    string
		payload map[string]any
	}

	var calls []telegramCall
	bot := New("token", 123, t.TempDir(), handler, nil)
	bot.state.registerApproval(telegramApprovalContext{
		ApprovalID: "appr_existing",
		SessionID:  "telegram_chat_55",
		ChatID:     55,
		MessageID:  42,
		ToolName:   "gmail.createDraft",
		PromptText: "pending approval",
	})
	bot.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
		}
		calls = append(calls, telegramCall{path: r.URL.Path, payload: payload})
		switch {
		case strings.HasSuffix(r.URL.Path, "/editMessageReplyMarkup"):
			return jsonResponse(http.StatusOK, `{"ok":true}`), nil
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":90}}`), nil
		case strings.HasSuffix(r.URL.Path, "/editMessageText"):
			return jsonResponse(http.StatusOK, `{"ok":true}`), nil
		default:
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
			return nil, nil
		}
	})}

	processed, err := bot.processUpdate(context.Background(), telegramUpdate{
		UpdateID: 14,
		Message: &telegramMessage{
			From: &telegramUser{ID: 123},
			Chat: telegramChat{ID: 55},
			Text: "sửa lại nội dung cho thân mật hơn nhé",
		},
	})
	if err != nil {
		t.Fatalf("processUpdate() error = %v", err)
	}
	if !processed {
		t.Fatal("expected update to be processed")
	}
	if handler.received.Text != "revise sửa lại nội dung cho thân mật hơn nhé" {
		t.Fatalf("unexpected user text routed to handler: %q", handler.received.Text)
	}
	if len(calls) < 3 {
		t.Fatalf("expected dismissal, processing message, and final update, got %#v", calls)
	}
	if !strings.HasSuffix(calls[0].path, "/editMessageReplyMarkup") {
		t.Fatalf("expected existing approval keyboard to be dismissed first, got %#v", calls)
	}
}

func TestTelegramRevisePromptIncludesPendingContext(t *testing.T) {
	handler := &fakeHandler{}
	var (
		editedText          string
		sentText            string
		editAttempts        int
		replyMarkupEdits    int
		dismissedMessageIDs []string
	)
	bot := New("token", 123, t.TempDir(), handler, nil)
	bot.state.registerApproval(telegramApprovalContext{
		ApprovalID: "appr_py",
		SessionID:  "telegram_chat_55",
		ChatID:     55,
		MessageID:  42,
		ToolName:   "sandbox.runPython",
		PromptText: "Hành động: Chạy mã Python trong sandbox\n\nMã Python sẽ chạy:\n\nprint('hello')",
	})
	bot.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			sentText = fmt.Sprint(payload["text"])
			editedText = sentText
			return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":77}}`), nil
		}
		if strings.HasSuffix(r.URL.Path, "/editMessageReplyMarkup") {
			replyMarkupEdits++
			dismissedMessageIDs = append(dismissedMessageIDs, fmt.Sprint(payload["message_id"]))
		}
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			editAttempts++
		}
		return jsonResponse(http.StatusOK, `{"ok":true}`), nil
	})}

	processed, err := bot.processCallbackQuery(context.Background(), telegramUpdate{
		CallbackQuery: &telegramCallbackQuery{
			ID:   "cb2",
			From: &telegramUser{ID: 123},
			Data: telegramApprovalCallbackData("revise", "appr_py"),
			Message: &telegramMessage{
				MessageID: 42,
				Chat:      telegramChat{ID: 55},
			},
		},
	})
	if err != nil {
		t.Fatalf("processCallbackQuery() error = %v", err)
	}
	if !processed {
		t.Fatal("expected callback to be processed")
	}
	if !strings.Contains(editedText, "Nội dung đang chờ xác nhận") {
		t.Fatalf("expected revise prompt to include pending context, got %q", editedText)
	}
	if !strings.Contains(editedText, "print(&#39;hello&#39;)") && !strings.Contains(editedText, "print('hello')") {
		t.Fatalf("expected revise prompt to include pending code, got %q", editedText)
	}
	if editAttempts != 0 {
		t.Fatalf("expected revise flow to preserve the original approval message, got %d edit attempts", editAttempts)
	}
	if replyMarkupEdits != 1 {
		t.Fatalf("expected revise flow to dismiss the old approval keyboard once, got %d", replyMarkupEdits)
	}
	if len(dismissedMessageIDs) != 1 || dismissedMessageIDs[0] != "42" {
		t.Fatalf("expected revise flow to dismiss keyboard on the original approval message, got %#v", dismissedMessageIDs)
	}
}

func TestTelegramRenderHTMLConvertsCodeBlockMarkers(t *testing.T) {
	rendered := telegramRenderHTML("Mã Python sẽ chạy:\n\n" + telegramCodeBlock("python", "print('hello')"))

	if !strings.Contains(rendered, "<pre><code") {
		t.Fatalf("expected html code block, got %q", rendered)
	}
	if !strings.Contains(rendered, "language-python") {
		t.Fatalf("expected python language class, got %q", rendered)
	}
	if !strings.Contains(rendered, "print(&#39;hello&#39;)") && !strings.Contains(rendered, "print('hello')") {
		t.Fatalf("expected escaped code content, got %q", rendered)
	}
}

func TestTelegramRenderHTMLConvertsPreBlockMarkers(t *testing.T) {
	rendered := telegramRenderHTML("Nội dung email:\n\n" + telegramPreBlock("Chào bạn,\n\nThân mến,\nV-Claw"))

	if !strings.Contains(rendered, "<blockquote>") {
		t.Fatalf("expected html blockquote, got %q", rendered)
	}
	if strings.Contains(rendered, "<code") {
		t.Fatalf("expected email preview to avoid code markup, got %q", rendered)
	}
	if !strings.Contains(rendered, "Thân mến,") {
		t.Fatalf("expected full email body in preview block, got %q", rendered)
	}
}

func TestTelegramRenderHTMLConvertsFieldMarkers(t *testing.T) {
	rendered := telegramRenderHTML(telegramField("Người nhận", "vmkqa2@gmail.com") + "\n" + telegramField("Tiêu đề", "Hỏi thăm sức khỏe"))

	for _, want := range []string{"<b>Người nhận:</b>", "<code>vmkqa2@gmail.com</code>", "<b>Tiêu đề:</b>", "<code>Hỏi thăm sức khỏe</code>"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered field markup %q, got %q", want, rendered)
		}
	}
}
