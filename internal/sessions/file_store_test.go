package sessions

import (
	"context"
	"strings"
	"sync"
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

func TestFileStoreSaveMemoryOverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	store, _ := NewFileStore(dir)

	if err := store.SaveMemory(ctx, "sess", SessionMemory{Summary: "first"}); err != nil {
		t.Fatalf("initial SaveMemory: %v", err)
	}
	if err := store.SaveMemory(ctx, "sess", SessionMemory{Summary: "second"}); err != nil {
		t.Fatalf("overwrite SaveMemory: %v", err)
	}

	loaded, err := store.LoadMemory(ctx, "sess")
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if loaded.Summary != "second" {
		t.Fatalf("expected overwritten summary, got %q", loaded.Summary)
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
	if got := sanitizeSessionID("telegram_123"); got != "telegram_123" {
		t.Fatalf("safe session id changed: %q", got)
	}
	if got := sanitizeSessionID(""); got != "_empty" {
		t.Fatalf("empty session id = %q", got)
	}
	got := sanitizeSessionID("user/../../etc")
	if !strings.HasPrefix(got, "user_______etc-") {
		t.Fatalf("unsafe session id missing readable prefix and hash: %q", got)
	}
	if sanitizeSessionID("a/b") == sanitizeSessionID("a?b") {
		t.Fatal("distinct unsafe session ids collided")
	}
}

func TestFileStoreConcurrentInstancesDoNotLoseAppends(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	first, _ := NewFileStore(dir)
	second, _ := NewFileStore(dir)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, store := range []*FileStore{first, second} {
		i, store := i, store
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := store.AppendMessage(ctx, "shared", providers.Message{
				Role:    providers.MessageRoleUser,
				Content: string(rune('a' + i)),
			}); err != nil {
				t.Errorf("AppendMessage: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	messages, err := first.LoadTranscript(ctx, "shared")
	if err != nil {
		t.Fatalf("LoadTranscript: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %v, want two appends", messages)
	}
}

func TestFileStoreCompareAndSetRejectsChangedTranscript(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	ctx := context.Background()
	original := []providers.Message{{Role: providers.MessageRoleUser, Content: "old"}}
	if err := store.SetTranscript(ctx, "sess", original); err != nil {
		t.Fatalf("SetTranscript: %v", err)
	}
	if err := store.AppendMessage(ctx, "sess", providers.Message{Role: providers.MessageRoleUser, Content: "new"}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	replaced, err := store.ReplaceTranscriptIfUnchanged(ctx, "sess", original, nil)
	if err != nil {
		t.Fatalf("ReplaceTranscriptIfUnchanged: %v", err)
	}
	if replaced {
		t.Fatal("changed transcript should not be replaced")
	}
	messages, _ := store.LoadTranscript(ctx, "sess")
	if len(messages) != 2 || messages[1].Content != "new" {
		t.Fatalf("new append was lost: %#v", messages)
	}
}
