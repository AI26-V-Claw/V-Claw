package agent

import (
	"strings"
	"testing"
	"time"

	"vclaw/internal/providers"
	"vclaw/internal/sessions"
)

func repeatTokens(prefix string, approxTokens int) string {
	// EstimateTokens ≈ runes/3, so 3 runes per desired token.
	var b strings.Builder
	for b.Len() < approxTokens*3 {
		b.WriteString(prefix)
		b.WriteString(" ")
	}
	return b.String()
}

func TestDefaultContextBudgetScalesWithWindow(t *testing.T) {
	small := DefaultContextBudget(32_000)
	large := DefaultContextBudget(128_000)
	if small.Available() <= 0 || large.Available() <= 0 {
		t.Fatalf("available budget must be positive: small=%d large=%d", small.Available(), large.Available())
	}
	if large.LongTermMemory <= small.LongTermMemory {
		t.Fatalf("larger window should allocate more long-term memory budget: %d vs %d", large.LongTermMemory, small.LongTermMemory)
	}
	if small.Available() >= small.ContextWindow {
		t.Fatalf("available must be below the window after output reserve")
	}
}

func TestDefaultContextBudgetZeroFallsBackTo128k(t *testing.T) {
	b := DefaultContextBudget(0)
	if b.ContextWindow != 128_000 {
		t.Fatalf("expected fallback window 128000, got %d", b.ContextWindow)
	}
}

func TestNormalizedFillsPartialOverride(t *testing.T) {
	b := ContextBudget{ContextWindow: 64_000, Summary: 1234}.normalized()
	if b.Summary != 1234 {
		t.Fatalf("explicit Summary override should be preserved, got %d", b.Summary)
	}
	if b.LongTermMemory <= 0 || b.OutputReserve <= 0 || b.References <= 0 || b.ActionResults <= 0 {
		t.Fatalf("unset fields must be filled from defaults: %+v", b)
	}
}

func TestTruncateToTokenBudget(t *testing.T) {
	text := repeatTokens("alpha", 500)
	out := truncateToTokenBudget(text, 50)
	if sessions.EstimateTokens(out) > 60 {
		t.Fatalf("truncated text should be within budget margin, got %d tokens", sessions.EstimateTokens(out))
	}
	if !strings.Contains(out, "truncated to fit context budget") {
		t.Fatalf("expected truncation marker, got %q", out)
	}
	// Small text is returned unchanged.
	if got := truncateToTokenBudget("short", 50); got != "short" {
		t.Fatalf("small text should pass through, got %q", got)
	}
}

func TestTruncateMemoryKeepsHeadingsAndQueryFacts(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Profile\n")
	b.WriteString("The user works on the Helios project with deadline in July.\n")
	// Padding unrelated content to force truncation.
	for i := 0; i < 200; i++ {
		b.WriteString("Unrelated filler note about random topics and chores.\n")
	}
	b.WriteString("# Contacts\n")
	b.WriteString("Primary contact for Helios is Mai.\n")

	out := truncateMemoryByTokens(b.String(), 60, "what is the Helios deadline")
	if sessions.EstimateTokens(out) > 80 {
		t.Fatalf("memory should be trimmed to budget, got %d tokens", sessions.EstimateTokens(out))
	}
	if !strings.Contains(out, "# Profile") || !strings.Contains(out, "# Contacts") {
		t.Fatalf("headings should be preserved: %q", out)
	}
	if !strings.Contains(out, "Helios project") {
		t.Fatalf("query-relevant fact should be retained: %q", out)
	}
}

func TestSelectTranscriptKeepsLatestUserWhenOversized(t *testing.T) {
	huge := repeatTokens("question", 5000)
	transcript := []providers.Message{
		{Role: providers.MessageRoleUser, Content: "older message"},
		{Role: providers.MessageRoleAssistant, Content: "older reply"},
		{Role: providers.MessageRoleUser, Content: huge},
	}
	out := selectTranscriptWithinBudget(transcript, 100)
	if len(out) == 0 {
		t.Fatalf("latest user message must always be kept")
	}
	last := out[len(out)-1]
	if last.Role != providers.MessageRoleUser {
		t.Fatalf("last kept message should be the user request, got %s", last.Role)
	}
	if sessions.EstimateMessagesTokens(out) > 160 {
		t.Fatalf("selection must respect budget, got %d tokens", sessions.EstimateMessagesTokens(out))
	}
}

func TestSelectTranscriptNewestFirstWithinBudget(t *testing.T) {
	transcript := []providers.Message{
		{Role: providers.MessageRoleUser, Content: repeatTokens("one", 40)},
		{Role: providers.MessageRoleAssistant, Content: repeatTokens("two", 40)},
		{Role: providers.MessageRoleUser, Content: repeatTokens("three", 40)},
	}
	// Budget fits ~2 messages.
	out := selectTranscriptWithinBudget(transcript, 90)
	if len(out) == 0 {
		t.Fatalf("expected at least the latest message")
	}
	// The newest message must be present.
	if !strings.Contains(out[len(out)-1].Content, "three") {
		t.Fatalf("newest message should be retained, got %q", out[len(out)-1].Content)
	}
}

func TestSelectTranscriptKeepsToolCallSequenceAtomic(t *testing.T) {
	toolBlock := []providers.Message{
		{Role: providers.MessageRoleAssistant, ToolCalls: []providers.ToolCall{
			{ID: "call_events", Name: "calendar.listEvents", Arguments: map[string]any{"timeMin": "2026-06-25T00:00:00+07:00"}},
			{ID: "call_mail", Name: "gmail.listEmails", Arguments: map[string]any{"query": "demo"}},
		}},
		{Role: providers.MessageRoleTool, ToolCallID: "call_events", Content: repeatTokens("calendar result", 100)},
		{Role: providers.MessageRoleTool, ToolCallID: "call_mail", Content: repeatTokens("gmail result", 100)},
	}
	transcript := append([]providers.Message{
		{Role: providers.MessageRoleUser, Content: repeatTokens("old", 200)},
		{Role: providers.MessageRoleAssistant, Content: repeatTokens("old reply", 200)},
		{Role: providers.MessageRoleUser, Content: "current request"},
	}, toolBlock...)

	budget := messageCost([]providers.Message{{Role: providers.MessageRoleUser, Content: "current request"}}) +
		messageCost(toolBlock) + 20
	out := selectTranscriptWithinBudget(transcript, budget)
	out = sanitizeProviderTranscriptForToolProtocol(out)

	if len(out) < 4 {
		t.Fatalf("expected current user plus complete tool sequence, got %#v", out)
	}
	if out[len(out)-3].Role != providers.MessageRoleAssistant || len(out[len(out)-3].ToolCalls) != 2 {
		t.Fatalf("assistant tool call block was not retained atomically: %#v", out)
	}
	if out[len(out)-2].Role != providers.MessageRoleTool || out[len(out)-1].Role != providers.MessageRoleTool {
		t.Fatalf("tool results should remain adjacent to assistant tool calls: %#v", out)
	}
	if out[len(out)-2].ToolCallID != "call_events" || out[len(out)-1].ToolCallID != "call_mail" {
		t.Fatalf("wrong tool results retained: %#v", out)
	}
}

// newBudgetedRuntime builds a Runtime with a small context window so assembly
// budgeting is exercised without needing huge inputs.
func newBudgetedRuntime(t *testing.T, contextWindow int, ltm string) *Runtime {
	t.Helper()
	now := time.Date(2026, time.June, 10, 9, 0, 0, 0, time.UTC)
	rt := NewRuntime(RuntimeConfig{
		Provider:      &fakeProvider{},
		Now:           func() time.Time { return now },
		ContextWindow: contextWindow,
	})
	if ltm != "" {
		rt.ltMemLoader = &fakeLTMemLoader{content: ltm}
	}
	return rt
}

func TestAssembledContextStaysWithinWindow(t *testing.T) {
	for _, window := range []int{32_000, 128_000} {
		rt := newBudgetedRuntime(t, window, repeatTokens("memory fact about projects", 20_000))
		// Build a transcript with an oversized user message.
		transcript := []providers.Message{
			{Role: providers.MessageRoleUser, Content: repeatTokens("history", 30_000)},
			{Role: providers.MessageRoleAssistant, Content: repeatTokens("reply", 30_000)},
			{Role: providers.MessageRoleUser, Content: repeatTokens("current huge request", 40_000)},
		}
		memory := sessions.SessionMemory{
			Summary: repeatTokens("summary", 20_000),
		}
		messages := rt.withRuntimeSystemPromptOptions(transcript, memory, nil, runtimePromptOptions{IncludeLongTermMemory: true})
		budget := rt.contextBudget.normalized()
		total := sessions.EstimateMessagesTokens(messages)
		if total > budget.Available() {
			t.Fatalf("window=%d assembled context %d tokens exceeds available %d", window, total, budget.Available())
		}
	}
}

func TestAssembledContextAlwaysKeepsCurrentUserMessage(t *testing.T) {
	rt := newBudgetedRuntime(t, 32_000, repeatTokens("memory", 50_000))
	marker := "PLEASE_ANSWER_THIS_SPECIFIC_REQUEST"
	transcript := []providers.Message{
		{Role: providers.MessageRoleUser, Content: repeatTokens("old", 5_000)},
		{Role: providers.MessageRoleUser, Content: marker},
	}
	messages := rt.withRuntimeSystemPromptOptions(transcript, sessions.SessionMemory{}, nil, runtimePromptOptions{IncludeLongTermMemory: true})
	found := false
	for _, m := range messages {
		if strings.Contains(m.Content, marker) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("current user message must never be dropped from assembled context")
	}
}

func TestAssembledContextTrimsLargeMemoryButKeepsQueryFacts(t *testing.T) {
	var mem strings.Builder
	mem.WriteString("# Projects\n")
	mem.WriteString("Helios ships in July; owner is Mai.\n")
	for i := 0; i < 5000; i++ {
		mem.WriteString("Filler note unrelated to the question.\n")
	}
	rt := newBudgetedRuntime(t, 32_000, mem.String())
	transcript := []providers.Message{
		{Role: providers.MessageRoleUser, Content: "When does Helios ship?"},
	}
	messages := rt.withRuntimeSystemPromptOptions(transcript, sessions.SessionMemory{}, nil, runtimePromptOptions{IncludeLongTermMemory: true})
	var memMsg string
	for _, m := range messages {
		if strings.Contains(m.Content, "Helios") {
			memMsg = m.Content
		}
	}
	if memMsg == "" {
		t.Fatalf("expected long-term memory with Helios fact to survive trimming")
	}
	budget := rt.contextBudget.normalized()
	if sessions.EstimateMessagesTokens(messages) > budget.Available() {
		t.Fatalf("assembled context with large memory exceeded budget")
	}
}
