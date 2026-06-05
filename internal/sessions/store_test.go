package sessions

import (
	"context"
	"testing"
	"time"

	"vclaw/internal/providers"
)

func TestInMemoryStoreLoadAppendClearTranscript(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	if err := store.AppendMessage(ctx, "sess_001", providers.Message{Role: providers.MessageRoleUser, Content: "hello"}); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role: providers.MessageRoleAssistant,
		ToolCalls: []providers.ToolCall{{
			ID:        "call_001",
			Name:      "calculator",
			Arguments: map[string]any{"a": 1},
		}},
	}); err != nil {
		t.Fatalf("append assistant message: %v", err)
	}

	transcript, err := store.LoadTranscript(ctx, "sess_001")
	if err != nil {
		t.Fatalf("load transcript: %v", err)
	}
	if len(transcript) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(transcript))
	}

	transcript[1].ToolCalls[0].Arguments["a"] = 99
	reloaded, err := store.LoadTranscript(ctx, "sess_001")
	if err != nil {
		t.Fatalf("reload transcript: %v", err)
	}
	if reloaded[1].ToolCalls[0].Arguments["a"] != 1 {
		t.Fatalf("store should clone messages, got %#v", reloaded[1].ToolCalls[0].Arguments)
	}

	if err := store.ClearSession(ctx, "sess_001"); err != nil {
		t.Fatalf("clear session: %v", err)
	}
	empty, err := store.LoadTranscript(ctx, "sess_001")
	if err != nil {
		t.Fatalf("load cleared transcript: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected cleared transcript, got %#v", empty)
	}
}

func TestInMemoryStoreLoadSaveClearMemory(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	memory := SessionMemory{
		Summary: "user discussed HITL",
		LastActionResults: []ActionResult{{
			ToolName:  "calendar.createEvent",
			Content:   "Event created",
			CreatedAt: time.Date(2026, 6, 4, 10, 0, 0, 0, time.UTC),
		}},
		PendingClarification: &PendingClarification{
			OriginalRequest: "create a meeting",
			Question:        "what time?",
			ToolName:        "calendar.createEvent",
			MissingFields:   []string{"start", "end"},
			PartialInput:    map[string]any{"title": "meeting"},
		},
	}

	if err := store.SaveMemory(ctx, "sess_001", memory); err != nil {
		t.Fatalf("save memory: %v", err)
	}
	loaded, err := store.LoadMemory(ctx, "sess_001")
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	if loaded.Summary != memory.Summary || len(loaded.LastActionResults) != 1 || loaded.PendingClarification == nil {
		t.Fatalf("unexpected memory: %#v", loaded)
	}
	loaded.LastActionResults[0].Content = "mutated"
	loaded.PendingClarification.MissingFields[0] = "mutated"
	loaded.PendingClarification.PartialInput["title"] = "mutated"
	reloaded, err := store.LoadMemory(ctx, "sess_001")
	if err != nil {
		t.Fatalf("reload memory: %v", err)
	}
	if reloaded.LastActionResults[0].Content != "Event created" {
		t.Fatalf("memory should be cloned, got %#v", reloaded.LastActionResults)
	}
	if reloaded.PendingClarification.MissingFields[0] != "start" || reloaded.PendingClarification.PartialInput["title"] != "meeting" {
		t.Fatalf("pending clarification should be cloned, got %#v", reloaded.PendingClarification)
	}

	if err := store.ClearSession(ctx, "sess_001"); err != nil {
		t.Fatalf("clear session: %v", err)
	}
	cleared, err := store.LoadMemory(ctx, "sess_001")
	if err != nil {
		t.Fatalf("load cleared memory: %v", err)
	}
	if cleared.Summary != "" || len(cleared.LastActionResults) != 0 || cleared.PendingClarification != nil {
		t.Fatalf("expected cleared memory, got %#v", cleared)
	}
}
