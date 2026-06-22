package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"vclaw/internal/contracts"
)

func TestTelegramNewSessionKeepsOldSessionAndNextMessageUsesNewSession(t *testing.T) {
	handler := &fakeHandler{
		outbound: contracts.AgentResponse{
			Status:  contracts.AgentStatusCompleted,
			Message: "ok",
		},
	}
	bot := New("token", 123, t.TempDir(), nil, handler, nil)
	bot.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":42}}`), nil
		case strings.HasSuffix(r.URL.Path, "/editMessageText"):
			return jsonResponse(http.StatusOK, `{"ok":true}`), nil
		default:
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
			return nil, nil
		}
	})}

	if _, err := bot.processUpdate(context.Background(), telegramUpdate{
		UpdateID: 1,
		Message:  &telegramMessage{From: &telegramUser{ID: 123}, Chat: telegramChat{ID: 55}, Text: "/new"},
	}); err != nil {
		t.Fatalf("/new: %v", err)
	}
	if _, err := bot.processUpdate(context.Background(), telegramUpdate{
		UpdateID: 2,
		Message:  &telegramMessage{From: &telegramUser{ID: 123}, Chat: telegramChat{ID: 55}, Text: "Tom tat email tuan nay giup toi"},
	}); err != nil {
		t.Fatalf("message: %v", err)
	}

	if handler.resetSession != "" {
		t.Fatalf("unexpected reset session: %q", handler.resetSession)
	}
	if handler.received.SessionID == telegramLegacySessionID(55) {
		t.Fatalf("expected new active session, got legacy %q", handler.received.SessionID)
	}
	index, err := bot.sessionIndex.List(context.Background(), 55, time.Now().UTC())
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	if len(index.Sessions) != 2 {
		t.Fatalf("expected legacy + new sessions, got %#v", index.Sessions)
	}
	active, ok := activeTelegramSession(index)
	if !ok {
		t.Fatalf("missing active session: %#v", index)
	}
	if active.SessionID != handler.received.SessionID {
		t.Fatalf("active session = %q, handler got %q", active.SessionID, handler.received.SessionID)
	}
	if active.Title != "Tom tat email tuan nay giup toi" {
		t.Fatalf("unexpected active title: %q", active.Title)
	}
}

func TestTelegramSessionsCommandDoesNotExposeSessionID(t *testing.T) {
	handler := &fakeHandler{}
	bot := New("token", 123, t.TempDir(), nil, handler, nil)
	record, err := bot.sessionIndex.Create(context.Background(), 55, time.Now().UTC())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := bot.sessionIndex.Touch(context.Background(), 55, record.SessionID, "Doc tai lieu du an V-Claw", time.Now().UTC()); err != nil {
		t.Fatalf("touch session: %v", err)
	}

	var sentText string
	var sentMarkup string
	bot.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		sentText = fmt.Sprint(payload["text"])
		sentMarkup = fmt.Sprint(payload["reply_markup"])
		return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":42}}`), nil
	})}

	if _, err := bot.processUpdate(context.Background(), telegramUpdate{
		UpdateID: 3,
		Message:  &telegramMessage{From: &telegramUser{ID: 123}, Chat: telegramChat{ID: 55}, Text: "/sessions"},
	}); err != nil {
		t.Fatalf("/sessions: %v", err)
	}
	combined := sentText + " " + sentMarkup
	if strings.Contains(combined, record.SessionID) || strings.Contains(combined, telegramLegacySessionID(55)) {
		t.Fatalf("session UI leaked raw session id: text=%q markup=%q", sentText, sentMarkup)
	}
	if !strings.Contains(sentText, "Doc tai lieu du an V-Claw") {
		t.Fatalf("session title missing from text: %q", sentText)
	}
	if !strings.Contains(sentMarkup, "vclaw:session:select:") || !strings.Contains(sentMarkup, "vclaw:session:delete:") {
		t.Fatalf("session keyboard missing callbacks: %q", sentMarkup)
	}
}

func TestTelegramSessionSelectCallbackChangesActiveSession(t *testing.T) {
	handler := &fakeHandler{}
	bot := New("token", 123, t.TempDir(), nil, handler, nil)
	first, err := bot.sessionIndex.Active(context.Background(), 55, time.Now().UTC())
	if err != nil {
		t.Fatalf("active session: %v", err)
	}
	second, err := bot.sessionIndex.Create(context.Background(), 55, time.Now().Add(time.Minute).UTC())
	if err != nil {
		t.Fatalf("create second session: %v", err)
	}
	if second.Key == first.Key {
		t.Fatalf("expected distinct sessions")
	}
	bot.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/editMessageText"), strings.HasSuffix(r.URL.Path, "/answerCallbackQuery"):
			return jsonResponse(http.StatusOK, `{"ok":true}`), nil
		default:
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
			return nil, nil
		}
	})}

	if _, err := bot.processCallbackQuery(context.Background(), telegramUpdate{
		UpdateID: 4,
		CallbackQuery: &telegramCallbackQuery{
			ID:      "cb-session-select",
			From:    &telegramUser{ID: 123},
			Message: &telegramMessage{MessageID: 99, Chat: telegramChat{ID: 55}},
			Data:    telegramSessionCallbackData("select", first.Key),
		},
	}); err != nil {
		t.Fatalf("select callback: %v", err)
	}
	active, err := bot.sessionIndex.Active(context.Background(), 55, time.Now().UTC())
	if err != nil {
		t.Fatalf("reload active: %v", err)
	}
	if active.Key != first.Key {
		t.Fatalf("active key = %q, want %q", active.Key, first.Key)
	}
}

func TestTelegramSessionDeleteRequiresConfirmAndClearsSession(t *testing.T) {
	handler := &fakeHandler{}
	bot := New("token", 123, t.TempDir(), nil, handler, nil)
	record, err := bot.sessionIndex.Create(context.Background(), 55, time.Now().UTC())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	var callbackAnswers []string
	bot.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			callbackAnswers = append(callbackAnswers, fmt.Sprint(payload["text"]))
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/editMessageText"), strings.HasSuffix(r.URL.Path, "/answerCallbackQuery"):
			return jsonResponse(http.StatusOK, `{"ok":true}`), nil
		default:
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
			return nil, nil
		}
	})}

	if _, err := bot.processCallbackQuery(context.Background(), telegramUpdate{
		UpdateID: 5,
		CallbackQuery: &telegramCallbackQuery{
			ID:      "cb-session-delete",
			From:    &telegramUser{ID: 123},
			Message: &telegramMessage{MessageID: 99, Chat: telegramChat{ID: 55}},
			Data:    telegramSessionCallbackData("delete", record.Key),
		},
	}); err != nil {
		t.Fatalf("delete callback: %v", err)
	}
	if handler.resetSession != "" {
		t.Fatalf("delete prompt should not clear session yet, got %q", handler.resetSession)
	}
	if _, err := bot.processCallbackQuery(context.Background(), telegramUpdate{
		UpdateID: 6,
		CallbackQuery: &telegramCallbackQuery{
			ID:      "cb-session-confirm-delete",
			From:    &telegramUser{ID: 123},
			Message: &telegramMessage{MessageID: 99, Chat: telegramChat{ID: 55}},
			Data:    telegramSessionCallbackData("confirm_delete", record.Key),
		},
	}); err != nil {
		t.Fatalf("confirm delete callback: %v", err)
	}
	if handler.resetSession != record.SessionID {
		t.Fatalf("reset session = %q, want %q", handler.resetSession, record.SessionID)
	}
	index, err := bot.sessionIndex.List(context.Background(), 55, time.Now().UTC())
	if err != nil {
		t.Fatalf("reload index: %v", err)
	}
	for _, session := range index.Sessions {
		if session.Key == record.Key {
			t.Fatalf("deleted session still visible: %#v", index.Sessions)
		}
	}
	if len(callbackAnswers) < 2 {
		t.Fatalf("expected delete and confirm callback answers, got %#v", callbackAnswers)
	}
}

func TestTelegramNewSessionBlockedWhenApprovalPending(t *testing.T) {
	handler := &fakeHandler{}
	bot := New("token", 123, t.TempDir(), nil, handler, nil)
	bot.state.registerApproval(telegramApprovalContext{
		ApprovalID: "appr_existing",
		SessionID:  telegramLegacySessionID(55),
		ChatID:     55,
		MessageID:  42,
	})
	bot.client = &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{"ok":true,"result":{"message_id":42}}`), nil
	})}

	if _, err := bot.processUpdate(context.Background(), telegramUpdate{
		UpdateID: 7,
		Message:  &telegramMessage{From: &telegramUser{ID: 123}, Chat: telegramChat{ID: 55}, Text: "/new"},
	}); err != nil {
		t.Fatalf("/new with pending approval: %v", err)
	}
	if handler.resetSession != "" || handler.calls != 0 {
		t.Fatalf("pending approval should block /new, reset=%q calls=%d", handler.resetSession, handler.calls)
	}
	index, err := bot.sessionIndex.List(context.Background(), 55, time.Now().UTC())
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	if len(index.Sessions) != 1 || index.Sessions[0].Key != "legacy" {
		t.Fatalf("pending /new should not create a new session, got %#v", index.Sessions)
	}
}
