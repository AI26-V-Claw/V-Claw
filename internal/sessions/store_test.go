package sessions

import (
	"context"
	"testing"

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
