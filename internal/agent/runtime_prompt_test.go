package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"vclaw/internal/agent/reference"
	"vclaw/internal/contracts"
	"vclaw/internal/governance"
	"vclaw/internal/knowledge"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
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

func TestPromptVersionStableAcrossConstructionTimes(t *testing.T) {
	early := time.Date(2026, time.January, 1, 8, 0, 0, 0, time.UTC)
	late := time.Date(2026, time.December, 31, 23, 59, 0, 0, time.UTC)
	r1 := NewRuntime(RuntimeConfig{Provider: &fakeProvider{}, Now: func() time.Time { return early }})
	r2 := NewRuntime(RuntimeConfig{Provider: &fakeProvider{}, Now: func() time.Time { return late }})
	if r1.promptVersion == "" {
		t.Fatalf("promptVersion should not be empty")
	}
	if r1.promptVersion != r2.promptVersion {
		t.Fatalf("promptVersion must be stable across construction times: %q vs %q", r1.promptVersion, r2.promptVersion)
	}
}

func TestPromptVersionChangesWhenStaticPromptChanges(t *testing.T) {
	base := runtimeSystemPromptStatic()
	mutated := base + "\nextra static rule"
	if governance.PromptVersion(mutated) == governance.PromptVersion(base) {
		t.Fatalf("promptVersion must change when static prompt content changes")
	}
}

func TestRuntimeSystemPromptStaticHasNoWallClock(t *testing.T) {
	// The static prompt used for versioning must not embed a real timestamp.
	static := runtimeSystemPromptStatic()
	if !strings.Contains(static, runtimeSystemPromptDatetimePlaceholder) {
		t.Fatalf("static prompt should carry the datetime placeholder, not a real time")
	}
}

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
	var memBlock string
	for _, m := range messages {
		if strings.Contains(m.Content, "Reference sources") {
			refBlock = m.Content
		}
		if strings.Contains(m.Content, "Known file references") {
			memBlock = m.Content
		}
	}
	if refBlock == "" {
		t.Fatal("reference sources block not found in assembled context")
	}
	if !strings.Contains(refBlock, "gmail.listEmails") {
		t.Errorf("reference sources block missing tool provenance, got: %q", refBlock)
	}
	// The provenance block lists the file by name and source but must NOT inject
	// the absolute host path — that stays in the session-memory block where it is
	// actually needed to attach the file.
	if !strings.Contains(refBlock, "report.pdf") {
		t.Errorf("reference sources block missing file name, got: %q", refBlock)
	}
	if strings.Contains(refBlock, "/workspace/report.pdf") {
		t.Errorf("reference sources block should not leak the absolute path, got: %q", refBlock)
	}
	if strings.Contains(memBlock, "/workspace/report.pdf") || strings.Contains(memBlock, "path=") || strings.Contains(memBlock, "driveId=") {
		t.Errorf("session memory block should not leak raw path/ID metadata, got: %q", memBlock)
	}
	if !strings.Contains(memBlock, "report.pdf") {
		t.Errorf("session memory block should retain the safe filename label, got: %q", memBlock)
	}
}

func TestReferenceSourcesDistinguishesRepeatedToolCalls(t *testing.T) {
	// Same tool called twice, each touching a different resource.
	memory := sessions.SessionMemory{
		LastActionResults: []sessions.ActionResult{
			{ToolName: "drive.downloadFile", Content: "downloaded A", ToolCallID: "call_1", Artifact: &sessions.ActionArtifact{Kind: "file", Label: "alpha.pdf", ID: "drive-id-aaa"}},
			{ToolName: "drive.downloadFile", Content: "downloaded B", ToolCallID: "call_2", Artifact: &sessions.ActionArtifact{Kind: "file", Label: "beta.pdf", ID: "drive-id-bbb"}},
		},
	}
	block := referenceSourcesPrompt(memory)
	if !strings.Contains(block, "alpha.pdf") || !strings.Contains(block, "beta.pdf") {
		t.Fatalf("provenance should distinguish both resources, got: %q", block)
	}
	if !strings.Contains(block, "[R1]") || !strings.Contains(block, "[R2]") {
		t.Fatalf("provenance should include stable prompt-local reference labels, got: %q", block)
	}
	// Two distinct result lines for the same tool.
	if got := strings.Count(block, "tool=drive.downloadFile"); got != 2 {
		t.Fatalf("expected 2 distinct provenance lines for repeated tool, got %d: %q", got, block)
	}
	// Opaque resource IDs must not be exposed in the provenance block.
	if strings.Contains(block, "drive-id-aaa") || strings.Contains(block, "drive-id-bbb") {
		t.Fatalf("opaque resource IDs must not appear in provenance block: %q", block)
	}
}

func TestRecordActionResultCapturesProvenance(t *testing.T) {
	now := time.Date(2026, time.June, 10, 9, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	store := sessions.NewInMemoryStore()
	r := NewRuntime(RuntimeConfig{
		Provider:     &fakeProvider{},
		SessionStore: store,
		Now:          func() time.Time { return now },
	})
	result := tools.ToolResult{
		ToolCallID:    "call_xyz",
		ToolName:      "gmail.listEmails",
		Success:       true,
		ContentForLLM: "found 3 emails",
		Source:        "tool:google_workspace",
		ArtifactRef:   &tools.ToolArtifactRef{Kind: "email", Label: "Inbox", ID: "msg-123"},
	}
	if errShape := r.recordActionResultForRun(context.Background(), "sess-1", "run-1", "req-1", result); errShape != nil {
		t.Fatalf("recordActionResultForRun failed: %v", errShape)
	}
	mem, _ := store.LoadMemory(context.Background(), "sess-1")
	if len(mem.LastActionResults) != 1 {
		t.Fatalf("expected 1 recorded action result, got %d", len(mem.LastActionResults))
	}
	ar := mem.LastActionResults[0]
	if ar.ToolCallID != "call_xyz" || ar.RunID != "run-1" || ar.RequestID != "req-1" || ar.Source != "tool:google_workspace" {
		t.Fatalf("provenance fields not captured: %+v", ar)
	}
	if ar.Artifact == nil || ar.Artifact.ID != "msg-123" || ar.Artifact.Label != "Inbox" {
		t.Fatalf("artifact provenance not captured: %+v", ar.Artifact)
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

func TestRedactSensitiveForPromptRemovesWholePEMBlock(t *testing.T) {
	content := strings.Join([]string{
		"Here is the deploy key for reference:",
		"-----BEGIN RSA PRIVATE KEY-----",
		"MIIEowIBAAKCAQEAabc123base64bodyLine1",
		"morebase64bodyLine2morebase64bodyLine3",
		"-----END RSA PRIVATE KEY-----",
		"Contact: mai@example.com",
	}, "\n")
	out := redactSensitiveForPrompt(content)
	for _, leak := range []string{"MIIEowIBAAKCAQEAabc123base64bodyLine1", "morebase64bodyLine2", "BEGIN RSA PRIVATE KEY"} {
		if strings.Contains(out, leak) {
			t.Errorf("PEM block leaked %q in output: %q", leak, out)
		}
	}
	if !strings.Contains(out, "Contact: mai@example.com") {
		t.Errorf("non-secret content after PEM block should survive: %q", out)
	}
}

// TestAssembledContextRedactsSecretsAcrossAllSections is an end-to-end check over
// the full ChatRequest.Messages: secrets injected into the transcript, long-term
// memory, and linked knowledge must not survive into any assembled message.
func TestAssembledContextRedactsSecretsAcrossAllSections(t *testing.T) {
	now := time.Date(2026, time.June, 10, 9, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	r := NewRuntime(RuntimeConfig{Provider: &fakeProvider{}, Now: func() time.Time { return now }})
	r.ltMemLoader = &fakeLTMemLoader{content: "Profile notes\nGITHUB_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz0123\nworks on Helios"}

	pem := strings.Join([]string{
		"-----BEGIN PRIVATE KEY-----",
		"MIIBVgIBADANBgkqhkiG9w0BAQEFAASCATSECRETKEYBODY",
		"-----END PRIVATE KEY-----",
	}, "\n")
	transcript := []providers.Message{
		{Role: providers.MessageRoleUser, Content: "here is my key\n" + pem},
		{Role: providers.MessageRoleAssistant, Content: "noted"},
		{Role: providers.MessageRoleUser, Content: "my api key is sk-zyxwvu9876543210abcdef please remember"},
	}
	linked := knowledge.LinkedContext{Items: []knowledge.ContextItem{
		{Type: "note", Title: "creds: AWS_SECRET_ACCESS_KEY=abcd1234secretvalue0000", Confidence: 0.9},
	}}
	memory := sessions.SessionMemory{
		Summary: "earlier the user pasted CLIENT_SECRET=supersecretvalue12345",
	}

	messages := r.withRuntimeSystemPromptOptions(transcript, memory, nil, runtimePromptOptions{
		IncludeLongTermMemory: true,
		LinkedKnowledge:       &linked,
	})

	secrets := []string{
		"ghp_abcdefghijklmnopqrstuvwxyz0123",
		"SECRETKEYBODY",
		"BEGIN PRIVATE KEY",
		"sk-zyxwvu9876543210abcdef",
		"abcd1234secretvalue0000",
		"supersecretvalue12345",
	}
	for _, m := range messages {
		for _, s := range secrets {
			if strings.Contains(m.Content, s) {
				t.Errorf("assembled context leaked secret %q in role %s: %q", s, m.Role, m.Content)
			}
		}
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

func TestAssembleProviderChatRequestCountsToolSchemasInBudget(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := registry.Register(parallelRuntimeTool{
		name:       "test.big_schema",
		parameters: tools.ToolSchema{"type": "object", "properties": map[string]any{"blob": strings.Repeat("x", 12000)}},
	}); err != nil {
		t.Fatalf("register big schema tool: %v", err)
	}
	r := NewRuntime(RuntimeConfig{
		Provider:      &fakeProvider{},
		Registry:      registry,
		ContextWindow: 32_000,
	})
	transcript := []providers.Message{
		{Role: providers.MessageRoleUser, Content: repeatTokens("history", 12_000)},
		{Role: providers.MessageRoleAssistant, Content: repeatTokens("reply", 6_000)},
		{Role: providers.MessageRoleUser, Content: repeatTokens("current request", 10_000)},
	}
	memory := sessions.SessionMemory{
		Summary: repeatTokens("session summary", 2_500),
	}
	request := r.assembleProviderChatRequest(transcript, memory, nil, runtimePromptOptions{
		IncludeLongTermMemory: true,
		PreSystemMessages: []providers.Message{
			{Role: providers.MessageRoleSystem, Content: repeatTokens("active plan", 500)},
		},
	})
	budget := r.contextBudget.normalized()
	total := estimateProviderRequestTokens(request.Messages, request.Tools)
	if total > budget.Available() {
		t.Fatalf("assembled provider request exceeds budget: total=%d available=%d", total, budget.Available())
	}
	if estimateToolDefinitionsTokens(request.Tools) == 0 {
		t.Fatal("expected tool schema tokens to be counted")
	}
}

func TestRuntimeFailsStableWhenProviderRequestStillExceedsBudget(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := registry.Register(parallelRuntimeTool{
		name:       "test.big_schema",
		parameters: tools.ToolSchema{"type": "object", "properties": map[string]any{"blob": strings.Repeat("x", 12000)}},
	}); err != nil {
		t.Fatalf("register big schema tool: %v", err)
	}
	provider := &fakeProvider{}
	r := NewRuntime(RuntimeConfig{
		Provider:      provider,
		Registry:      registry,
		ContextWindow: 1024,
	})
	response, err := r.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusFailed {
		t.Fatalf("expected failed response, got %#v", response)
	}
	if response.Error == nil || response.Error.Code != contracts.ErrorInternal {
		t.Fatalf("expected stable internal error for budget overflow, got %#v", response.Error)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("provider should not be called when request exceeds budget, got %d calls", len(provider.calls))
	}
	if !strings.Contains(response.Message, "exceeds context budget") {
		t.Fatalf("expected context budget failure message, got %q", response.Message)
	}
}
func TestRuntimePromptBoundsSandboxPDFExtractionOutput(t *testing.T) {
	prompt := runtimeSystemPrompt(time.Date(2026, time.June, 10, 9, 30, 0, 0, time.FixedZone("ICT", 7*60*60)))
	for _, want := range []string{
		"NEVER print the entire extracted document text",
		"under 4000 characters",
		"For PDF summarization specifically",
		"Do not do text += page_text for every page followed by print(text)",
		"use that exact path in Python and preserve every subdirectory",
		"/workspace/data/telegram_attachments/",
		"Do not reduce a nested file to its basename",
		"docs.createDocument creates an empty document only",
		"use sandbox.extractPDF to produce structured Markdown",
		"docs.createDocument and docs.appendMarkdown",
		"Do not call filesystem.readFile first",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("runtime prompt missing sandbox output guidance %q", want)
		}
	}
}
