package sessions

import (
	"context"
	"testing"

	"vclaw/internal/providers"
)

type compactorTestProvider struct {
	generateCalls int
}

func (p *compactorTestProvider) Chat(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{}, nil
}

func (p *compactorTestProvider) Generate(context.Context, *providers.GenerateRequest) (*providers.GenerateResponse, error) {
	p.generateCalls++
	return &providers.GenerateResponse{Text: "summary"}, nil
}

func (*compactorTestProvider) Name() string { return "test" }
func (*compactorTestProvider) Close() error { return nil }

func TestCompactorSummarizesAndKeepsRecentMessages(t *testing.T) {
	provider := &compactorTestProvider{}
	compactor := NewCompactor(provider, CompactorConfig{
		ContextWindow:    30,
		ThresholdRatio:   0.5,
		KeepTokenRatio:   0.2,
		KeepLastMessages: 2,
	}, nil)
	transcript := []providers.Message{
		{Role: providers.MessageRoleUser, Content: "first message with enough text"},
		{Role: providers.MessageRoleAssistant, Content: "second message with enough text"},
		{Role: providers.MessageRoleUser, Content: "recent user"},
		{Role: providers.MessageRoleAssistant, Content: "recent assistant"},
	}
	result, err := compactor.MaybeCompact(context.Background(), "sess", transcript, SessionMemory{}, CompactorGuard{})
	if err != nil {
		t.Fatalf("MaybeCompact: %v", err)
	}
	if !result.Compacted || result.Summary != "summary" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if provider.generateCalls != 1 || len(result.KeptMessages) == 0 {
		t.Fatalf("unexpected compaction calls/kept messages: calls=%d kept=%d", provider.generateCalls, len(result.KeptMessages))
	}
}

func TestCompactorSkipsPendingApproval(t *testing.T) {
	provider := &compactorTestProvider{}
	compactor := NewCompactor(provider, CompactorConfig{ContextWindow: 1, ThresholdRatio: 0.1}, nil)
	result, err := compactor.MaybeCompact(
		context.Background(),
		"sess",
		[]providers.Message{{Role: providers.MessageRoleUser, Content: "large message"}},
		SessionMemory{},
		CompactorGuard{HasPendingApproval: func(string) bool { return true }},
	)
	if err != nil {
		t.Fatalf("MaybeCompact: %v", err)
	}
	if result.SkipReason != "pending_approval" || provider.generateCalls != 0 {
		t.Fatalf("unexpected guarded result: %#v calls=%d", result, provider.generateCalls)
	}
}
