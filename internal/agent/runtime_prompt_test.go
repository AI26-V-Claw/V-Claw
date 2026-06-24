package agent

import (
	"strings"
	"testing"
	"time"

	"vclaw/internal/agent/reference"
	"vclaw/internal/knowledge"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	drivetool "vclaw/internal/tools/office/drive"
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

func TestLongTermMemoryCanBeSuppressedForFreshWorkspaceRead(t *testing.T) {
	r := NewRuntime(RuntimeConfig{Provider: &fakeProvider{}})
	r.ltMemLoader = &fakeLTMemLoader{content: "## Memory\n- Deleted Event should not appear"}

	messages := r.withRuntimeSystemPromptOptions(
		[]providers.Message{{Role: providers.MessageRoleUser, Content: "lich tuan nay co gi"}},
		sessions.SessionMemory{Summary: "session context remains available"},
		nil,
		runtimePromptOptions{IncludeLongTermMemory: false},
	)
	joined := providerMessagesContent(messages)
	if strings.Contains(joined, "Deleted Event should not appear") {
		t.Fatalf("long-term memory should be suppressed, got: %s", joined)
	}
	if !strings.Contains(joined, "session context remains available") {
		t.Fatalf("session memory should still be included by this prompt option, got: %s", joined)
	}
}

func TestLinkedKnowledgePromptInjectedAsContextOnly(t *testing.T) {
	r := NewRuntime(RuntimeConfig{Provider: &fakeProvider{}})
	linked := knowledge.LinkedContext{Items: []knowledge.ContextItem{{
		Type:       knowledge.NodeTypeMeeting,
		Title:      "Design review",
		Confidence: 0.9,
		Metadata:   map[string]any{"start": "2026-06-23T09:00:00+07:00"},
	}}}

	messages := r.withRuntimeSystemPromptOptions(
		[]providers.Message{{Role: providers.MessageRoleUser, Content: "project nay lien quan gi"}},
		sessions.SessionMemory{},
		nil,
		runtimePromptOptions{IncludeLongTermMemory: true, LinkedKnowledge: &linked},
	)

	joined := providerMessagesContent(messages)
	if !strings.Contains(joined, "Linked knowledge context") || !strings.Contains(joined, "context_only") {
		t.Fatalf("linked knowledge guard missing from prompt: %s", joined)
	}
	if !strings.Contains(joined, "Design review") {
		t.Fatalf("linked knowledge item missing from prompt: %s", joined)
	}
}

func TestRuntimePromptRoutesDriveMoveToMoveFile(t *testing.T) {
	// Move rules live in the drive.moveFile tool description, not the system prompt.
	found := false
	for _, entry := range drivetool.RegistryEntries {
		if entry.Name != drivetool.ToolNameMoveFile {
			continue
		}
		found = true
		for _, want := range []string{
			"drive.updateFileMetadata",
			"drive.listFiles",
		} {
			if !strings.Contains(entry.Description, want) {
				t.Fatalf("drive.moveFile description missing %q, got: %q", want, entry.Description)
			}
		}
	}
	if !found {
		t.Fatal("drive.moveFile not found in RegistryEntries")
	}
}

func TestRuntimePromptRoutesDriveFolderCreationToCreateFolder(t *testing.T) {
	// Folder creation rules live in the drive.createFolder tool description, not the system prompt.
	found := false
	for _, entry := range drivetool.RegistryEntries {
		if entry.Name != drivetool.ToolNameCreateFolder {
			continue
		}
		found = true
		for _, want := range []string{
			"parentIds",
			"drive.listFiles",
		} {
			if !strings.Contains(entry.Description, want) {
				t.Fatalf("drive.createFolder description missing %q, got: %q", want, entry.Description)
			}
		}
	}
	if !found {
		t.Fatal("drive.createFolder not found in RegistryEntries")
	}
}

func TestRuntimePromptRequiresReadableEmailParagraphBreaks(t *testing.T) {
	prompt := runtimeSystemPrompt(time.Date(2026, time.June, 10, 9, 30, 0, 0, time.FixedZone("ICT", 7*60*60)))
	for _, want := range []string{
		"paragraph breaks",
		"greeting, main content, and closing/signature",
		"Do not collapse the whole email into one paragraph",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("runtime prompt missing email formatting guidance %q", want)
		}
	}
}

func TestRuntimePromptBoundsSandboxPDFExtractionOutput(t *testing.T) {
	prompt := runtimeSystemPrompt(time.Date(2026, time.June, 10, 9, 30, 0, 0, time.FixedZone("ICT", 7*60*60)))
	for _, want := range []string{
		"NEVER print the entire extracted document text",
		"under 4000 characters",
		"For PDF summarization specifically",
		"Do not do text += page_text for every page followed by print(text)",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("runtime prompt missing sandbox output guidance %q", want)
		}
	}
}
