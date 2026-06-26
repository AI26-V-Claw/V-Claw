package agent

import (
	"testing"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
)

func TestIsWorkspaceReadTool(t *testing.T) {
	tests := []struct {
		name string
		tool string
		want bool
	}{
		{name: "gmail read", tool: "gmail.listEmails", want: true},
		{name: "calendar read", tool: "calendar.listEvents", want: true},
		{name: "drive read", tool: "drive.listFiles", want: true},
		{name: "docs read", tool: "docs.getDocument", want: true},
		{name: "sheets read", tool: "sheets.readValues", want: true},
		{name: "chat read", tool: "chat.listMessages", want: true},
		{name: "people read", tool: "people.searchDirectory", want: true},
		{name: "web read", tool: "web.search", want: true},

		// Write tools should return false.
		{name: "gmail write is not read", tool: "gmail.createDraft", want: false},
		{name: "calendar write is not read", tool: "calendar.createEvent", want: false},
		{name: "drive write is not read", tool: "drive.createFile", want: false},

		// Unknown tools should return false.
		{name: "unknown tool", tool: "unknown.tool", want: false},
		{name: "empty string", tool: "", want: false},
		{name: "sandbox tool", tool: "sandbox.runPython", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsWorkspaceReadTool(tt.tool); got != tt.want {
				t.Errorf("IsWorkspaceReadTool(%q) = %v, want %v", tt.tool, got, tt.want)
			}
		})
	}
}

func TestIsWorkspaceWriteTool(t *testing.T) {
	tests := []struct {
		name string
		tool string
		want bool
	}{
		{name: "gmail write", tool: "gmail.createDraft", want: true},
		{name: "gmail send", tool: "gmail.sendDraft", want: true},
		{name: "calendar create", tool: "calendar.createEvent", want: true},
		{name: "calendar update", tool: "calendar.updateEvent", want: true},
		{name: "calendar delete", tool: "calendar.deleteEvent", want: true},
		{name: "drive create", tool: "drive.createFile", want: true},
		{name: "drive move", tool: "drive.moveFile", want: true},
		{name: "docs create", tool: "docs.createDocument", want: true},
		{name: "sheets update", tool: "sheets.updateValues", want: true},
		{name: "chat send", tool: "chat.sendMessage", want: true},

		// Read tools should return false.
		{name: "gmail read is not write", tool: "gmail.listEmails", want: false},
		{name: "drive read is not write", tool: "drive.getFile", want: false},
		{name: "calendar read is not write", tool: "calendar.listEvents", want: false},

		// Unknown tools should return false.
		{name: "unknown tool", tool: "unknown.tool", want: false},
		{name: "empty string", tool: "", want: false},
		{name: "filesystem tool", tool: "filesystem.writeFile", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsWorkspaceWriteTool(tt.tool); got != tt.want {
				t.Errorf("IsWorkspaceWriteTool(%q) = %v, want %v", tt.tool, got, tt.want)
			}
		})
	}
}

func TestValidateReadBeforeWrite_WriteBeforeRead(t *testing.T) {
	proposed := []providers.ToolCall{
		{ID: "call_1", Name: "gmail.createDraft"},
	}
	var previousResults []contracts.ToolResult

	warning, violated := ValidateReadBeforeWrite(proposed, previousResults)
	if !violated {
		t.Fatal("expected violated=true when writing without prior read")
	}
	if warning == "" {
		t.Fatal("expected non-empty warning message")
	}
}

func TestValidateReadBeforeWrite_ReadThenWrite(t *testing.T) {
	proposed := []providers.ToolCall{
		{ID: "call_2", Name: "gmail.createDraft"},
	}
	previousResults := []contracts.ToolResult{
		{
			ToolCallID: "call_0",
			ToolName:   "gmail.listEmails",
			Success:    true,
		},
	}

	warning, violated := ValidateReadBeforeWrite(proposed, previousResults)
	if violated {
		t.Fatalf("expected violated=false after successful read, got warning=%q", warning)
	}
	if warning != "" {
		t.Fatalf("expected empty warning, got %q", warning)
	}
}

func TestValidateReadBeforeWrite_OnlyReads(t *testing.T) {
	proposed := []providers.ToolCall{
		{ID: "call_1", Name: "gmail.listEmails"},
		{ID: "call_2", Name: "drive.listFiles"},
	}
	var previousResults []contracts.ToolResult

	warning, violated := ValidateReadBeforeWrite(proposed, previousResults)
	if violated {
		t.Fatalf("expected violated=false when only read tools proposed, got warning=%q", warning)
	}
}

func TestValidateReadBeforeWrite_FailedReadThenWrite(t *testing.T) {
	proposed := []providers.ToolCall{
		{ID: "call_2", Name: "calendar.createEvent"},
	}
	previousResults := []contracts.ToolResult{
		{
			ToolCallID: "call_1",
			ToolName:   "calendar.listEvents",
			Success:    false,
			Error:      &contracts.ErrorShape{Code: "INTERNAL_ERROR", Message: "calendar API unavailable"},
		},
	}

	warning, violated := ValidateReadBeforeWrite(proposed, previousResults)
	if !violated {
		t.Fatal("expected violated=true when only prior reads failed")
	}
	if warning == "" {
		t.Fatal("expected non-empty warning message")
	}
}

func TestValidateReadBeforeWrite_NonWorkspaceTools(t *testing.T) {
	proposed := []providers.ToolCall{
		{ID: "call_1", Name: "sandbox.runPython"},
		{ID: "call_2", Name: "filesystem.writeFile"},
	}
	var previousResults []contracts.ToolResult

	warning, violated := ValidateReadBeforeWrite(proposed, previousResults)
	if violated {
		t.Fatalf("expected violated=false for non-workspace tools, got warning=%q", warning)
	}
}

func TestHasSuccessfulWorkspaceRead(t *testing.T) {
	tests := []struct {
		name    string
		results []contracts.ToolResult
		want    bool
	}{
		{
			name:    "nil results",
			results: nil,
			want:    false,
		},
		{
			name:    "empty results",
			results: []contracts.ToolResult{},
			want:    false,
		},
		{
			name: "single successful read",
			results: []contracts.ToolResult{
				{ToolCallID: "c1", ToolName: "gmail.listEmails", Success: true},
			},
			want: true,
		},
		{
			name: "single failed read",
			results: []contracts.ToolResult{
				{ToolCallID: "c1", ToolName: "gmail.listEmails", Success: false, Error: &contracts.ErrorShape{Code: "INTERNAL_ERROR", Message: "fail"}},
			},
			want: false,
		},
		{
			name: "failed read then successful read",
			results: []contracts.ToolResult{
				{ToolCallID: "c1", ToolName: "gmail.listEmails", Success: false, Error: &contracts.ErrorShape{Code: "INTERNAL_ERROR", Message: "fail"}},
				{ToolCallID: "c2", ToolName: "drive.listFiles", Success: true},
			},
			want: true,
		},
		{
			name: "successful write only",
			results: []contracts.ToolResult{
				{ToolCallID: "c1", ToolName: "gmail.createDraft", Success: true},
			},
			want: false,
		},
		{
			name: "non-workspace tool succeeds",
			results: []contracts.ToolResult{
				{ToolCallID: "c1", ToolName: "sandbox.runPython", Success: true},
			},
			want: false,
		},
		{
			name: "mixed workspace and non-workspace with one successful read",
			results: []contracts.ToolResult{
				{ToolCallID: "c1", ToolName: "sandbox.runPython", Success: true},
				{ToolCallID: "c2", ToolName: "calendar.listEvents", Success: true},
				{ToolCallID: "c3", ToolName: "gmail.createDraft", Success: true},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasSuccessfulWorkspaceRead(tt.results); got != tt.want {
				t.Errorf("HasSuccessfulWorkspaceRead() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyToolCallPhase(t *testing.T) {
	tests := []struct {
		name string
		tool string
		want string
	}{
		{name: "gmail read", tool: "gmail.listEmails", want: "read"},
		{name: "drive read", tool: "drive.getFile", want: "read"},
		{name: "web search", tool: "web.search", want: "read"},
		{name: "gmail write", tool: "gmail.createDraft", want: "write"},
		{name: "calendar write", tool: "calendar.createEvent", want: "write"},
		{name: "chat write", tool: "chat.sendMessage", want: "write"},
		{name: "unknown tool", tool: "sandbox.runPython", want: "other"},
		{name: "empty string", tool: "", want: "other"},
		{name: "filesystem tool", tool: "filesystem.readFile", want: "other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyToolCallPhase(tt.tool); got != tt.want {
				t.Errorf("ClassifyToolCallPhase(%q) = %q, want %q", tt.tool, got, tt.want)
			}
		})
	}
}

func TestSummarizeFlowSteps(t *testing.T) {
	results := []contracts.ToolResult{
		{ToolCallID: "c1", ToolName: "gmail.listEmails", Success: true},
		{ToolCallID: "c2", ToolName: "drive.getFile", Success: false, Error: &contracts.ErrorShape{Code: "INTERNAL_ERROR", Message: "file not found"}},
		{ToolCallID: "c3", ToolName: "gmail.createDraft", Success: true},
		{ToolCallID: "c4", ToolName: "sandbox.runPython", Success: true},
		{ToolCallID: "c5", ToolName: "chat.sendMessage", Success: false, Error: &contracts.ErrorShape{Code: "PROVIDER_TIMEOUT", Message: "request timed out"}},
	}

	steps := SummarizeFlowSteps(results)

	if len(steps) != len(results) {
		t.Fatalf("SummarizeFlowSteps returned %d steps, want %d", len(steps), len(results))
	}

	// Step 0: successful read
	assertFlowStep(t, steps[0], "gmail.listEmails", "read", true, "")

	// Step 1: failed read
	assertFlowStep(t, steps[1], "drive.getFile", "read", false, "file not found")

	// Step 2: successful write
	assertFlowStep(t, steps[2], "gmail.createDraft", "write", true, "")

	// Step 3: non-workspace tool
	assertFlowStep(t, steps[3], "sandbox.runPython", "other", true, "")

	// Step 4: failed write
	assertFlowStep(t, steps[4], "chat.sendMessage", "write", false, "request timed out")
}

func TestSummarizeFlowSteps_Empty(t *testing.T) {
	steps := SummarizeFlowSteps(nil)
	if len(steps) != 0 {
		t.Fatalf("SummarizeFlowSteps(nil) returned %d steps, want 0", len(steps))
	}

	steps = SummarizeFlowSteps([]contracts.ToolResult{})
	if len(steps) != 0 {
		t.Fatalf("SummarizeFlowSteps(empty) returned %d steps, want 0", len(steps))
	}
}

func assertFlowStep(t *testing.T, step FlowStep, wantName, wantPhase string, wantSuccess bool, wantErrorMsg string) {
	t.Helper()
	if step.ToolName != wantName {
		t.Errorf("FlowStep.ToolName = %q, want %q", step.ToolName, wantName)
	}
	if step.Phase != wantPhase {
		t.Errorf("FlowStep.Phase = %q, want %q", step.Phase, wantPhase)
	}
	if step.Success != wantSuccess {
		t.Errorf("FlowStep.Success = %v, want %v", step.Success, wantSuccess)
	}
	if step.ErrorMsg != wantErrorMsg {
		t.Errorf("FlowStep.ErrorMsg = %q, want %q", step.ErrorMsg, wantErrorMsg)
	}
}
