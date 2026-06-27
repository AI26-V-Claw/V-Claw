package agent

import (
	"context"
	"testing"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

// newTestRegistry creates a ToolRegistry populated with common workspace
// tools for testing.  The tools are stubs that implement the Tool interface
// with the correct Name, Capability, and RiskLevel.
func newTestRegistry() *tools.ToolRegistry {
	registry := tools.NewToolRegistry()
	stubs := []struct {
		name       string
		capability tools.Capability
		riskLevel  tools.RiskLevel
	}{
		// Gmail
		{"gmail.listEmails", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		{"gmail.getEmail", tools.CapabilityReadOnly, tools.RiskLevelSensitiveRead},
		{"gmail.createDraft", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"gmail.sendDraft", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"gmail.downloadAttachments", tools.CapabilityMutating, tools.RiskLevelLocalWrite},
		// Calendar
		{"calendar.listEvents", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		{"calendar.getEvent", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		{"calendar.createEvent", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"calendar.updateEvent", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"calendar.deleteEvent", tools.CapabilityMutating, tools.RiskLevelDestructive},
		// Drive
		{"drive.listFiles", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		{"drive.getFile", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		{"drive.createFile", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"drive.shareFile", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"drive.trashFile", tools.CapabilityMutating, tools.RiskLevelDestructive},
		// Docs
		{"docs.getDocument", tools.CapabilityReadOnly, tools.RiskLevelSensitiveRead},
		{"docs.createDocument", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"docs.appendText", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"docs.deleteContent", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		// Sheets
		{"sheets.getSpreadsheet", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		{"sheets.readValues", tools.CapabilityReadOnly, tools.RiskLevelSensitiveRead},
		{"sheets.createSpreadsheet", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"sheets.updateValues", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"sheets.clearValues", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"sheets.deleteSheet", tools.CapabilityMutating, tools.RiskLevelDestructive},
		// Chat
		{"chat.listSpaces", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		{"chat.listMessages", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		{"chat.sendMessage", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
		{"chat.deleteMessage", tools.CapabilityMutating, tools.RiskLevelDestructive},
		// People
		{"people.searchDirectory", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		// Web
		{"web.search", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		{"web.fetch", tools.CapabilityReadOnly, tools.RiskLevelSafeRead},
		// Meet
		{"meet.createMeeting", tools.CapabilityMutating, tools.RiskLevelExternalWrite},
	}
	for _, s := range stubs {
		_ = registry.Register(stubTool{name: s.name, cap: s.capability, risk: s.riskLevel})
	}
	return registry
}

// stubTool implements tools.Tool for test registration.
type stubTool struct {
	name string
	cap  tools.Capability
	risk tools.RiskLevel
}

func (s stubTool) Name() string                                                   { return s.name }
func (s stubTool) Description() string                                            { return s.name }
func (s stubTool) Parameters() tools.ToolSchema                                   { return tools.ToolSchema{} }
func (s stubTool) Capability() tools.Capability                                   { return s.cap }
func (s stubTool) RiskLevel() tools.RiskLevel                                     { return s.risk }
func (s stubTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true}
}

// ---------- IsWorkspaceReadTool ----------

func TestIsWorkspaceReadTool(t *testing.T) {
	registry := newTestRegistry()
	tests := []struct {
		name string
		tool string
		want bool
	}{
		{"gmail read", "gmail.listEmails", true},
		{"gmail sensitive read", "gmail.getEmail", true},
		{"calendar read", "calendar.listEvents", true},
		{"drive read", "drive.listFiles", true},
		{"docs read", "docs.getDocument", true},
		{"sheets read", "sheets.readValues", true},
		{"chat read", "chat.listMessages", true},
		{"people read", "people.searchDirectory", true},
		{"web search", "web.search", true},
		{"web fetch", "web.fetch", true},

		// Write tools should return false.
		{"gmail write is not read", "gmail.createDraft", false},
		{"calendar write is not read", "calendar.createEvent", false},
		{"drive write is not read", "drive.createFile", false},
		{"docs write is not read", "docs.createDocument", false},
		{"sheets write is not read", "sheets.updateValues", false},

		// Non-workspace tools should return false.
		{"sandbox tool", "sandbox.runPython", false},
		{"filesystem tool", "filesystem.writeFile", false},
		{"memory tool", "memory.save", false},
		{"unknown tool", "unknown.tool", false},
		{"empty string", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsWorkspaceReadTool(tt.tool, registry); got != tt.want {
				t.Errorf("IsWorkspaceReadTool(%q) = %v, want %v", tt.tool, got, tt.want)
			}
		})
	}
}

func TestIsWorkspaceReadTool_NilRegistry(t *testing.T) {
	if IsWorkspaceReadTool("gmail.listEmails", nil) {
		t.Error("expected false when registry is nil")
	}
}

// ---------- IsWorkspaceWriteTool ----------

func TestIsWorkspaceWriteTool(t *testing.T) {
	registry := newTestRegistry()
	tests := []struct {
		name string
		tool string
		want bool
	}{
		{"gmail write", "gmail.createDraft", true},
		{"gmail send", "gmail.sendDraft", true},
		{"calendar create", "calendar.createEvent", true},
		{"calendar update", "calendar.updateEvent", true},
		{"calendar delete", "calendar.deleteEvent", true},
		{"drive create", "drive.createFile", true},
		{"drive share", "drive.shareFile", true},
		{"drive trash", "drive.trashFile", true},
		{"docs create", "docs.createDocument", true},
		{"docs append", "docs.appendText", true},
		{"docs delete", "docs.deleteContent", true},
		{"sheets create", "sheets.createSpreadsheet", true},
		{"sheets update", "sheets.updateValues", true},
		{"sheets clear", "sheets.clearValues", true},
		{"sheets deleteSheet", "sheets.deleteSheet", true},
		{"chat send", "chat.sendMessage", true},
		{"chat delete", "chat.deleteMessage", true},
		{"meet create", "meet.createMeeting", true},

		// Read tools should return false.
		{"gmail read is not write", "gmail.listEmails", false},
		{"drive read is not write", "drive.getFile", false},
		{"calendar read is not write", "calendar.listEvents", false},

		// Non-workspace tools should return false.
		{"sandbox tool", "sandbox.runPython", false},
		{"filesystem tool", "filesystem.writeFile", false},
		{"unknown tool", "unknown.tool", false},
		{"empty string", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsWorkspaceWriteTool(tt.tool, registry); got != tt.want {
				t.Errorf("IsWorkspaceWriteTool(%q) = %v, want %v", tt.tool, got, tt.want)
			}
		})
	}
}

// ---------- ValidateReadBeforeWrite ----------

func TestValidateReadBeforeWrite_WriteBeforeRead(t *testing.T) {
	registry := newTestRegistry()
	proposed := []providers.ToolCall{
		{ID: "call_1", Name: "gmail.createDraft"},
	}
	var previousResults []contracts.ToolResult

	warning, violated := ValidateReadBeforeWrite(proposed, previousResults, registry)
	if !violated {
		t.Fatal("expected violated=true when writing without prior read")
	}
	if warning == "" {
		t.Fatal("expected non-empty warning message")
	}
}

func TestValidateReadBeforeWrite_ReadThenWrite(t *testing.T) {
	registry := newTestRegistry()
	proposed := []providers.ToolCall{
		{ID: "call_2", Name: "gmail.createDraft"},
	}
	previousResults := []contracts.ToolResult{
		{ToolCallID: "call_0", ToolName: "gmail.listEmails", Success: true},
	}

	warning, violated := ValidateReadBeforeWrite(proposed, previousResults, registry)
	if violated {
		t.Fatalf("expected violated=false after successful read, got warning=%q", warning)
	}
}

func TestValidateReadBeforeWrite_OnlyReads(t *testing.T) {
	registry := newTestRegistry()
	proposed := []providers.ToolCall{
		{ID: "call_1", Name: "gmail.listEmails"},
		{ID: "call_2", Name: "drive.listFiles"},
	}
	var previousResults []contracts.ToolResult

	warning, violated := ValidateReadBeforeWrite(proposed, previousResults, registry)
	if violated {
		t.Fatalf("expected violated=false when only read tools proposed, got warning=%q", warning)
	}
}

func TestValidateReadBeforeWrite_FailedReadThenWrite(t *testing.T) {
	registry := newTestRegistry()
	proposed := []providers.ToolCall{
		{ID: "call_2", Name: "calendar.createEvent"},
	}
	previousResults := []contracts.ToolResult{
		{
			ToolCallID: "call_1", ToolName: "calendar.listEvents", Success: false,
			Error: &contracts.ErrorShape{Code: "INTERNAL_ERROR", Message: "calendar API unavailable"},
		},
	}

	warning, violated := ValidateReadBeforeWrite(proposed, previousResults, registry)
	if !violated {
		t.Fatal("expected violated=true when only prior reads failed")
	}
	if warning == "" {
		t.Fatal("expected non-empty warning message")
	}
}

func TestValidateReadBeforeWrite_NonWorkspaceTools(t *testing.T) {
	registry := newTestRegistry()
	proposed := []providers.ToolCall{
		{ID: "call_1", Name: "sandbox.runPython"},
		{ID: "call_2", Name: "filesystem.writeFile"},
	}
	var previousResults []contracts.ToolResult

	warning, violated := ValidateReadBeforeWrite(proposed, previousResults, registry)
	if violated {
		t.Fatalf("expected violated=false for non-workspace tools, got warning=%q", warning)
	}
}

func TestValidateReadBeforeWrite_BatchReadThenWrite(t *testing.T) {
	registry := newTestRegistry()
	// Batch contains read before write — should be allowed (Fix #2).
	proposed := []providers.ToolCall{
		{ID: "call_1", Name: "web.search"},
		{ID: "call_2", Name: "docs.createDocument"},
	}
	var previousResults []contracts.ToolResult

	warning, violated := ValidateReadBeforeWrite(proposed, previousResults, registry)
	if violated {
		t.Fatalf("expected violated=false when batch has read before write, got warning=%q", warning)
	}
}

func TestValidateReadBeforeWrite_BatchWriteThenRead(t *testing.T) {
	registry := newTestRegistry()
	// Batch with write before read — should be blocked.
	proposed := []providers.ToolCall{
		{ID: "call_1", Name: "docs.createDocument"},
		{ID: "call_2", Name: "web.search"},
	}
	var previousResults []contracts.ToolResult

	_, violated := ValidateReadBeforeWrite(proposed, previousResults, registry)
	if !violated {
		t.Fatal("expected violated=true when batch has write before read")
	}
}

func TestValidateReadBeforeWrite_MixedBatchGmailCalendarChat(t *testing.T) {
	registry := newTestRegistry()
	// Realistic batch: read Gmail + create Calendar in same batch.
	proposed := []providers.ToolCall{
		{ID: "call_1", Name: "gmail.listEmails"},
		{ID: "call_2", Name: "calendar.createEvent"},
	}
	var previousResults []contracts.ToolResult

	warning, violated := ValidateReadBeforeWrite(proposed, previousResults, registry)
	if violated {
		t.Fatalf("expected batch with read+write to pass, got warning=%q", warning)
	}
}

// ---------- HasSuccessfulWorkspaceRead ----------

func TestHasSuccessfulWorkspaceRead(t *testing.T) {
	registry := newTestRegistry()
	tests := []struct {
		name    string
		results []contracts.ToolResult
		want    bool
	}{
		{"nil results", nil, false},
		{"empty results", []contracts.ToolResult{}, false},
		{
			"single successful read",
			[]contracts.ToolResult{
				{ToolCallID: "c1", ToolName: "gmail.listEmails", Success: true},
			},
			true,
		},
		{
			"single failed read",
			[]contracts.ToolResult{
				{ToolCallID: "c1", ToolName: "gmail.listEmails", Success: false,
					Error: &contracts.ErrorShape{Code: "INTERNAL_ERROR", Message: "fail"}},
			},
			false,
		},
		{
			"failed read then successful read",
			[]contracts.ToolResult{
				{ToolCallID: "c1", ToolName: "gmail.listEmails", Success: false,
					Error: &contracts.ErrorShape{Code: "INTERNAL_ERROR", Message: "fail"}},
				{ToolCallID: "c2", ToolName: "drive.listFiles", Success: true},
			},
			true,
		},
		{
			"successful write only",
			[]contracts.ToolResult{
				{ToolCallID: "c1", ToolName: "gmail.createDraft", Success: true},
			},
			false,
		},
		{
			"non-workspace tool succeeds",
			[]contracts.ToolResult{
				{ToolCallID: "c1", ToolName: "sandbox.runPython", Success: true},
			},
			false,
		},
		{
			"web.fetch is read",
			[]contracts.ToolResult{
				{ToolCallID: "c1", ToolName: "web.fetch", Success: true},
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasSuccessfulWorkspaceRead(tt.results, registry); got != tt.want {
				t.Errorf("HasSuccessfulWorkspaceRead() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------- isCreateFromScratch ----------

func TestIsCreateFromScratch(t *testing.T) {
	tests := []struct {
		name  string
		calls []providers.ToolCall
		want  bool
	}{
		{
			"empty calls",
			nil,
			false,
		},
		{
			"calendar.createEvent with all user input",
			[]providers.ToolCall{{
				ID: "c1", Name: "calendar.createEvent",
				Arguments: map[string]any{"title": "Meeting", "start": "2026-06-27T10:00:00+07:00", "end": "2026-06-27T11:00:00+07:00"},
			}},
			true,
		},
		{
			"docs.createDocument with title",
			[]providers.ToolCall{{
				ID: "c1", Name: "docs.createDocument",
				Arguments: map[string]any{"title": "Report"},
			}},
			true,
		},
		{
			"chat.sendMessage with space and text",
			[]providers.ToolCall{{
				ID: "c1", Name: "chat.sendMessage",
				Arguments: map[string]any{"space": "spaces/A", "text": "hello"},
			}},
			true,
		},
		{
			"calendar.updateEvent references eventId",
			[]providers.ToolCall{{
				ID: "c1", Name: "calendar.updateEvent",
				Arguments: map[string]any{"eventId": "abc123", "title": "Updated"},
			}},
			false,
		},
		{
			"docs.appendText references documentId",
			[]providers.ToolCall{{
				ID: "c1", Name: "docs.appendText",
				Arguments: map[string]any{"documentId": "doc123", "text": "content"},
			}},
			false,
		},
		{
			"drive.moveFile is modify verb",
			[]providers.ToolCall{{
				ID: "c1", Name: "drive.moveFile",
				Arguments: map[string]any{"source": "a", "destination": "b"},
			}},
			false,
		},
		{
			"gmail.modifyMessage is modify verb",
			[]providers.ToolCall{{
				ID: "c1", Name: "gmail.modifyMessage",
				Arguments: map[string]any{},
			}},
			false,
		},
		{
			"mixed: one create + one update → not from scratch",
			[]providers.ToolCall{
				{ID: "c1", Name: "docs.createDocument", Arguments: map[string]any{"title": "New"}},
				{ID: "c2", Name: "docs.appendText", Arguments: map[string]any{"documentId": "d1", "text": "x"}},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCreateFromScratch(tt.calls); got != tt.want {
				t.Errorf("isCreateFromScratch() = %v, want %v", got, tt.want)
			}
		})
	}
}
