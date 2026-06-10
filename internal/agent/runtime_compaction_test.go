package agent

import (
	"context"
	"testing"

	"vclaw/internal/providers"
	"vclaw/internal/sessions"
)

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
