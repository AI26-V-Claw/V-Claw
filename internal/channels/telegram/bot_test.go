package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	if len(calls) < 4 {
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
	last := calls[len(calls)-1]
	if !strings.HasSuffix(last.path, "/editMessageText") || last.payload["text"] != "hello from runtime" {
		t.Fatalf("unexpected final telegram call: %#v", last)
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

func TestRedactTelegramToken(t *testing.T) {
	got := redactTelegramToken("Post https://api.telegram.org/botabc/sendMessage failed", "abc")
	if strings.Contains(got, "abc") {
		t.Fatalf("token was not redacted: %q", got)
	}
}
