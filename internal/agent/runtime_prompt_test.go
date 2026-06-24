package agent

import (
	"strings"
	"testing"
	"time"

	"vclaw/internal/agent/reference"
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

func TestRuntimeSystemPromptHasLabeledSafetySections(t *testing.T) {
	prompt := runtimeSystemPrompt(time.Date(2026, time.June, 10, 9, 30, 0, 0, time.FixedZone("ICT", 7*60*60)))
	for _, section := range []string{"<role>", "<limits>", "<tool-policy>", "<memory-rule>", "<hitl>"} {
		if !strings.Contains(prompt, section) {
			t.Errorf("runtime prompt missing required section %q", section)
		}
	}
	// HITL section must spell out that side-effect actions wait for human approval.
	if !strings.Contains(prompt, "human approval") {
		t.Error("runtime prompt HITL section missing human approval requirement")
	}
	// Memory rule must forbid filling dangerous parameters from memory alone.
	if !strings.Contains(prompt, "Do not use memory alone to fill required parameters") {
		t.Error("runtime prompt memory-rule section missing memory-isolation guidance")
	}
}

func TestReferenceSourcesBlockListsProvenance(t *testing.T) {
	now := time.Date(2026, time.June, 10, 9, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	r := NewRuntime(RuntimeConfig{
		Provider: &fakeProvider{},
		Now:      func() time.Time { return now },
	})
	memory := sessions.SessionMemory{
		Summary: "summary text",
		LastActionResults: []sessions.ActionResult{
			{ToolName: "gmail.listEmails", Content: "found 3 emails"},
		},
		FileRefs: map[string]sessions.FileRef{
			"report.pdf": {Source: "local", Path: "/workspace/report.pdf"},
		},
	}
	messages := r.withRuntimeSystemPrompt(
		[]providers.Message{{Role: providers.MessageRoleUser, Content: "hi"}},
		memory,
		nil,
	)
	var refBlock string
	for _, m := range messages {
		if strings.Contains(m.Content, "Reference sources") {
			refBlock = m.Content
		}
	}
	if refBlock == "" {
		t.Fatal("reference sources block not found in assembled context")
	}
	if !strings.Contains(refBlock, "gmail.listEmails") {
		t.Errorf("reference sources block missing tool provenance, got: %q", refBlock)
	}
	if !strings.Contains(refBlock, "report.pdf") || !strings.Contains(refBlock, "/workspace/report.pdf") {
		t.Errorf("reference sources block missing file provenance, got: %q", refBlock)
	}
}

func TestSessionMemoryPromptRedactsSecrets(t *testing.T) {
	memory := sessions.SessionMemory{
		Summary: "User shared config\nOPENAI_API_KEY=sk-abcdefghijklmnop1234\nmeeting at 10am",
	}
	prompt := sessionMemoryPrompt(memory)
	if strings.Contains(prompt, "sk-abcdefghijklmnop1234") {
		t.Errorf("session memory prompt leaked secret value: %q", prompt)
	}
	if !strings.Contains(prompt, "meeting at 10am") {
		t.Errorf("session memory prompt dropped non-secret content: %q", prompt)
	}
	if !strings.Contains(prompt, "[redacted") {
		t.Errorf("session memory prompt should mark redaction: %q", prompt)
	}
}

func TestContextContinuityAfterCompaction(t *testing.T) {
	now := time.Date(2026, time.June, 10, 9, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	r := NewRuntime(RuntimeConfig{
		Provider: &fakeProvider{},
		Now:      func() time.Time { return now },
	})
	// Simulate post-compaction state: summary holds the compacted history,
	// transcript holds only the recent kept messages.
	memory := sessions.SessionMemory{Summary: "compacted summary: user asked about Q2 report"}
	keptTranscript := []providers.Message{
		{Role: providers.MessageRoleUser, Content: "latest follow-up question"},
	}
	messages := r.withRuntimeSystemPrompt(keptTranscript, memory, nil)

	summaryIdx, userIdx := -1, -1
	for i, m := range messages {
		if strings.Contains(m.Content, "compacted summary") {
			summaryIdx = i
		}
		if m.Role == providers.MessageRoleUser && strings.Contains(m.Content, "latest follow-up question") {
			userIdx = i
		}
	}
	if summaryIdx < 0 {
		t.Fatal("compacted summary missing from assembled context")
	}
	if userIdx < 0 {
		t.Fatal("kept transcript message missing from assembled context")
	}
	if summaryIdx >= userIdx {
		t.Errorf("summary (index %d) must precede kept transcript (index %d) for continuity", summaryIdx, userIdx)
	}
}

func TestLongTermMemoryRedactsSecrets(t *testing.T) {
	now := time.Date(2026, time.June, 10, 9, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	r := NewRuntime(RuntimeConfig{
		Provider: &fakeProvider{},
		Now:      func() time.Time { return now },
	})
	r.ltMemLoader = &fakeLTMemLoader{content: "## Long-term memory\n- Name: Quang\n- GITHUB_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz1234"}

	messages := r.withRuntimeSystemPrompt(
		[]providers.Message{{Role: providers.MessageRoleUser, Content: "hi"}},
		sessions.SessionMemory{},
		nil,
	)
	for _, m := range messages {
		if strings.Contains(m.Content, "ghp_abcdefghijklmnopqrstuvwxyz1234") {
			t.Errorf("long-term memory leaked secret into context: %q", m.Content)
		}
	}
	found := false
	for _, m := range messages {
		if strings.Contains(m.Content, "Name: Quang") {
			found = true
		}
	}
	if !found {
		t.Error("long-term memory dropped non-secret content")
	}
}

func TestReferenceContextRedactsSecrets(t *testing.T) {
	now := time.Date(2026, time.June, 10, 9, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	r := NewRuntime(RuntimeConfig{
		Provider: &fakeProvider{},
		Now:      func() time.Time { return now },
	})
	resolution := &reference.Resolution{
		HasReference:    true,
		ReferenceType:   reference.TypeGmailEmail,
		ReferenceID:     "email-1",
		Source:          reference.SourceRecentHistory,
		Confidence:      0.9,
		ResolvedContext: map[string]any{"note": "api_key=sk-abcdefghijklmnop1234"},
	}
	messages := r.withRuntimeSystemPrompt(
		[]providers.Message{{Role: providers.MessageRoleUser, Content: "hi"}},
		sessions.SessionMemory{},
		resolution,
	)
	for _, m := range messages {
		if strings.Contains(m.Content, "sk-abcdefghijklmnop1234") {
			t.Errorf("reference context leaked secret into context: %q", m.Content)
		}
	}
}
