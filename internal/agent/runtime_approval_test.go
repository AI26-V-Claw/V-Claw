package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
	calendartool "vclaw/internal/tools/office/calendar"
	chattool "vclaw/internal/tools/office/chat"
	docstool "vclaw/internal/tools/office/docs"
	drivetool "vclaw/internal/tools/office/drive"
	gmailtool "vclaw/internal/tools/office/gmail"
	sheetstool "vclaw/internal/tools/office/sheets"
	fstool "vclaw/internal/tools/os/filesystem"
	sandboxtool "vclaw/internal/tools/system/sandbox"
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

func TestApprovalSummariesCoverApprovalRequiredProductionTools(t *testing.T) {
	fallback := approvalSummary("unknown.tool", contracts.RiskLevelExternalWrite)
	legacyFallback := legacyApprovalSummary("unknown.tool", contracts.RiskLevelExternalWrite)

	for _, toolName := range approvalRequiredProductionToolNames() {
		if got := approvalSummary(toolName, contracts.RiskLevelExternalWrite); got == fallback {
			t.Errorf("approvalSummary(%q) returned fallback summary", toolName)
		}
		if got := legacyApprovalSummary(toolName, contracts.RiskLevelExternalWrite); got == legacyFallback {
			t.Errorf("legacyApprovalSummary(%q) returned fallback summary", toolName)
		}
	}
}

func approvalRequiredProductionToolNames() []string {
	names := []string{
		fstool.ToolNameWriteFile,
		sandboxtool.ToolNameRunPython,
		sandboxtool.ToolNameRunShell,
	}
	for _, entry := range gmailtool.RegistryEntries {
		if entry.RequiresApproval {
			names = append(names, entry.Name)
		}
	}
	for _, entry := range drivetool.RegistryEntries {
		if entry.RequiresApproval {
			names = append(names, entry.Name)
		}
	}
	for _, entry := range docstool.RegistryEntries {
		if entry.RequiresApproval {
			names = append(names, entry.Name)
		}
	}
	for _, entry := range sheetstool.RegistryEntries {
		if entry.RequiresApproval {
			names = append(names, entry.Name)
		}
	}
	for _, entry := range calendartool.RegistryEntries {
		if entry.RequiresApproval {
			names = append(names, entry.Name)
		}
	}
	for _, entry := range chattool.RegistryEntries {
		if entry.RequiresApproval {
			names = append(names, entry.Name)
		}
	}
	return names
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

func TestApprovalRejectDoesNotExecuteWrite(t *testing.T) {
	ctx := context.Background()
	executions := 0
	runtime := newApprovalLifecycleTestRuntime(t, &executions, nil, fixedTestTime)
	approval := requireCalendarApproval(t, runtime, ctx)

	response, err := runtime.ResolveApproval(ctx, approval.SessionID, contracts.ApprovalDecision{
		ApprovalID: approval.ApprovalID,
		RequestID:  "req_reject",
		Decision:   contracts.ApprovalDecisionRejected,
		Comment:    "cancel this write",
		DecidedAt:  fixedTestTime(),
	})
	if err != nil {
		t.Fatalf("resolve reject: %v", err)
	}
	if response.Status != contracts.AgentStatusNeedClarification {
		t.Fatalf("reject with comment status = %s, want need_clarification", response.Status)
	}
	if executions != 0 {
		t.Fatalf("rejected approval executed write %d times", executions)
	}

	duplicate, err := runtime.ResolveApproval(ctx, approval.SessionID, contracts.ApprovalDecision{
		ApprovalID: approval.ApprovalID,
		RequestID:  "req_reject_duplicate",
		Decision:   contracts.ApprovalDecisionApproved,
		DecidedAt:  fixedTestTime(),
	})
	if err != nil {
		t.Fatalf("resolve duplicate after reject: %v", err)
	}
	if duplicate.Error == nil || duplicate.Error.Code != contracts.ErrorApprovalNotFound {
		t.Fatalf("duplicate approval after reject error = %#v, want APPROVAL_NOT_FOUND", duplicate.Error)
	}
	if executions != 0 {
		t.Fatalf("duplicate approval after reject executed write %d times", executions)
	}
}

func TestApprovalApproveDuplicateExecutesOnce(t *testing.T) {
	ctx := context.Background()
	executions := 0
	provider := &fakeProvider{responses: []providers.ChatResponse{{Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "completed"}}}}
	runtime := newApprovalLifecycleTestRuntime(t, &executions, provider, fixedTestTime)
	approval := requireCalendarApproval(t, runtime, ctx)

	approved, err := runtime.ResolveApproval(ctx, approval.SessionID, contracts.ApprovalDecision{
		ApprovalID: approval.ApprovalID,
		RequestID:  "req_approve",
		Decision:   contracts.ApprovalDecisionApproved,
		DecidedAt:  fixedTestTime(),
	})
	if err != nil {
		t.Fatalf("resolve approve: %v", err)
	}
	if approved.Status != contracts.AgentStatusCompleted {
		t.Fatalf("approve status = %s, want completed", approved.Status)
	}
	if executions != 1 {
		t.Fatalf("approved write executions = %d, want 1", executions)
	}

	duplicate, err := runtime.ResolveApproval(ctx, approval.SessionID, contracts.ApprovalDecision{
		ApprovalID: approval.ApprovalID,
		RequestID:  "req_duplicate",
		Decision:   contracts.ApprovalDecisionApproved,
		DecidedAt:  fixedTestTime(),
	})
	if err != nil {
		t.Fatalf("resolve duplicate approve: %v", err)
	}
	if duplicate.Error == nil || duplicate.Error.Code != contracts.ErrorApprovalNotFound {
		t.Fatalf("duplicate approval error = %#v, want APPROVAL_NOT_FOUND", duplicate.Error)
	}
	if executions != 1 {
		t.Fatalf("duplicate approval executions = %d, want still 1", executions)
	}
}

func TestApprovalExpiredDoesNotExecuteWrite(t *testing.T) {
	ctx := context.Background()
	executions := 0
	now := fixedTestTime()
	runtime := newApprovalLifecycleTestRuntime(t, &executions, nil, func() time.Time { return now })
	approval := requireCalendarApproval(t, runtime, ctx)

	now = approval.ExpiresAt.Add(time.Second)
	response, err := runtime.ResolveApproval(ctx, approval.SessionID, contracts.ApprovalDecision{
		ApprovalID: approval.ApprovalID,
		RequestID:  "req_expired",
		Decision:   contracts.ApprovalDecisionApproved,
		DecidedAt:  now,
	})
	if err != nil {
		t.Fatalf("resolve expired approval: %v", err)
	}
	if response.Error == nil || response.Error.Code != contracts.ErrorApprovalExpired {
		t.Fatalf("expired approval error = %#v, want APPROVAL_EXPIRED", response.Error)
	}
	if executions != 0 {
		t.Fatalf("expired approval executed write %d times", executions)
	}
}

func TestApprovalReviseCreatesReplacementApprovalWithoutExecutingOriginal(t *testing.T) {
	ctx := context.Background()
	executions := 0
	provider := &fakeProvider{responses: []providers.ChatResponse{{Message: providers.Message{
		Role: providers.MessageRoleAssistant,
		ToolCalls: []providers.ToolCall{{
			ID:   "call_chat_revised",
			Name: "chat.sendMessage",
			Arguments: map[string]any{
				"space": "spaces/A",
				"text":  "revised text",
			},
		}},
	}}}}
	runtime := newApprovalLifecycleTestRuntime(t, &executions, provider, fixedTestTime)
	approval := requireCalendarApproval(t, runtime, ctx)

	response, err := runtime.ResolveApproval(ctx, approval.SessionID, contracts.ApprovalDecision{
		ApprovalID: approval.ApprovalID,
		RequestID:  "req_revise",
		Decision:   contracts.ApprovalDecisionRevised,
		Comment:    "change title to revised title",
		DecidedAt:  fixedTestTime(),
	})
	if err != nil {
		t.Fatalf("resolve revise: %v", err)
	}
	if response.Status != contracts.AgentStatusApprovalRequired {
		t.Fatalf("revise status = %s, want approval_required", response.Status)
	}
	if response.ApprovalRequest == nil {
		t.Fatal("expected replacement approval request")
	}
	if response.ApprovalRequest.ParentApprovalID != approval.ApprovalID {
		t.Fatalf("replacement parent approval = %q, want %q", response.ApprovalRequest.ParentApprovalID, approval.ApprovalID)
	}
	if got := response.ApprovalRequest.ToolCall.Input["text"]; got != "revised text" {
		t.Fatalf("replacement text = %#v, want revised text", got)
	}
	if executions != 0 {
		t.Fatalf("revise executed original write %d times", executions)
	}
}

func newApprovalLifecycleTestRuntime(t *testing.T, executions *int, provider *fakeProvider, now func() time.Time) *Runtime {
	t.Helper()
	initialApprovalResponse := providers.ChatResponse{Message: providers.Message{
		Role: providers.MessageRoleAssistant,
		ToolCalls: []providers.ToolCall{{
			ID:   "call_chat_initial",
			Name: "chat.sendMessage",
			Arguments: map[string]any{
				"space": "spaces/A",
				"text":  "initial text",
			},
		}},
	}}
	if provider == nil {
		provider = &fakeProvider{}
	}
	provider.responses = append([]providers.ChatResponse{initialApprovalResponse}, provider.responses...)
	registry := tools.NewToolRegistry()
	if err := registry.Register(chatSendRuntimeTool{executions: executions}); err != nil {
		t.Fatalf("register calendar tool: %v", err)
	}
	return NewRuntime(RuntimeConfig{
		Provider: provider,
		Registry: registry,
		Now:      now,
	})
}

func requireCalendarApproval(t *testing.T, runtime *Runtime, ctx context.Context) *contracts.ApprovalRequest {
	t.Helper()
	response, err := runtime.Run(ctx, contracts.UserMessage{
		RequestID: "req_initial",
		SessionID: "sess_approval_lifecycle",
		Channel:   "dev",
		Text:      "create calendar event requiring approval",
		Timestamp: runtime.now(),
	})
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusApprovalRequired {
		t.Fatalf("initial status = %s, want approval_required: %#v", response.Status, response)
	}
	if response.ApprovalRequest == nil {
		t.Fatal("expected approval request")
	}
	return response.ApprovalRequest
}
