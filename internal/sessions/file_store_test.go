package sessions

import (
	"context"
	"testing"

	"vclaw/internal/providers"
)

func TestFileStoreTranscriptPersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s1, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	msg := providers.Message{Role: providers.MessageRoleUser, Content: "hello"}
	if err := s1.AppendMessage(ctx, "s1", msg); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// New instance, same dir — simulates process restart.
	s2, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	messages, err := s2.LoadTranscript(ctx, "s1")
	if err != nil {
		t.Fatalf("LoadTranscript: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "hello" {
		t.Fatalf("expected persisted message, got %v", messages)
	}
}

func TestFileStoreSetTranscriptOverwrites(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	s, _ := NewFileStore(dir)

	for _, content := range []string{"a", "b", "c"} {
		_ = s.AppendMessage(ctx, "sess", providers.Message{Role: providers.MessageRoleUser, Content: content})
	}
	kept := []providers.Message{{Role: providers.MessageRoleUser, Content: "c"}}
	if err := s.SetTranscript(ctx, "sess", kept); err != nil {
		t.Fatalf("SetTranscript: %v", err)
	}
	messages, _ := s.LoadTranscript(ctx, "sess")
	if len(messages) != 1 || messages[0].Content != "c" {
		t.Fatalf("expected 1 message after SetTranscript, got %v", messages)
	}
}

func TestFileStoreMemoryPersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s1, _ := NewFileStore(dir)
	mem := SessionMemory{Summary: "prior work summary"}
	if err := s1.SaveMemory(ctx, "sess", mem); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}

	s2, _ := NewFileStore(dir)
	loaded, err := s2.LoadMemory(ctx, "sess")
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if loaded.Summary != "prior work summary" {
		t.Fatalf("expected persisted summary, got %q", loaded.Summary)
	}
}

func TestFileStoreClearSessionRemovesFiles(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	s, _ := NewFileStore(dir)

	_ = s.AppendMessage(ctx, "sess", providers.Message{Role: providers.MessageRoleUser, Content: "hi"})
	_ = s.SaveMemory(ctx, "sess", SessionMemory{Summary: "x"})
	if err := s.ClearSession(ctx, "sess"); err != nil {
		t.Fatalf("ClearSession: %v", err)
	}

	messages, _ := s.LoadTranscript(ctx, "sess")
	if len(messages) != 0 {
		t.Fatalf("expected empty transcript after clear, got %v", messages)
	}
	memory, _ := s.LoadMemory(ctx, "sess")
	if memory.Summary != "" {
		t.Fatalf("expected empty memory after clear, got %q", memory.Summary)
	}
}

func TestFileStoreSanitizeSessionID(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"telegram_123", "telegram_123"},
		{"slack-C0B80", "slack-C0B80"},
		{"user/../../etc", "user_______etc"},
		{"", "_empty"},
	}
	for _, c := range cases {
		got := sanitizeSessionID(c.input)
		if got != c.want {
			t.Errorf("sanitizeSessionID(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
