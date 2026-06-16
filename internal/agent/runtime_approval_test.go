package agent

import (
	"context"
	"strings"
	"testing"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
)

// TestContinuationMessageFullTextReachesProvider verifies that when r.Run() is called
// as an approval continuation the full message.Text (with "do not repeat" instructions)
// is sent to the provider, not the stripped "[Tiếp tục...]" placeholder stored in the
// transcript.
func TestContinuationMessageFullTextReachesProvider(t *testing.T) {
	ctx := context.Background()
	store := sessions.NewInMemoryStore()
	sessionID := "sess_cont_fix"

	// Seed: user request → assistant tool_use → ACTION_REQUIRES_APPROVAL placeholder.
	for _, msg := range []providers.Message{
		{Role: providers.MessageRoleUser, Content: "chạy python"},
		{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{
				ID: "call_1", Name: "sandbox.runPython",
				Arguments: map[string]any{"code": "print(42)"},
			}},
		},
		{Role: providers.MessageRoleTool, ToolCallID: "call_1", Content: "ACTION_REQUIRES_APPROVAL: Python sandbox requires approval"},
	} {
		_ = store.AppendMessage(ctx, sessionID, msg)
	}

	provider := &fakeProvider{
		responses: []providers.ChatResponse{
			{Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "Kết quả là 42."}},
		},
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:     provider,
		Registry:     tools.NewToolRegistry(),
		SessionStore: store,
		Now:          fixedTestTime,
	})

	const continuationInstructions = "Do not repeat the tool that was just executed."
	continuation := contracts.UserMessage{
		RequestID: "req_cont",
		SessionID: sessionID,
		Channel:   "dev",
		Text:      "An approved tool just completed. Completed tool: sandbox.runPython. Result: 42. " + continuationInstructions,
		Metadata: map[string]any{
			"continuationOf": "approval_abc",
			"completedTool":  "sandbox.runPython",
		},
		Timestamp: fixedTestTime(),
	}

	_, _ = runtime.Run(ctx, continuation)

	if len(provider.calls) == 0 {
		t.Fatal("expected at least one provider call")
	}
	lastCall := provider.calls[len(provider.calls)-1]
	lastUserContent := ""
	for _, msg := range lastCall.Messages {
		if msg.Role == providers.MessageRoleUser {
			lastUserContent = msg.Content
		}
	}
	if !strings.Contains(lastUserContent, continuationInstructions) {
		t.Fatalf("continuation instructions not found in provider messages\nlast user content: %q", lastUserContent)
	}
	if strings.Contains(lastUserContent, "Tiếp tục sau khi") {
		t.Fatalf("provider received stripped placeholder instead of full continuation text\nlast user content: %q", lastUserContent)
	}
}

func TestEnrichDriveMoveApprovalInputUsesRecentListFilesResults(t *testing.T) {
	input := map[string]any{
		"fileId":         "file_1",
		"targetParentId": "folder_1",
	}
	transcript := []providers.Message{{
		Role:       providers.MessageRoleTool,
		ToolCallID: "call_list_sources",
		Content:    `{"Files":[{"id":"file_1","name":"Thuật toán segment tree","mimeType":"application/vnd.google-apps.document"},{"id":"folder_1","name":"Nhập môn lập trình","mimeType":"application/vnd.google-apps.folder"}]}`,
	}}

	got := enrichApprovalInput("drive.moveFile", input, transcript)
	sources, ok := got["sourceFiles"].([]string)
	if !ok || len(sources) != 1 {
		t.Fatalf("sourceFiles = %#v, want one source", got["sourceFiles"])
	}
	if !strings.Contains(sources[0], "Thuật toán segment tree") || !strings.Contains(sources[0], "file_1") {
		t.Fatalf("unexpected source display: %q", sources[0])
	}
	target, _ := got["targetFolder"].(string)
	if !strings.Contains(target, "Nhập môn lập trình") || !strings.Contains(target, "folder_1") {
		t.Fatalf("unexpected target display: %q", target)
	}
}

func TestEnrichDriveMoveFilesApprovalInputShowsEverySource(t *testing.T) {
	input := map[string]any{
		"fileIds":        []any{"file_1", "file_2"},
		"targetParentId": "folder_1",
	}
	transcript := []providers.Message{{
		Role:    providers.MessageRoleTool,
		Content: `{"Files":[{"ID":"file_1","Name":"A"},{"ID":"file_2","Name":"B"},{"ID":"folder_1","Name":"Đích"}]}`,
	}}

	got := enrichApprovalInput("drive.moveFiles", input, transcript)
	sources, ok := got["sourceFiles"].([]string)
	if !ok || len(sources) != 2 {
		t.Fatalf("sourceFiles = %#v, want two sources", got["sourceFiles"])
	}
	if !strings.Contains(strings.Join(sources, "\n"), "A") || !strings.Contains(strings.Join(sources, "\n"), "B") {
		t.Fatalf("unexpected sources: %#v", sources)
	}
}
