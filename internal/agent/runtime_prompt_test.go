package agent

import (
	"strings"
	"testing"
	"time"

	"vclaw/internal/agent/reference"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
)

func TestRuntimePromptOrdersSystemMemoryReferenceAndTranscript(t *testing.T) {
	now := time.Date(2026, time.June, 10, 9, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	runtime := NewRuntime(RuntimeConfig{
		Provider: &fakeProvider{},
		Now:      func() time.Time { return now },
	})
	messages := runtime.withRuntimeSystemPrompt(
		[]providers.Message{{Role: providers.MessageRoleUser, Content: "continue"}},
		sessions.SessionMemory{Summary: "remembered summary"},
		&reference.Resolution{
			HasReference:    true,
			ReferenceType:   reference.TypeCalendarEvent,
			ReferenceID:     "event-1",
			Source:          reference.SourceRecentHistory,
			Confidence:      0.9,
			ResolvedContext: map[string]any{"title": "planning"},
		},
		nil,
	)

	if len(messages) != 4 {
		t.Fatalf("expected four prompt messages, got %#v", messages)
	}
	for i := 0; i < 3; i++ {
		if messages[i].Role != providers.MessageRoleSystem {
			t.Fatalf("message %d should be system, got %#v", i, messages[i])
		}
	}
	if !strings.Contains(messages[0].Content, now.Format(time.RFC3339)) {
		t.Fatalf("runtime prompt missing current time: %q", messages[0].Content)
	}
	if !strings.Contains(messages[1].Content, "remembered summary") {
		t.Fatalf("memory prompt missing summary: %q", messages[1].Content)
	}
	if !strings.Contains(messages[2].Content, "event-1") {
		t.Fatalf("reference prompt missing resolved reference: %q", messages[2].Content)
	}
	if messages[3].Role != providers.MessageRoleUser || messages[3].Content != "continue" {
		t.Fatalf("transcript should follow runtime context, got %#v", messages[3])
	}
}
