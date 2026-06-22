package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vclaw/internal/agent"
	"vclaw/internal/contracts"
	"vclaw/internal/monitoring"
	"vclaw/internal/policies"
)

type fakeHandler struct {
	calls        int
	ignored      int
	finalized    int
	received     contracts.UserMessage
	receivedAll  []contracts.UserMessage
	resetSession string
	outbound     contracts.AgentResponse
	progress     []agent.ProgressEvent
	handleErr    error
	finalizedErr error
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func TestTelegramApproveFlowKeepsOriginalApprovalMessage(t *testing.T) {
	handler := &fakeHandler{
		outbound: contracts.AgentResponse{
			Status:  contracts.AgentStatusCompleted,
			Message: "Đã chạy xong.",
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
		PromptText: "Hành động: Chạy mã Python trong sandbox\n\nMã Python sẽ chạy:\n\nprint('hello')",
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

func TestTelegramApproveCallbackFallsBackToPersistedApprovalWhenStateIsMissing(t *testing.T) {
	handler := &fakeHandler{
		outbound: contracts.AgentResponse{
			Status:  contracts.AgentStatusCompleted,
			Message: "approval handled",
		},
	}

	type telegramCall struct {
		path    string
		payload map[string]any
	}

	var calls []telegramCall
	bot := New("token", 123, t.TempDir(), handler, nil)
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
			return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":89}}`), nil
		case strings.HasSuffix(r.URL.Path, "/editMessageText"):
			return jsonResponse(http.StatusOK, `{"ok":true}`), nil
		default:
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
			return nil, nil
		}
	})}

	processed, err := bot.processCallbackQuery(context.Background(), telegramUpdate{
		UpdateID: 13,
		CallbackQuery: &telegramCallbackQuery{
			ID:   "cb4",
			From: &telegramUser{ID: 123},
			Data: telegramApprovalCallbackData("approve", "appr_db"),
			Message: &telegramMessage{
				MessageID: 43,
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
	if handler.received.Text != "approve appr_db" {
		t.Fatalf("unexpected approval command: %q", handler.received.Text)
	}
	if handler.received.SessionID != "telegram_chat_55" {
		t.Fatalf("expected fallback session id from chat, got %q", handler.received.SessionID)
	}
	if got := handler.received.Metadata["approvalId"]; got != "appr_db" {
		t.Fatalf("expected approvalId metadata, got %#v", got)
	}
	if len(calls) < 4 {
		t.Fatalf("expected stale keyboard dismissal, callback answer, processing message, and final update, got %#v", calls)
	}
	if !strings.HasSuffix(calls[0].path, "/editMessageReplyMarkup") {
		t.Fatalf("expected stale keyboard dismissal first, got %#v", calls)
	}
	if !strings.HasSuffix(calls[1].path, "/answerCallbackQuery") {
		t.Fatalf("expected callback answer after stale fallback, got %#v", calls)
	}
}

func (f *fakeHandler) HandleMessage(ctx context.Context, message contracts.UserMessage) (contracts.AgentResponse, error) {
	f.calls++
	f.received = message
	f.receivedAll = append(f.receivedAll, message)
	for _, event := range f.progress {
		agent.ReportProgress(ctx, event)
	}
	return f.outbound, f.handleErr
}

func (f *fakeHandler) ResetSession(_ context.Context, sessionID string) error {
	f.resetSession = sessionID
	return nil
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

	bot := New("token", 123, t.TempDir(), nil, handler, nil)
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

func TestTelegramTextFromResponseFormatsDownloadAttachmentsResult(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	path := filepath.Join(homeDir, "Downloads", "Vclaw", "Google Workspace Message.png")
	response := contracts.AgentResponse{
		Status:  contracts.AgentStatusCompleted,
		Message: "ignored generic message",
		ToolResults: []contracts.ToolResult{{
			ToolName: "gmail.downloadAttachments",
			Success:  true,
			Data: map[string]any{
				"contentForLLM": fmt.Sprintf(`{"files":[{"filename":"Google Workspace Message.png","path":%q}]}`, path),
			},
		}},
	}

	got := telegramTextFromResponse(response)
	want := "Đã tải xuống: Google Workspace Message.png\nThư mục: ~/Downloads/Vclaw/"
	if got != want {
		t.Fatalf("telegramTextFromResponse() = %q, want %q", got, want)
	}
}

func TestTelegramProgressTextHidesInternalRoutingStages(t *testing.T) {
	for _, stage := range []agent.ProgressStage{
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

func TestIsTelegramPolicyCommand(t *testing.T) {
	for _, input := range []string{"/policy", "/policy@vclaw_bot", "/policy extra args"} {
		if !isTelegramPolicyCommand(input) {
			t.Fatalf("expected %q to match /policy command", input)
		}
	}
	for _, input := range []string{"policy", "cài đặt policy", "/other"} {
		if isTelegramPolicyCommand(input) {
			t.Fatalf("unexpected match for %q", input)
		}
	}
}

func TestIsTelegramStatusCommand(t *testing.T) {
	for _, input := range []string{"/status", "/status@vclaw_bot", "/status extra args"} {
		if !isTelegramStatusCommand(input) {
			t.Fatalf("expected %q to match /status command", input)
		}
	}
	for _, input := range []string{"status", "xem status", "/other"} {
		if isTelegramStatusCommand(input) {
			t.Fatalf("unexpected match for %q", input)
		}
	}
}

func TestIsTelegramHistoryCommand(t *testing.T) {
	for _, input := range []string{"/history", "/history@vclaw_bot", "/history extra args"} {
		if !isTelegramHistoryCommand(input) {
			t.Fatalf("expected %q to match /history command", input)
		}
	}
	for _, input := range []string{"history", "xem history", "/other"} {
		if isTelegramHistoryCommand(input) {
			t.Fatalf("unexpected match for %q", input)
		}
	}
}

func TestIsTelegramNewCommand(t *testing.T) {
	for _, input := range []string{"/new", "/new@vclaw_bot", "/new extra args"} {
		if !isTelegramNewCommand(input) {
			t.Fatalf("expected %q to match /new command", input)
		}
	}
	for _, input := range []string{"new", "tạo phiên mới", "/other"} {
		if isTelegramNewCommand(input) {
			t.Fatalf("unexpected match for %q", input)
		}
	}
}

func TestProcessUpdateRoutesNewCommandToNewActiveSession(t *testing.T) {
	handler := &fakeHandler{}
	var sentText string
	botTransport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		sentText = fmt.Sprint(payload["text"])
		return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":42}}`), nil
	})

	bot := New("token", 123, t.TempDir(), nil, handler, nil)
	bot.client = &http.Client{Transport: botTransport}

	processed, err := bot.processUpdate(context.Background(), telegramUpdate{
		UpdateID: 8,
		Message: &telegramMessage{
			From: &telegramUser{ID: 123},
			Chat: telegramChat{ID: 55},
			Text: "/new",
		},
	})
	if err != nil {
		t.Fatalf("processUpdate() error = %v", err)
	}
	if !processed {
		t.Fatal("expected update to be processed")
	}
	if handler.resetSession != "" {
		t.Fatalf("/new should not reset an existing session, got %q", handler.resetSession)
	}
	if handler.calls != 0 {
		t.Fatalf("/new should not call HandleMessage, got %d calls", handler.calls)
	}
	if !strings.Contains(sentText, "Đã tạo phiên mới") {
		t.Fatalf("unexpected reset confirmation: %q", sentText)
	}
	index, err := bot.sessionIndex.List(context.Background(), 55, time.Now().UTC())
	if err != nil {
		t.Fatalf("load session index: %v", err)
	}
	if len(index.Sessions) != 2 {
		t.Fatalf("expected legacy + new session, got %#v", index.Sessions)
	}
	if index.ActiveSessionKey == "legacy" {
		t.Fatalf("expected /new to switch active session, got %#v", index)
	}
}

func TestProcessUpdateRoutesStatusCommandToMonitoringSummary(t *testing.T) {
	handler := &fakeHandler{}
	oldLatest := queryLatestTelegramRun
	oldRunByID := queryTelegramRunByID
	t.Cleanup(func() {
		queryLatestTelegramRun = oldLatest
		queryTelegramRunByID = oldRunByID
	})
	queryLatestTelegramRun = func(_ context.Context, _ string, sessionID string) (monitoring.LatestRun, error) {
		if sessionID != "telegram_chat_55" {
			t.Fatalf("unexpected session id: %s", sessionID)
		}
		return monitoring.LatestRun{
			RunID:     "run_1",
			RequestID: "req_1",
			SessionID: sessionID,
			Status:    "completed",
		}, nil
	}
	queryTelegramRunByID = func(_ context.Context, _ string, runID string) (*agent.RunState, error) {
		if runID != "run_1" {
			t.Fatalf("unexpected run id: %s", runID)
		}
		completedAt := time.Date(2026, 6, 17, 14, 32, 4, 100_000_000, time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60))
		return &agent.RunState{
			RunID:        "run_1",
			OriginalGoal: "Tóm tắt email + tạo draft báo cáo",
			Status:       "completed",
			CostUSD:      0.0123,
			Steps: []agent.RunStep{
				{OK: true, Text: "Đọc 12 email mới"},
				{OK: true, Text: "Đã tạo bản nháp email"},
			},
			CreatedAt:   time.Date(2026, 6, 17, 14, 32, 0, 0, time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60)),
			CompletedAt: &completedAt,
		}, nil
	}

	var sentText string
	bot := New("token", 123, t.TempDir(), nil, handler, nil)
	bot.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		sentText = fmt.Sprint(payload["text"])
		return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":42}}`), nil
	})}

	processed, err := bot.processUpdate(context.Background(), telegramUpdate{
		UpdateID: 21,
		Message: &telegramMessage{
			From: &telegramUser{ID: 123},
			Chat: telegramChat{ID: 55},
			Text: "/status",
		},
	})
	if err != nil {
		t.Fatalf("processUpdate() error = %v", err)
	}
	if !processed {
		t.Fatal("expected update to be processed")
	}
	if handler.calls != 0 {
		t.Fatalf("expected /status to bypass agent pipeline, got %d handler calls", handler.calls)
	}
	for _, want := range []string{
		"📊 *Trạng thái lệnh gần nhất*",
		"🗓 Thời gian: 17/06/2026 14:32:00",
		"📝 *Yêu cầu*",
		"Tóm tắt email + tạo draft báo cáo",
		"✅ Đọc 12 email mới",
		"✅ Đã tạo bản nháp email",
		"⚡ Thời gian xử lý: 4.1 giây",
		"💰 Chi phí: $0.0123 (~316 VNĐ)",
		"Trạng thái: ✅ Hoàn thành",
	} {
		if !strings.Contains(sentText, want) {
			t.Fatalf("missing %q in /status text:\n%s", want, sentText)
		}
	}
}

func TestProcessUpdateRoutesStatusCommandToFailedMonitoringSummary(t *testing.T) {
	handler := &fakeHandler{}
	oldLatest := queryLatestTelegramRun
	oldRunByID := queryTelegramRunByID
	t.Cleanup(func() {
		queryLatestTelegramRun = oldLatest
		queryTelegramRunByID = oldRunByID
	})
	queryLatestTelegramRun = func(_ context.Context, _ string, _ string) (monitoring.LatestRun, error) {
		return monitoring.LatestRun{
			RunID:     "run_2",
			RequestID: "req_2",
			SessionID: "telegram_chat_55",
			Status:    "failed",
		}, nil
	}
	queryTelegramRunByID = func(_ context.Context, _ string, runID string) (*agent.RunState, error) {
		if runID != "run_2" {
			t.Fatalf("unexpected run id: %s", runID)
		}
		completedAt := time.Date(2026, 6, 17, 14, 32, 2, 300_000_000, time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60))
		return &agent.RunState{
			RunID:        "run_2",
			OriginalGoal: "Gửi báo cáo",
			Status:       "failed",
			ErrorRef:     "A8F3C2",
			Steps: []agent.RunStep{
				{OK: false, Text: "Không kết nối được Google Drive"},
			},
			CreatedAt:   time.Date(2026, 6, 17, 14, 32, 0, 0, time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60)),
			CompletedAt: &completedAt,
		}, nil
	}

	var sentText string
	bot := New("token", 123, t.TempDir(), nil, handler, nil)
	bot.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		sentText = fmt.Sprint(payload["text"])
		return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":43}}`), nil
	})}

	processed, err := bot.processUpdate(context.Background(), telegramUpdate{
		UpdateID: 22,
		Message: &telegramMessage{
			From: &telegramUser{ID: 123},
			Chat: telegramChat{ID: 55},
			Text: "/status",
		},
	})
	if err != nil {
		t.Fatalf("processUpdate() error = %v", err)
	}
	if !processed {
		t.Fatal("expected update to be processed")
	}
	for _, want := range []string{
		"Gửi báo cáo",
		"❌ Không kết nối được Google Drive",
		"⚡ Thời gian xử lý: 2.3 giây",
		"Trạng thái: ❌ Thất bại",
		"🔍 Ref: A8F3C2",
	} {
		if !strings.Contains(sentText, want) {
			t.Fatalf("missing %q in failed /status text:\n%s", want, sentText)
		}
	}
}

func TestProcessUpdateRoutesHistoryCommandToMonitoringSummary(t *testing.T) {
	handler := &fakeHandler{}
	oldRecent := queryRecentTelegramRuns
	oldRunByID := queryTelegramRunByID
	t.Cleanup(func() {
		queryRecentTelegramRuns = oldRecent
		queryTelegramRunByID = oldRunByID
	})
	queryRecentTelegramRuns = func(_ context.Context, _ string, sessionID string, limit int) ([]monitoring.LatestRun, error) {
		if sessionID != "telegram_chat_55" || limit != 10 {
			t.Fatalf("unexpected history query: session=%s limit=%d", sessionID, limit)
		}
		return []monitoring.LatestRun{
			{RunID: "run_1"},
			{RunID: "run_2"},
			{RunID: "run_3"},
		}, nil
	}
	queryTelegramRunByID = func(_ context.Context, _ string, runID string) (*agent.RunState, error) {
		loc := time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60)
		switch runID {
		case "run_1":
			return &agent.RunState{RunID: "run_1", ShortLabel: "Tóm tắt email", Category: "gmail", Status: "completed", CreatedAt: time.Date(2026, 6, 17, 14, 32, 0, 0, loc), CompletedAt: ptrTime(time.Date(2026, 6, 17, 14, 32, 3, 100_000_000, loc))}, nil
		case "run_2":
			return &agent.RunState{RunID: "run_2", ShortLabel: "Đặt lịch họp", Category: "calendar", Status: "failed", CreatedAt: time.Date(2026, 6, 17, 14, 30, 0, 0, loc), CompletedAt: ptrTime(time.Date(2026, 6, 17, 14, 30, 2, 400_000_000, loc)), ErrorRef: "A8F3C2"}, nil
		case "run_3":
			return &agent.RunState{RunID: "run_3", ShortLabel: "Gửi báo cáo tuần", Category: "docs", Status: "completed", CreatedAt: time.Date(2026, 6, 17, 14, 28, 0, 0, loc), CompletedAt: ptrTime(time.Date(2026, 6, 17, 14, 28, 5, 200_000_000, loc))}, nil
		default:
			t.Fatalf("unexpected run id: %s", runID)
			return nil, nil
		}
	}

	var sentText string
	bot := New("token", 123, t.TempDir(), nil, handler, nil)
	bot.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		sentText = fmt.Sprint(payload["text"])
		return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":44}}`), nil
	})}

	processed, err := bot.processUpdate(context.Background(), telegramUpdate{
		UpdateID: 23,
		Message:  &telegramMessage{From: &telegramUser{ID: 123}, Chat: telegramChat{ID: 55}, Text: "/history"},
	})
	if err != nil {
		t.Fatalf("processUpdate() error = %v", err)
	}
	if !processed {
		t.Fatal("expected update to be processed")
	}
	if handler.calls != 0 {
		t.Fatalf("expected /history to bypass agent pipeline, got %d handler calls", handler.calls)
	}
	for _, want := range []string{
		"📋 *Lịch sử gần nhất*",
		"1.",
		"📧",
		"Tóm tắt email",
		"❌",
		"📅",
		"Đặt lịch họp",
		"📄",
		"Gửi báo cáo tuần",
		"_Gõ_ `/history <số>` _để xem chi tiết_",
	} {
		if !strings.Contains(sentText, want) {
			t.Fatalf("missing %q in /history text:\n%s", want, sentText)
		}
	}
}

func TestProcessUpdateRoutesHistoryCommandWhenNoRunsExist(t *testing.T) {
	handler := &fakeHandler{}
	oldRecent := queryRecentTelegramRuns
	t.Cleanup(func() {
		queryRecentTelegramRuns = oldRecent
	})
	queryRecentTelegramRuns = func(_ context.Context, _ string, _ string, _ int) ([]monitoring.LatestRun, error) {
		return []monitoring.LatestRun{}, nil
	}

	var sentText string
	bot := New("token", 123, t.TempDir(), nil, handler, nil)
	bot.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		sentText = fmt.Sprint(payload["text"])
		return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":45}}`), nil
	})}

	processed, err := bot.processUpdate(context.Background(), telegramUpdate{
		UpdateID: 24,
		Message:  &telegramMessage{From: &telegramUser{ID: 123}, Chat: telegramChat{ID: 55}, Text: "/history"},
	})
	if err != nil {
		t.Fatalf("processUpdate() error = %v", err)
	}
	if !processed {
		t.Fatal("expected update to be processed")
	}
	if sentText != "Chưa có lịch sử nào." {
		t.Fatalf("unexpected empty history text: %s", sentText)
	}
}

func TestFormatStatusShowsFullDateAndNoFakeZeroCost(t *testing.T) {
	loc := time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60)
	completedAt := time.Date(2026, 6, 17, 14, 32, 4, 100_000_000, loc)
	run := &agent.RunState{
		RunID:        "run_1",
		OriginalGoal: "Xem lịch ngày mai của tôi",
		Status:       "completed",
		CostUSD:      0,
		CreatedAt:    time.Date(2026, 6, 17, 14, 32, 0, 0, loc),
		CompletedAt:  &completedAt,
	}
	text := FormatStatus(run)
	for _, want := range []string{
		"🗓 Thời gian: 17/06/2026 14:32:00",
		"💰 Chi phí: chưa ghi nhận",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in status text:\n%s", want, text)
		}
	}
}

func TestFormatStatusShowsEmojiStatusLine(t *testing.T) {
	loc := time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60)
	run := &agent.RunState{
		RunID:       "run_ok",
		Status:      "completed",
		CreatedAt:   time.Date(2026, 6, 17, 14, 32, 0, 0, loc),
		CompletedAt: func() *time.Time { t := time.Date(2026, 6, 17, 14, 32, 4, 0, loc); return &t }(),
	}
	text := FormatStatus(run)
	if !strings.Contains(text, "Trạng thái: ✅ Hoàn thành") {
		t.Fatalf("missing success emoji line in status text:\n%s", text)
	}
}

func TestSetMyCommandsRegistersTelegramSlashCommands(t *testing.T) {
	bot := New("token", 123, t.TempDir(), &fakeHandler{}, nil)
	var gotPath string
	var gotPayload map[string]any
	bot.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		return jsonResponse(http.StatusOK, `{"ok":true,"result":true}`), nil
	})}

	if err := bot.setMyCommands(context.Background()); err != nil {
		t.Fatalf("setMyCommands() error = %v", err)
	}
	if gotPath != "/bottoken/setMyCommands" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	commands, ok := gotPayload["commands"].([]any)
	if !ok || len(commands) < 3 {
		t.Fatalf("unexpected commands payload: %#v", gotPayload["commands"])
	}
}

func TestFormatHistoryShowsFiveRuns(t *testing.T) {
	completed := func(seconds float64) *time.Time {
		loc := time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60)
		timeValue := time.Date(2026, 6, 17, 14, 0, 0, 0, loc).Add(time.Duration(seconds * float64(time.Second)))
		return &timeValue
	}
	now := time.Date(2026, 6, 17, 15, 0, 0, 0, time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60))
	runs := []*agent.RunState{
		{RunID: "run_1", ShortLabel: "1", Category: "gmail", Status: "completed", CreatedAt: time.Date(2026, 6, 17, 14, 0, 0, 0, now.Location()), CompletedAt: completed(1)},
		{RunID: "run_2", ShortLabel: "2", Category: "calendar", Status: "completed", CreatedAt: time.Date(2026, 6, 17, 14, 0, 0, 0, now.Location()), CompletedAt: completed(2)},
		{RunID: "run_3", ShortLabel: "3", Category: "drive", Status: "failed", CreatedAt: time.Date(2026, 6, 17, 14, 0, 0, 0, now.Location()), CompletedAt: completed(3)},
		{RunID: "run_4", ShortLabel: "4", Category: "docs", Status: "completed", CreatedAt: time.Date(2026, 6, 17, 14, 0, 0, 0, now.Location()), CompletedAt: completed(4)},
		{RunID: "run_5", ShortLabel: "5", Category: "search", Status: "completed", CreatedAt: time.Date(2026, 6, 17, 14, 0, 0, 0, now.Location()), CompletedAt: completed(5)},
	}
	text := FormatHistory(runs, now)
	if strings.Count(text, "\n") != 8 {
		t.Fatalf("expected header + divider + 5 rows + footer, got %q", text)
	}
	for _, want := range []string{"14:00", "📧", "📅", "📁", "📄", "🔍", "❌"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
}

func TestFormatHistoryShowsDateForOlderRuns(t *testing.T) {
	loc := time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60)
	now := time.Date(2026, 6, 18, 15, 0, 0, 0, loc)
	runs := []*agent.RunState{
		{RunID: "run_old", ShortLabel: "cũ", Category: "gmail", Status: "completed", CreatedAt: time.Date(2026, 6, 15, 14, 0, 0, 0, loc)},
	}
	text := FormatHistory(runs, now)
	if !strings.Contains(text, "15/06 14:00") {
		t.Fatalf("expected older run to include date, got:\n%s", text)
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
	t.Setenv("VCLAW_SANDBOX_WORKSPACE_DIR", filepath.Join(dataDir, "sandbox-root"))
	bot := New("token", 123, dataDir, nil, handler, nil)
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
	if !strings.Contains(paths[0], filepath.Join("agent", "workspace", "data", "telegram_attachments")) {
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
	bot := New("token", 123, t.TempDir(), nil, handler, nil)

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

func TestTelegramPolicySettingsCallbackRoundTrip(t *testing.T) {
	raw := telegramPolicySettingsCallbackData(telegramPolicySettingsCycleAction, 123, contracts.RiskLevelDestructive)
	action, chatID, riskLevel, ok := parseTelegramPolicySettingsCallback(raw)
	if !ok {
		t.Fatalf("expected callback to parse: %q", raw)
	}
	if action != telegramPolicySettingsCycleAction || chatID != 123 || riskLevel != contracts.RiskLevelDestructive {
		t.Fatalf("unexpected parsed callback: action=%q chatID=%d riskLevel=%q", action, chatID, riskLevel)
	}
}

func TestTelegramPolicySettingsKeyboardContainsSaveButton(t *testing.T) {
	assignments := policies.EffectivePolicyAssignments(policies.UserPolicyConfig{})
	keyboard := telegramPolicySettingsKeyboard(123, assignments)
	rows, ok := keyboard["inline_keyboard"].([][]map[string]string)
	if !ok {
		t.Fatalf("unexpected keyboard format: %#v", keyboard)
	}
	if len(rows) != 8 {
		t.Fatalf("expected seven cycle rows plus save row, got %d", len(rows))
	}
	for _, want := range []string{
		"Xem danh sách & thông tin tổng quan\n✅ Tự động cho phép",
		"Tóm tắt, phân tích nội dung\n✅ Tự động cho phép",
		"Đọc nội dung riêng tư\n👤 Cần phê duyệt",
		"Tạo, chỉnh sửa & gửi đi\n👤 Cần phê duyệt",
		"Tải & lưu file về máy\n👤 Cần phê duyệt",
		"Chạy lệnh hệ thống\n👤 Cần phê duyệt",
		"Xóa dữ liệu\n🚫 Luôn chặn",
	} {
		found := false
		for _, row := range rows[:len(rows)-1] {
			if len(row) > 0 && row[0]["text"] == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected keyboard to contain label %q, got %#v", want, keyboard)
		}
	}
	lastRow := rows[len(rows)-1]
	if len(lastRow) != 1 || lastRow[0]["text"] != "Lưu" {
		t.Fatalf("expected save button, got %#v", lastRow)
	}
}

func TestTelegramPolicySettingsCyclesEditButtonsAndSaveShowsBriefConfirmation(t *testing.T) {
	dataDir := t.TempDir()
	store, err := policies.NewUserPolicyStore(filepath.Join(dataDir, "user-policy.json"))
	if err != nil {
		t.Fatalf("new user policy store: %v", err)
	}

	type telegramCall struct {
		path    string
		payload map[string]any
	}
	var calls []telegramCall
	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			calls = append(calls, telegramCall{path: r.URL.Path, payload: payload})
			if strings.HasSuffix(r.URL.Path, "/sendMessage") {
				return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":42}}`), nil
			}
			return jsonResponse(http.StatusOK, `{"ok":true}`), nil
		}),
	}

	bot := New("token", 123, dataDir, store, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	bot.client = client

	if err := bot.sendPolicySettingsMenu(context.Background(), 55); err != nil {
		t.Fatalf("sendPolicySettingsMenu() error = %v", err)
	}

	sendCall := calls[0]
	if !strings.HasSuffix(sendCall.path, "/sendMessage") {
		t.Fatalf("expected initial sendMessage call, got %#v", sendCall)
	}
	if fmt.Sprint(sendCall.payload["text"]) != "Chạm vào một nút để đổi nhóm cho từng mức rủi ro." {
		t.Fatalf("unexpected menu text: %#v", sendCall.payload["text"])
	}
	assertPolicyKeyboardText(t, sendCall.payload["reply_markup"], map[contracts.RiskLevel]string{
		contracts.RiskLevelSafeRead:      "Xem danh sách & thông tin tổng quan\n✅ Tự động cho phép",
		contracts.RiskLevelSafeCompute:   "Tóm tắt, phân tích nội dung\n✅ Tự động cho phép",
		contracts.RiskLevelSensitiveRead: "Đọc nội dung riêng tư\n👤 Cần phê duyệt",
		contracts.RiskLevelExternalWrite: "Tạo, chỉnh sửa & gửi đi\n👤 Cần phê duyệt",
		contracts.RiskLevelLocalWrite:    "Tải & lưu file về máy\n👤 Cần phê duyệt",
		contracts.RiskLevelCodeExecution: "Chạy lệnh hệ thống\n👤 Cần phê duyệt",
		contracts.RiskLevelDestructive:   "Xóa dữ liệu\n🚫 Luôn chặn",
	})

	for _, level := range []contracts.RiskLevel{contracts.RiskLevelSafeRead, contracts.RiskLevelSafeCompute} {
		processed, err := bot.processCallbackQuery(context.Background(), telegramUpdate{
			CallbackQuery: &telegramCallbackQuery{
				ID:   "callback-" + string(level),
				From: &telegramUser{ID: 123},
				Message: &telegramMessage{
					MessageID: 42,
					Chat:      telegramChat{ID: 55},
				},
				Data: telegramPolicySettingsCallbackData(telegramPolicySettingsCycleAction, 55, level),
			},
		})
		if err != nil {
			t.Fatalf("cycle callback for %s: %v", level, err)
		}
		if !processed {
			t.Fatalf("expected cycle callback for %s to be processed", level)
		}
	}

	processed, err := bot.processCallbackQuery(context.Background(), telegramUpdate{
		CallbackQuery: &telegramCallbackQuery{
			ID:   "callback-save",
			From: &telegramUser{ID: 123},
			Message: &telegramMessage{
				MessageID: 42,
				Chat:      telegramChat{ID: 55},
			},
			Data: telegramPolicySettingsCallbackData(telegramPolicySettingsSaveAction, 55, ""),
		},
	})
	if err != nil {
		t.Fatalf("save callback error = %v", err)
	}
	if !processed {
		t.Fatal("expected save callback to be processed")
	}

	var editCalls []telegramCall
	for _, call := range calls {
		if strings.HasSuffix(call.path, "/editMessageText") {
			editCalls = append(editCalls, call)
		}
	}
	if len(editCalls) != 3 {
		t.Fatalf("expected 3 message edits, got %d: %#v", len(editCalls), editCalls)
	}
	if fmt.Sprint(editCalls[0].payload["text"]) != "Chạm vào một nút để đổi nhóm cho từng mức rủi ro." {
		t.Fatalf("unexpected cycle text: %#v", editCalls[0].payload["text"])
	}
	assertPolicyKeyboardText(t, editCalls[0].payload["reply_markup"], map[contracts.RiskLevel]string{
		contracts.RiskLevelSafeRead:      "Xem danh sách & thông tin tổng quan\n👤 Cần phê duyệt",
		contracts.RiskLevelSafeCompute:   "Tóm tắt, phân tích nội dung\n✅ Tự động cho phép",
		contracts.RiskLevelSensitiveRead: "Đọc nội dung riêng tư\n👤 Cần phê duyệt",
		contracts.RiskLevelExternalWrite: "Tạo, chỉnh sửa & gửi đi\n👤 Cần phê duyệt",
		contracts.RiskLevelLocalWrite:    "Tải & lưu file về máy\n👤 Cần phê duyệt",
		contracts.RiskLevelCodeExecution: "Chạy lệnh hệ thống\n👤 Cần phê duyệt",
		contracts.RiskLevelDestructive:   "Xóa dữ liệu\n🚫 Luôn chặn",
	})
	assertPolicyKeyboardText(t, editCalls[1].payload["reply_markup"], map[contracts.RiskLevel]string{
		contracts.RiskLevelSafeRead:      "Xem danh sách & thông tin tổng quan\n👤 Cần phê duyệt",
		contracts.RiskLevelSafeCompute:   "Tóm tắt, phân tích nội dung\n👤 Cần phê duyệt",
		contracts.RiskLevelSensitiveRead: "Đọc nội dung riêng tư\n👤 Cần phê duyệt",
		contracts.RiskLevelExternalWrite: "Tạo, chỉnh sửa & gửi đi\n👤 Cần phê duyệt",
		contracts.RiskLevelLocalWrite:    "Tải & lưu file về máy\n👤 Cần phê duyệt",
		contracts.RiskLevelCodeExecution: "Chạy lệnh hệ thống\n👤 Cần phê duyệt",
		contracts.RiskLevelDestructive:   "Xóa dữ liệu\n🚫 Luôn chặn",
	})
	if fmt.Sprint(editCalls[2].payload["text"]) != "✅ Đã lưu cài đặt." {
		t.Fatalf("unexpected save text: %#v", editCalls[2].payload["text"])
	}
	markup, ok := editCalls[2].payload["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected save markup: %#v", editCalls[2].payload["reply_markup"])
	}
	if rows, ok := markup["inline_keyboard"].([]any); !ok || len(rows) != 0 {
		t.Fatalf("expected empty keyboard after save, got %#v", editCalls[2].payload["reply_markup"])
	}
}

func TestTelegramPolicySettingsSaveRejectsDestructiveAutoAllowKeepsMenuOpen(t *testing.T) {
	type telegramCall struct {
		path    string
		payload map[string]any
	}
	var calls []telegramCall
	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			calls = append(calls, telegramCall{path: r.URL.Path, payload: payload})
			switch {
			case strings.HasSuffix(r.URL.Path, "/answerCallbackQuery"):
				return jsonResponse(http.StatusOK, `{"ok":true}`), nil
			case strings.HasSuffix(r.URL.Path, "/editMessageText"):
				return jsonResponse(http.StatusOK, `{"ok":true}`), nil
			default:
				return jsonResponse(http.StatusOK, `{"ok":true}`), nil
			}
		}),
	}

	bot := New("token", 123, t.TempDir(), nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	bot.client = client
	bot.setTelegramPolicyDraft(55, map[contracts.RiskLevel]policies.PolicyGroup{
		contracts.RiskLevelSafeRead:      policies.PolicyGroupAutoAllow,
		contracts.RiskLevelSafeCompute:   policies.PolicyGroupAutoAllow,
		contracts.RiskLevelSensitiveRead: policies.PolicyGroupRequireApprove,
		contracts.RiskLevelExternalWrite: policies.PolicyGroupRequireApprove,
		contracts.RiskLevelLocalWrite:    policies.PolicyGroupRequireApprove,
		contracts.RiskLevelCodeExecution: policies.PolicyGroupRequireApprove,
		contracts.RiskLevelDestructive:   policies.PolicyGroupAutoAllow,
	})

	processed, err := bot.processCallbackQuery(context.Background(), telegramUpdate{
		CallbackQuery: &telegramCallbackQuery{
			ID:   "callback-save",
			From: &telegramUser{ID: 123},
			Message: &telegramMessage{
				MessageID: 42,
				Chat:      telegramChat{ID: 55},
			},
			Data: telegramPolicySettingsCallbackData(telegramPolicySettingsSaveAction, 55, ""),
		},
	})
	if err != nil {
		t.Fatalf("save callback error = %v", err)
	}
	if !processed {
		t.Fatal("expected save callback to be processed")
	}

	var editCall telegramCall
	for _, call := range calls {
		if strings.HasSuffix(call.path, "/editMessageText") {
			editCall = call
			break
		}
	}
	if fmt.Sprint(editCall.payload["text"]) != "🚫 Xóa dữ liệu không thể để Tự động cho phép." {
		t.Fatalf("unexpected validation text: %#v", editCall.payload["text"])
	}
	assertPolicyKeyboardText(t, editCall.payload["reply_markup"], map[contracts.RiskLevel]string{
		contracts.RiskLevelSafeRead:      "Xem danh sách & thông tin tổng quan\n✅ Tự động cho phép",
		contracts.RiskLevelSafeCompute:   "Tóm tắt, phân tích nội dung\n✅ Tự động cho phép",
		contracts.RiskLevelSensitiveRead: "Đọc nội dung riêng tư\n👤 Cần phê duyệt",
		contracts.RiskLevelExternalWrite: "Tạo, chỉnh sửa & gửi đi\n👤 Cần phê duyệt",
		contracts.RiskLevelLocalWrite:    "Tải & lưu file về máy\n👤 Cần phê duyệt",
		contracts.RiskLevelCodeExecution: "Chạy lệnh hệ thống\n👤 Cần phê duyệt",
		contracts.RiskLevelDestructive:   "Xóa dữ liệu\n✅ Tự động cho phép",
	})
}

func assertPolicyKeyboardText(t *testing.T, replyMarkup any, want map[contracts.RiskLevel]string) {
	t.Helper()

	data, err := json.Marshal(replyMarkup)
	if err != nil {
		t.Fatalf("marshal reply markup: %v", err)
	}
	var parsed struct {
		InlineKeyboard [][]map[string]any `json:"inline_keyboard"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal reply markup: %v", err)
	}
	rows := parsed.InlineKeyboard
	if rows == nil {
		t.Fatalf("unexpected inline keyboard: %#v", replyMarkup)
	}
	if len(rows) == 0 {
		if len(want) != 0 {
			t.Fatalf("unexpected empty inline keyboard: %#v", replyMarkup)
		}
		return
	}
	if len(rows) > 0 {
		last := rows[len(rows)-1]
		if len(last) == 1 {
			if text, _ := last[0]["text"].(string); text == "Lưu" {
				rows = rows[:len(rows)-1]
			}
		}
	}
	if len(rows) != len(want) {
		t.Fatalf("unexpected button count: got %d want %d", len(rows), len(want))
	}
	for _, row := range rows {
		if len(row) != 1 {
			t.Fatalf("expected one button per row, got %#v", row)
		}
		text, _ := row[0]["text"].(string)
		found := false
		for level, wantText := range want {
			if text == wantText {
				found = true
				delete(want, level)
				break
			}
		}
		if !found {
			t.Fatalf("unexpected button text: %q", text)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing button texts: %#v", want)
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

func TestTelegramTextFromBlockedByPolicyResponseShowsPolicyMessage(t *testing.T) {
	text := telegramTextFromResponse(contracts.AgentResponse{
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

func TestTelegramTextFromApprovalExpiredResponseShowsExpiredMessage(t *testing.T) {
	text := telegramTextFromResponse(contracts.AgentResponse{
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

func TestTelegramTextFromFailedResponseIncludesTraceLinkWhenConfigured(t *testing.T) {
	t.Setenv("LANGFUSE_HOST", "https://us.cloud.langfuse.com")
	t.Setenv("LANGFUSE_PROJECT_ID", "proj_123")

	text := telegramTextFromResponse(contracts.AgentResponse{
		Status: contracts.AgentStatusFailed,
		Data: map[string]any{
			"trace_id": "trace_abc",
		},
	})

	if !strings.Contains(text, "🔍 Xem chi tiết: https://us.cloud.langfuse.com/project/proj_123/traces/trace_abc") {
		t.Fatalf("missing trace link: %q", text)
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

func TestTelegramTextFromResponsePreservesMultilineFormatting(t *testing.T) {
	message := "Đây là một bộ khung mục lục:\n\nCHƯƠNG 2\n2.1 Cơ sở lý thuyết\n  2.1.1 Khái niệm hệ thống thông tin\n  2.1.2 Khái niệm cơ sở dữ liệu\n\n- Mục 1\n- Mục 2"

	text := telegramTextFromResponse(contracts.AgentResponse{
		Status:  contracts.AgentStatusCompleted,
		Message: message,
	})

	if text != message {
		t.Fatalf("expected response formatting to be preserved, got %q want %q", text, message)
	}
}

func TestTelegramTextFromResponsePreservesCalendarEventLinks(t *testing.T) {
	message := "Đã tạo sự kiện Calendar.\n- Tiêu đề: Sprint Review\n- Link sự kiện: https://calendar.google.com/calendar/event?eid=evt_1"
	text := telegramTextFromResponse(contracts.AgentResponse{
		Status:  contracts.AgentStatusCompleted,
		Message: message,
	})
	want := "Đã tạo sự kiện Calendar.\n- Tiêu đề: Sprint Review\n- Link sự kiện: [Mở sự kiện](https://calendar.google.com/calendar/event?eid=evt_1)"
	if text != want {
		t.Fatalf("expected calendar link text to be compacted, got %q want %q", text, want)
	}
}

func TestTelegramTextFromResponsePrefersFinalMessageOverOutputFallback(t *testing.T) {
	message := "Dưới đây là các thư mục Google Drive bạn đang có:\n1. **Vclaw** - Link: [Mở thư mục](https://drive.google.com/drive/folders/folder_1)"
	text := telegramTextFromResponse(contracts.AgentResponse{
		Status:  contracts.AgentStatusCompleted,
		Message: message,
		Output: &contracts.UserOutput{
			Kind: contracts.UserOutputKindSuccess,
			Text: "Kết quả:\n- Files: Vclaw",
		},
		ToolResults: []contracts.ToolResult{{
			ToolName: "drive.listFiles",
			Success:  true,
			Data: map[string]any{
				"contentForUser": `{"Files":[{"ID":"folder_1","Name":"Vclaw","WebViewLink":"https://drive.google.com/drive/folders/folder_1"}]}`,
			},
		}},
	})

	if text != message {
		t.Fatalf("expected final message to win, got %q want %q", text, message)
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

	for _, want := range []string{"Người nhận:", "vmkqa2@gmail.com", "Tiêu đề:", "Mời họp chiều nay", "Mời bạn tham dự cuộc họp chiều nay.", "Thân mến,", telegramPreBlockOpen, telegramPreBlockClose, telegramFieldOpen, telegramFieldClose} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected email approval text to contain %q, got %q", want, text)
		}
	}
	if strings.Contains(text, "Nội dung email:") {
		t.Fatalf("expected email approval text to omit body label, got %q", text)
	}
}

func TestTelegramApprovalTextShowsCalendarEventDetails(t *testing.T) {
	text := telegramTextFromResponse(contracts.AgentResponse{
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
		"Tiêu đề:", "Họp",
		"Bắt đầu:", "10/06/2026, 08:00 (+07:00)",
		"Kết thúc:", "10/06/2026, 09:30 (+07:00)",
		"Thời lượng:", "1 giờ 30 phút",
		"Người tham gia:", "a@test.com, b@test.com",
		"Địa điểm:", "Phòng A",
		"Ghi chú:", "Chuẩn bị số liệu bán hàng.",
		telegramTextFieldOpen, telegramTextFieldClose, telegramPreBlockOpen, telegramPreBlockClose,
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

func TestTelegramApprovalTextShowsChatMessageDetails(t *testing.T) {
	text := telegramTextFromResponse(contracts.AgentResponse{
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

	for _, want := range []string{"Mọi người vui lòng tăng ca đến 10h đêm nay nhé. Cảm ơn mọi người.", telegramPreBlockOpen, telegramPreBlockClose} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected chat approval text to contain %q, got %q", want, text)
		}
	}
	if strings.Contains(text, "Nội dung:") || strings.Contains(text, "Space:") || strings.Contains(text, "spaces/87bFdyAAAAE") {
		t.Fatalf("expected chat approval text to omit raw space identifier, got %q", text)
	}
}

func TestTelegramApprovalTextForChatListMessagesHidesSpaceID(t *testing.T) {
	text := telegramTextFromResponse(contracts.AgentResponse{
		Status: contracts.AgentStatusApprovalRequired,
		ApprovalRequest: &contracts.ApprovalRequest{
			ApprovalID: "appr_chat_list",
			Summary:    "Cho phép tôi đọc tin nhắn trong Google Chat nhé?",
			ToolCall: contracts.ToolCall{
				ToolName: "chat.listMessages",
				Input: map[string]any{
					"space":      "spaces/AAQAEUb3OG4",
					"maxResults": 50,
				},
			},
		},
	})

	if !strings.Contains(text, "Cuộc trò chuyện: Google Chat đã chọn") {
		t.Fatalf("expected chat list approval to show neutral conversation context, got %q", text)
	}
	if strings.Contains(text, "Space:") || strings.Contains(text, "spaces/AAQAEUb3OG4") {
		t.Fatalf("expected chat list approval to omit raw space identifier, got %q", text)
	}
}

func TestTelegramApprovalTextShowsWorkspaceDefaultForGmailAttachments(t *testing.T) {
	// Relative and absent outputDir should both show the workspace sandbox label —
	// not a Downloads path, which is outside the workspace guard's allowed roots.
	for _, outputDir := range []any{"./", "", nil} {
		input := map[string]any{"messageId": "msg-1"}
		if outputDir != nil {
			input["outputDir"] = outputDir
		}
		text := telegramTextFromResponse(contracts.AgentResponse{
			Status: contracts.AgentStatusApprovalRequired,
			ApprovalRequest: &contracts.ApprovalRequest{
				ApprovalID: "appr_download",
				Summary:    "Tôi cần bạn xác nhận trước khi tải attachment Gmail xuống máy local.",
				ToolCall: contracts.ToolCall{
					ToolName: "gmail.downloadAttachments",
					Input:    input,
				},
			},
		})
		if strings.Contains(text, "~/Downloads") {
			t.Fatalf("outputDir=%v: expected no Downloads path in approval text, got %q", outputDir, text)
		}
		if !strings.Contains(text, "workspace sandbox") {
			t.Fatalf("outputDir=%v: expected workspace sandbox label in approval text, got %q", outputDir, text)
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
			Text: "chay tiep nhe",
		},
	})
	if err != nil {
		t.Fatalf("processUpdate() error = %v", err)
	}
	if !processed {
		t.Fatal("expected update to be processed")
	}
	if handler.calls != 2 {
		t.Fatalf("expected auto-reject plus the new user turn, got %d handler calls", handler.calls)
	}
	if len(handler.receivedAll) != 2 {
		t.Fatalf("expected two handler messages, got %#v", handler.receivedAll)
	}
	if handler.receivedAll[0].Text != "reject appr_existing" {
		t.Fatalf("expected first handler message to reject the pending approval, got %q", handler.receivedAll[0].Text)
	}
	if handler.receivedAll[1].Text != "chay tiep nhe" {
		t.Fatalf("expected second handler message to preserve the new user text, got %q", handler.receivedAll[1].Text)
	}
	if handler.received.Text != "chay tiep nhe" {
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

func TestTelegramRenderHTMLConvertsRawFencedCodeBlock(t *testing.T) {
	rendered := telegramRenderHTML("Vi du:\n```python\nif True:\n    print('hello')\n```")

	if !strings.Contains(rendered, "<pre><code") {
		t.Fatalf("expected raw fenced code to render as html code block, got %q", rendered)
	}
	if !strings.Contains(rendered, "language-python") {
		t.Fatalf("expected raw fenced code to preserve the language class, got %q", rendered)
	}
	if !strings.Contains(rendered, "    print(&#39;hello&#39;)") && !strings.Contains(rendered, "    print('hello')") {
		t.Fatalf("expected raw fenced code indentation to be preserved, got %q", rendered)
	}
}

func TestTelegramRenderHTMLPreservesLeadingSpacesInPlainText(t *testing.T) {
	rendered := telegramRenderHTML("  2.1.1 Khái niệm hệ thống thông tin")

	if !strings.Contains(rendered, "\u00a0\u00a0"+"2.1.1 Khái niệm hệ thống thông tin") {
		t.Fatalf("expected leading spaces to be preserved in plain text, got %q", rendered)
	}
}

func XTestTelegramRenderHTMLConvertsNbspEntitiesToVisibleIndentation(t *testing.T) {
	rendered := telegramRenderHTML("&nbsp;&nbsp;- số")

	if strings.Contains(rendered, "&amp;nbsp;") {
		t.Fatalf("expected nbsp entities to render as spacing, got %q", rendered)
	}
	if strings.Contains(rendered, "&nbsp;") {
		t.Fatalf("expected nbsp entity text to be removed from output, got %q", rendered)
	}
	if !strings.Contains(rendered, "• số") {
		t.Fatalf("expected nbsp entities to become a visible bullet item, got %q", rendered)
	}
}

func TestTelegramRenderHTMLFormatsMarkdownHeading(t *testing.T) {
	rendered := telegramRenderHTML("## Giới thiệu")

	if !strings.Contains(rendered, "<b>GIỚI THIỆU</b>") {
		t.Fatalf("expected markdown heading to render as bold uppercase, got %q", rendered)
	}
}

func TestTelegramRenderHTMLFormatsDriveFolderMarkdown(t *testing.T) {
	rendered := telegramRenderHTML(strings.Join([]string{
		"Dưới đây là các thư mục Google Drive bạn đang có:",
		"",
		"1. **Vclaw**",
		"   - Link: [Mở thư mục](https://drive.google.com/drive/folders/folder_1)",
		"   - Được chỉnh sửa lần cuối: 10 tháng 6, 2026",
	}, "\n"))

	for _, want := range []string{
		"1. <b>Vclaw</b>",
		"   • Link: <a href=\"https://drive.google.com/drive/folders/folder_1\">Mở thư mục</a>",
		"   • Được chỉnh sửa lần cuối: 10 tháng 6, 2026",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered Drive markdown to contain %q, got %q", want, rendered)
		}
	}
	if strings.Contains(rendered, "**Vclaw**") || strings.Contains(rendered, "[Mở thư mục]") {
		t.Fatalf("expected markdown markers to be rendered away, got %q", rendered)
	}
}

func XTestTelegramRenderHTMLConvertsDashListsToBullets(t *testing.T) {
	rendered := telegramRenderHTML("- Mục lớn\n - Mục con")

	if !strings.Contains(rendered, "• Mục lớn") {
		t.Fatalf("expected top-level dash item to become a bullet, got %q", rendered)
	}
	if !strings.Contains(rendered, "\u00a0\u00a0\u00a0\u00a0"+"• Mục con") {
		t.Fatalf("expected nested dash item to become a deeper indented bullet, got %q", rendered)
	}
}

func TestTelegramRenderHTMLConvertsPreBlockMarkers(t *testing.T) {
	rendered := telegramRenderHTML(telegramPreBlock("Chào bạn,\n\nThân mến,\nV-Claw"))

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

func TestTelegramRenderHTMLConvertsTextFieldMarkers(t *testing.T) {
	rendered := telegramRenderHTML(telegramTextField("Bắt đầu", "10/06/2026, 08:00 (+07:00)"))

	if !strings.Contains(rendered, "<b>Bắt đầu:</b> 10/06/2026, 08:00 (+07:00)") {
		t.Fatalf("expected rendered text field without code markup, got %q", rendered)
	}
	if strings.Contains(rendered, "<code>10/06/2026, 08:00 (+07:00)</code>") {
		t.Fatalf("expected text field to avoid code markup, got %q", rendered)
	}
}
