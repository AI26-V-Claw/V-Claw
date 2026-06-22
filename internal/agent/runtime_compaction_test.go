package agent

import (
	"context"
	"sync/atomic"
	"testing"

	"vclaw/internal/contracts"
	"vclaw/internal/longmem"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
)

// fakeLTMemFlusher records Flush calls for assertions.
type fakeLTMemFlusher struct {
	calls       atomic.Int32
	habitCalls  atomic.Int32
	lastSummary string
	err         error
}

func (f *fakeLTMemFlusher) Flush(_ context.Context, summary string) error {
	f.calls.Add(1)
	f.lastSummary = summary
	return f.err
}

func (f *fakeLTMemFlusher) RecordRepeatedHabits(_ context.Context, _ longmem.HabitInput) error {
	f.habitCalls.Add(1)
	return f.err
}

type blockingCompactionProvider struct {
	started chan struct{}
	release chan struct{}
}

func (p *blockingCompactionProvider) Chat(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{}, nil
}

func (p *blockingCompactionProvider) Generate(context.Context, *providers.GenerateRequest) (*providers.GenerateResponse, error) {
	close(p.started)
	<-p.release
	return &providers.GenerateResponse{Text: "summary"}, nil
}

func (*blockingCompactionProvider) Name() string { return "test" }
func (*blockingCompactionProvider) Close() error { return nil }

func TestRuntimeCompactionDoesNotOverwriteNewTranscriptMessages(t *testing.T) {
	ctx := context.Background()
	store := sessions.NewInMemoryStore()
	sessionID := "compaction-race"
	for _, message := range []providers.Message{
		{Role: providers.MessageRoleUser, Content: "first message with enough text to compact"},
		{Role: providers.MessageRoleAssistant, Content: "second message with enough text to compact"},
		{Role: providers.MessageRoleUser, Content: "third message with enough text to compact"},
		{Role: providers.MessageRoleAssistant, Content: "fourth message with enough text to compact"},
	} {
		if err := store.AppendMessage(ctx, sessionID, message); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	provider := &blockingCompactionProvider{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:     provider,
		SessionStore: store,
		Compactor: sessions.NewCompactor(provider, sessions.CompactorConfig{
			ContextWindow:    30,
			ThresholdRatio:   0.5,
			KeepTokenRatio:   0.2,
			KeepLastMessages: 2,
		}, nil),
	})
	done := make(chan struct{})
	go func() {
		runtime.maybeCompactAsync(sessionID)
		close(done)
	}()

	<-provider.started
	newMessage := providers.Message{Role: providers.MessageRoleUser, Content: "message appended while compaction is running"}
	if err := store.AppendMessage(ctx, sessionID, newMessage); err != nil {
		t.Fatalf("append concurrent message: %v", err)
	}
	close(provider.release)
	<-done

	transcript, err := store.LoadTranscript(ctx, sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript: %v", err)
	}
	if len(transcript) != 5 || transcript[len(transcript)-1].Content != newMessage.Content {
		t.Fatalf("compaction overwrote concurrent transcript update: %#v", transcript)
	}
	memory, err := store.LoadMemory(ctx, sessionID)
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if memory.Summary != "" {
		t.Fatalf("summary should not persist when compare-and-set fails: %#v", memory)
	}
}

func TestMaybeCompactAsyncCallsFlusher(t *testing.T) {
	ctx := context.Background()
	store := sessions.NewInMemoryStore()
	sessionID := "sess_flush"

	// Seed enough messages to trigger compaction.
	for i := 0; i < 6; i++ {
		_ = store.AppendMessage(ctx, sessionID, providers.Message{
			Role:    providers.MessageRoleUser,
			Content: "message with enough words to push token count above threshold for compaction",
		})
		_ = store.AppendMessage(ctx, sessionID, providers.Message{
			Role:    providers.MessageRoleAssistant,
			Content: "reply with enough words to push token count above threshold for compaction",
		})
	}

	flusher := &fakeLTMemFlusher{}
	provider := &fakeProvider{
		responses: []providers.ChatResponse{
			{Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "compact summary"}},
		},
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:     provider,
		SessionStore: store,
		Compactor: sessions.NewCompactor(provider, sessions.CompactorConfig{
			ContextWindow:    30,
			ThresholdRatio:   0.5,
			KeepTokenRatio:   0.2,
			KeepLastMessages: 2,
		}, nil),
	})
	runtime.ltMemFlusher = flusher

	runtime.maybeCompactAsync(sessionID)

	if flusher.calls.Load() != 1 {
		t.Errorf("expected Flush called once, got %d", flusher.calls.Load())
	}
	if flusher.lastSummary == "" {
		t.Error("Flush called with empty summary")
	}
}

func TestMaybeCompactAsyncFlusherErrorDoesNotFailCompaction(t *testing.T) {
	ctx := context.Background()
	store := sessions.NewInMemoryStore()
	sessionID := "sess_flush_err"

	for i := 0; i < 6; i++ {
		_ = store.AppendMessage(ctx, sessionID, providers.Message{
			Role:    providers.MessageRoleUser,
			Content: "message with enough words to push token count above threshold for compaction",
		})
		_ = store.AppendMessage(ctx, sessionID, providers.Message{
			Role:    providers.MessageRoleAssistant,
			Content: "reply with enough words to push token count above threshold for compaction",
		})
	}

	flusher := &fakeLTMemFlusher{err: context.DeadlineExceeded}
	provider := &fakeProvider{
		responses: []providers.ChatResponse{
			{Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "compact summary"}},
		},
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:     provider,
		SessionStore: store,
		Compactor: sessions.NewCompactor(provider, sessions.CompactorConfig{
			ContextWindow:    30,
			ThresholdRatio:   0.5,
			KeepTokenRatio:   0.2,
			KeepLastMessages: 2,
		}, nil),
	})
	runtime.ltMemFlusher = flusher

	runtime.maybeCompactAsync(sessionID)

	// Compaction should have succeeded even though flusher errored.
	memory, err := store.LoadMemory(ctx, sessionID)
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if memory.Summary == "" {
		t.Error("compaction summary should be saved even when flusher errors")
	}
}

func TestRuntimeRecordsRepeatedHabitsAfterUserMessageAppend(t *testing.T) {
	ctx := context.Background()
	store := sessions.NewInMemoryStore()
	sessionID := "sess_repeated_habit"
	if err := store.AppendMessage(ctx, sessionID, providers.Message{
		Role:    providers.MessageRoleUser,
		Content: "Xem mail lúc 8h sáng giúp tôi",
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	flusher := &fakeLTMemFlusher{}
	runtime := NewRuntime(RuntimeConfig{
		Provider:     &fakeProvider{},
		Registry:     tools.NewToolRegistry(),
		SessionStore: store,
	})
	runtime.ltMemFlusher = flusher

	response, err := runtime.Run(ctx, contracts.UserMessage{
		RequestID: "req_repeated_habit",
		SessionID: sessionID,
		Channel:   "telegram",
		Text:      "Check email lúc 8h sáng",
		Timestamp: runtime.now(),
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("unexpected response status: %s", response.Status)
	}
	if flusher.habitCalls.Load() == 0 {
		t.Fatal("expected repeated habit recorder to be called")
	}
}

func TestRefreshSessionSummaryDoesNotOverwriteLLMSummary(t *testing.T) {
	ctx := context.Background()
	store := sessions.NewInMemoryStore()
	sessionID := "sess_summary_overwrite"

	// Build a transcript long enough for buildExtractiveSessionSummary to produce output
	// (needs more than 12 messages — the recentWindow default).
	for i := 0; i < 16; i++ {
		_ = store.AppendMessage(ctx, sessionID, providers.Message{
			Role:    providers.MessageRoleUser,
			Content: "user message with enough words for extractive summary to include it",
		})
		_ = store.AppendMessage(ctx, sessionID, providers.Message{
			Role:    providers.MessageRoleAssistant,
			Content: "assistant reply with enough words for extractive summary to include it",
		})
	}

	// Simulate a compactor-written LLM summary already present in memory.
	const llmSummary = "LLM-generated summary: user discussed the project timeline in detail"
	_ = store.SaveMemory(ctx, sessionID, sessions.SessionMemory{Summary: llmSummary})

	transcript, err := store.LoadTranscript(ctx, sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript: %v", err)
	}

	runtime := NewRuntime(RuntimeConfig{
		Provider:     &blockingCompactionProvider{started: make(chan struct{}), release: make(chan struct{})},
		SessionStore: store,
	})

	if errShape := runtime.refreshSessionSummary(ctx, sessionID, transcript); errShape != nil {
		t.Fatalf("refreshSessionSummary: %v", errShape)
	}

	memory, err := store.LoadMemory(ctx, sessionID)
	if err != nil {
		t.Fatalf("LoadMemory: %v", err)
	}
	if memory.Summary != llmSummary {
		t.Fatalf("LLM summary was overwritten by heuristic\ngot:  %q\nwant: %q", memory.Summary, llmSummary)
	}
}
