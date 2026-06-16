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

// fakeLTMemLoader is a test double for longTermMemoryLoader.
type fakeLTMemLoader struct{ content string }

func (f *fakeLTMemLoader) Load() string { return f.content }

func TestLongTermMemoryInjectedAfterBasePrompt(t *testing.T) {
	now := time.Date(2026, time.June, 10, 9, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	r := NewRuntime(RuntimeConfig{
		Provider: &fakeProvider{},
		Now:      func() time.Time { return now },
	})
	r.ltMemLoader = &fakeLTMemLoader{content: "## Bộ nhớ dài hạn\n- Tên: Quang"}

	messages := r.withRuntimeSystemPrompt(
		[]providers.Message{{Role: providers.MessageRoleUser, Content: "hi"}},
		sessions.SessionMemory{},
		nil,
	)
	// Expect: [base_system, long_term_memory, user_message]
	if len(messages) < 3 {
		t.Fatalf("expected at least 3 messages, got %d: %#v", len(messages), messages)
	}
	if !strings.Contains(messages[1].Content, "Bộ nhớ dài hạn") {
		t.Errorf("messages[1] should contain LT memory, got: %q", messages[1].Content)
	}
	if messages[1].Role != providers.MessageRoleSystem {
		t.Errorf("LT memory message should be system role, got: %q", messages[1].Role)
	}
}

func TestLongTermMemoryBeforeSessionMemory(t *testing.T) {
	now := time.Date(2026, time.June, 10, 9, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	r := NewRuntime(RuntimeConfig{
		Provider: &fakeProvider{},
		Now:      func() time.Time { return now },
	})
	r.ltMemLoader = &fakeLTMemLoader{content: "## Bộ nhớ dài hạn\n- project X"}

	messages := r.withRuntimeSystemPrompt(
		[]providers.Message{{Role: providers.MessageRoleUser, Content: "hi"}},
		sessions.SessionMemory{Summary: "session summary ABC"},
		nil,
	)
	ltIdx, sessIdx := -1, -1
	for i, m := range messages {
		if strings.Contains(m.Content, "Bộ nhớ dài hạn") {
			ltIdx = i
		}
		if strings.Contains(m.Content, "session summary ABC") {
			sessIdx = i
		}
	}
	if ltIdx < 0 {
		t.Fatal("LT memory message not found")
	}
	if sessIdx < 0 {
		t.Fatal("session memory message not found")
	}
	if ltIdx >= sessIdx {
		t.Errorf("LT memory (index %d) should come before session memory (index %d)", ltIdx, sessIdx)
	}
}

func TestLongTermMemoryNotInjectedWhenLoaderNil(t *testing.T) {
	now := time.Date(2026, time.June, 10, 9, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	r := NewRuntime(RuntimeConfig{
		Provider: &fakeProvider{},
		Now:      func() time.Time { return now },
	})
	// ltMemLoader is nil by default (no LongMemDir set)

	messages := r.withRuntimeSystemPrompt(
		[]providers.Message{{Role: providers.MessageRoleUser, Content: "hi"}},
		sessions.SessionMemory{Summary: "session summary"},
		nil,
	)
	for _, m := range messages {
		if strings.Contains(m.Content, "Bộ nhớ dài hạn") {
			t.Errorf("LT memory should not be injected when loader is nil, found in: %q", m.Content)
		}
	}
}

func TestRuntimePromptRoutesDriveMoveToMoveFile(t *testing.T) {
	prompt := runtimeSystemPrompt(time.Date(2026, time.June, 11, 18, 30, 0, 0, time.FixedZone("ICT", 7*60*60)))
	for _, want := range []string{
		"di chuyển file X vào folder Y",
		"drive.moveFile",
		"Do not use drive.updateFileMetadata for moving files",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected runtime prompt to contain %q", want)
		}
	}
}

func TestRuntimePromptRoutesDriveFolderCreationToCreateFolder(t *testing.T) {
	prompt := runtimeSystemPrompt(time.Date(2026, time.June, 11, 18, 30, 0, 0, time.FixedZone("ICT", 7*60*60)))
	for _, want := range []string{
		"tạo thư mục X",
		"drive.createFolder",
		"parentIds",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected runtime prompt to contain %q", want)
		}
	}
}
