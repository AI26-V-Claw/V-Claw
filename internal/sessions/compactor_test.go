package sessions

import (
	"context"
	"strings"
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

// capturingCompactorTestProvider captures the last user prompt for assertion.
type capturingCompactorTestProvider struct {
	generateCalls  int
	lastUserPrompt string
}

func (p *capturingCompactorTestProvider) Chat(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{}, nil
}

func (p *capturingCompactorTestProvider) Generate(_ context.Context, req *providers.GenerateRequest) (*providers.GenerateResponse, error) {
	p.generateCalls++
	p.lastUserPrompt = req.UserPrompt
	return &providers.GenerateResponse{Text: "summary"}, nil
}

func (*capturingCompactorTestProvider) Name() string { return "test" }
func (*capturingCompactorTestProvider) Close() error { return nil }

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

func TestPendingClarificationSurvivesCompaction(t *testing.T) {
	provider := &capturingCompactorTestProvider{}
	compactor := NewCompactor(provider, CompactorConfig{
		ContextWindow:    30,
		ThresholdRatio:   0.5,
		KeepTokenRatio:   0.0,
		KeepLastMessages: 1,
	}, nil)
	transcript := []providers.Message{
		{Role: providers.MessageRoleUser, Content: "first message with enough text to trigger compaction"},
		{Role: providers.MessageRoleAssistant, Content: "second message with enough text to trigger compaction"},
		{Role: providers.MessageRoleUser, Content: "recent user asking about meeting"},
	}
	question := "what time should the meeting start?"
	memory := SessionMemory{
		PendingClarification: &PendingClarification{
			OriginalRequest: "create a meeting",
			Question:        question,
		},
	}
	result, err := compactor.MaybeCompact(context.Background(), "sess", transcript, memory, CompactorGuard{})
	if err != nil {
		t.Fatalf("MaybeCompact: %v", err)
	}
	if !result.Compacted {
		t.Fatalf("expected compaction, got skip reason: %s", result.SkipReason)
	}
	if !strings.Contains(provider.lastUserPrompt, question) {
		t.Fatalf("pending clarification question not found in compaction prompt:\n%s", provider.lastUserPrompt)
	}
}

func TestSummaryMergedCapAfterCompaction(t *testing.T) {
	provider := &capturingCompactorTestProvider{}
	compactor := NewCompactor(provider, CompactorConfig{
		ContextWindow:         30,
		ThresholdRatio:        0.5,
		KeepTokenRatio:        0.0,
		KeepLastMessages:      1,
		MaxMergedSummaryBytes: 100,
	}, nil)
	transcript := []providers.Message{
		{Role: providers.MessageRoleUser, Content: "first message with enough text to trigger compaction"},
		{Role: providers.MessageRoleAssistant, Content: "second message with enough text to trigger compaction"},
		{Role: providers.MessageRoleUser, Content: "recent"},
	}
	// Existing summary fills most of the cap; merged result should not exceed 100 bytes.
	existingSummary := strings.Repeat("x", 90)
	memory := SessionMemory{Summary: existingSummary}

	result, err := compactor.MaybeCompact(context.Background(), "sess", transcript, memory, CompactorGuard{})
	if err != nil {
		t.Fatalf("MaybeCompact: %v", err)
	}
	if !result.Compacted {
		t.Fatalf("expected compaction, got: %s", result.SkipReason)
	}
	if len(result.Summary) > 100 {
		t.Fatalf("merged summary exceeds cap: got %d bytes, want <= 100", len(result.Summary))
	}
	// The new LLM summary ("summary") must be preserved intact at the end.
	if !strings.HasSuffix(result.Summary, "summary") {
		t.Fatalf("new LLM summary must appear at end of merged result, got: %q", result.Summary)
	}
}

func TestMessageCountThresholdTriggersCompaction(t *testing.T) {
	provider := &compactorTestProvider{}
	compactor := NewCompactor(provider, CompactorConfig{
		ContextWindow:         100_000, // very high — token threshold never triggers
		ThresholdRatio:        0.90,
		KeepTokenRatio:        0.0,
		KeepLastMessages:      2,
		MessageCountThreshold: 5,
	}, nil)

	// 5 short messages — token threshold never triggers, but message count does.
	transcript := make([]providers.Message, 5)
	for i := range transcript {
		transcript[i] = providers.Message{Role: providers.MessageRoleUser, Content: "hi"}
	}
	result, err := compactor.MaybeCompact(context.Background(), "sess", transcript, SessionMemory{}, CompactorGuard{})
	if err != nil {
		t.Fatalf("MaybeCompact at threshold: %v", err)
	}
	if !result.Compacted {
		t.Fatalf("expected compaction triggered by message count, got skip reason: %s", result.SkipReason)
	}

	// 4 messages — below both thresholds, must skip.
	provider.generateCalls = 0
	result2, err := compactor.MaybeCompact(context.Background(), "sess2", transcript[:4], SessionMemory{}, CompactorGuard{})
	if err != nil {
		t.Fatalf("MaybeCompact below threshold: %v", err)
	}
	if result2.Compacted || result2.SkipReason != "below_threshold" {
		t.Fatalf("expected below_threshold skip for 4 messages, got: %#v", result2)
	}
}

