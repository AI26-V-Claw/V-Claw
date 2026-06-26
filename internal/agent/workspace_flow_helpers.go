// workspace_flow_helpers.go provides helper functions for validating and
// classifying workspace multi-tool flows. It maintains canonical maps of
// workspace read and write tool names and exposes utilities for enforcing
// read-before-write ordering, classifying tool call phases, and summarising
// completed flow steps.
package agent

import (
	"vclaw/internal/contracts"
	"vclaw/internal/providers"
)

// workspaceReadTools is the canonical set of workspace tool names that perform
// read-only operations.
var workspaceReadTools = map[string]bool{
	// Gmail
	"gmail.listEmails": true,
	"gmail.getEmail":   true,
	"gmail.getThread":  true,
	"gmail.listThreads": true,
	"gmail.listDrafts":  true,
	"gmail.getDraft":    true,
	"gmail.getProfile":  true,
	"gmail.listLabels":  true,

	// Calendar
	"calendar.listEvents": true,

	// Drive
	"drive.listFiles":    true,
	"drive.getFile":      true,
	"drive.exportFile":   true,
	"drive.downloadFile": true,

	// Docs
	"docs.getDocument": true,

	// Sheets
	"sheets.getSpreadsheet": true,
	"sheets.readValues":     true,

	// Chat
	"chat.listSpaces":          true,
	"chat.listMembers":         true,
	"chat.findSpacesByMembers": true,
	"chat.listMessages":        true,

	// People
	"people.searchDirectory": true,

	// Web
	"web.search": true,
}

// workspaceWriteTools is the canonical set of workspace tool names that perform
// mutating operations.
var workspaceWriteTools = map[string]bool{
	// Gmail
	"gmail.createDraft":         true,
	"gmail.sendDraft":           true,
	"gmail.replyDraft":          true,
	"gmail.forwardDraft":        true,
	"gmail.modifyMessage":       true,
	"gmail.batchModifyMessages": true,
	"gmail.trashMessage":        true,
	"gmail.untrashMessage":      true,

	// Calendar
	"calendar.createEvent":  true,
	"calendar.updateEvent":  true,
	"calendar.deleteEvent":  true,
	"calendar.respondEvent": true,

	// Drive
	"drive.createFile":   true,
	"drive.uploadFile":   true,
	"drive.createFolder": true,
	"drive.moveFile":     true,
	"drive.moveFiles":    true,

	// Docs
	"docs.createDocument": true,
	"docs.appendText":     true,
	"docs.replaceText":    true,

	// Sheets
	"sheets.createSpreadsheet": true,
	"sheets.updateValues":      true,

	// Chat
	"chat.sendMessage":   true,
	"chat.updateMessage": true,
	"chat.deleteMessage": true,
	"chat.createSpace":   true,
	"chat.addMember":     true,
	"chat.removeMember":  true,
}

// IsWorkspaceReadTool reports whether name is a recognised workspace read tool.
func IsWorkspaceReadTool(name string) bool {
	return workspaceReadTools[name]
}

// IsWorkspaceWriteTool reports whether name is a recognised workspace write tool.
func IsWorkspaceWriteTool(name string) bool {
	return workspaceWriteTools[name]
}

// ValidateReadBeforeWrite checks whether any proposed write tool call would
// execute without a prior successful workspace read in previousResults. If so
// it returns a human-readable warning and violated=true.
func ValidateReadBeforeWrite(proposedCalls []providers.ToolCall, previousResults []contracts.ToolResult) (warning string, violated bool) {
	hasRead := HasSuccessfulWorkspaceRead(previousResults)
	if hasRead {
		return "", false
	}
	for _, call := range proposedCalls {
		if IsWorkspaceWriteTool(call.Name) {
			return "You should read relevant data before writing. " +
				"Call read tools (gmail.listEmails, calendar.listEvents, drive.listFiles, etc.) first, " +
				"then use the results to compose your write action.", true
		}
	}
	return "", false
}

// HasSuccessfulWorkspaceRead reports whether results contain at least one
// successful execution of a workspace read tool.
func HasSuccessfulWorkspaceRead(results []contracts.ToolResult) bool {
	for _, r := range results {
		if r.Success && IsWorkspaceReadTool(r.ToolName) {
			return true
		}
	}
	return false
}

// ClassifyToolCallPhase returns the phase label for a tool name: "read" for
// workspace read tools, "write" for workspace write tools, or "other" for
// everything else.
func ClassifyToolCallPhase(name string) string {
	switch {
	case IsWorkspaceReadTool(name):
		return "read"
	case IsWorkspaceWriteTool(name):
		return "write"
	default:
		return "other"
	}
}

// FlowStep summarises a single completed tool execution within a multi-tool
// workspace flow.
type FlowStep struct {
	ToolName string
	Phase    string
	Success  bool
	ErrorMsg string
}

// SummarizeFlowSteps creates a slice of FlowStep from completed tool results,
// capturing the tool name, phase classification, success state, and any error
// message for each step.
func SummarizeFlowSteps(results []contracts.ToolResult) []FlowStep {
	steps := make([]FlowStep, 0, len(results))
	for _, r := range results {
		step := FlowStep{
			ToolName: r.ToolName,
			Phase:    ClassifyToolCallPhase(r.ToolName),
			Success:  r.Success,
		}
		if r.Error != nil {
			step.ErrorMsg = r.Error.Message
		}
		steps = append(steps, step)
	}
	return steps
}
