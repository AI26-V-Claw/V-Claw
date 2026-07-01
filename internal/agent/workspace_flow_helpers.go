// workspace_flow_helpers.go provides helper functions for validating
// workspace multi-tool flows.  Classification is registry-driven: tools
// are identified as workspace read/write by their registered Capability
// and a namespace prefix check, so the list stays in sync with the tool
// registry automatically.
package agent

import (
	"strings"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
)

// workspaceNamespaces lists the Google-Workspace (and web) tool prefixes
// that the read-before-write guard applies to.  Tools outside these
// namespaces (sandbox.*, filesystem.*, memory.*, etc.) are ignored by the
// guard.
var workspaceNamespaces = []string{
	"gmail.",
	"calendar.",
	"drive.",
	"docs.",
	"sheets.",
	"chat.",
	"people.",
	"web.",
	"meet.",
}

// isWorkspaceNamespace reports whether name belongs to a workspace namespace.
func isWorkspaceNamespace(name string) bool {
	for _, ns := range workspaceNamespaces {
		if strings.HasPrefix(name, ns) {
			return true
		}
	}
	return false
}

// IsWorkspaceReadTool reports whether name is a workspace read tool by
// checking its namespace and Capability in the registry.  If registry is
// nil or the tool is not registered, the function falls back to namespace +
// a safe default (unknown workspace tools are treated as non-read).
func IsWorkspaceReadTool(name string, registry *tools.ToolRegistry) bool {
	if !isWorkspaceNamespace(name) {
		return false
	}
	if registry == nil {
		return false
	}
	def, ok := registry.GetDefinition(name)
	if !ok {
		return false
	}
	return def.Capability == tools.CapabilityReadOnly
}

// IsWorkspaceWriteTool reports whether name is a workspace write tool by
// checking its namespace and Capability in the registry.
func IsWorkspaceWriteTool(name string, registry *tools.ToolRegistry) bool {
	if !isWorkspaceNamespace(name) {
		return false
	}
	if registry == nil {
		return false
	}
	def, ok := registry.GetDefinition(name)
	if !ok {
		return false
	}
	return def.Capability == tools.CapabilityMutating
}

// ValidateReadBeforeWrite checks whether any proposed write tool call
// would execute without a prior successful workspace read.
//
// It inspects both previousResults (from earlier iterations in this run)
// and the proposed batch itself.  If the batch contains a workspace read
// tool *before* the first workspace write tool, the batch is considered
// valid — the read will execute first and provide data for the write.
//
// Returns a human-readable warning and violated=true when the guard fires.
func ValidateReadBeforeWrite(
	proposedCalls []providers.ToolCall,
	previousResults []contracts.ToolResult,
	registry *tools.ToolRegistry,
) (warning string, violated bool) {
	// 1. Already has a successful read from a previous iteration → OK.
	if HasSuccessfulWorkspaceRead(previousResults, registry) {
		return "", false
	}

	// 2. Scan the proposed batch for the first read and first write.
	firstReadIdx := -1
	firstWriteIdx := -1
	for i, call := range proposedCalls {
		if firstReadIdx == -1 && IsWorkspaceReadTool(call.Name, registry) {
			firstReadIdx = i
		}
		if firstWriteIdx == -1 && IsWorkspaceWriteTool(call.Name, registry) {
			firstWriteIdx = i
		}
	}

	// No workspace write in this batch → nothing to guard.
	if firstWriteIdx == -1 {
		return "", false
	}

	// Batch contains a read before (or at the same position as) the first
	// write → the runtime will execute reads first, so allow the batch.
	if firstReadIdx != -1 && firstReadIdx <= firstWriteIdx {
		return "", false
	}

	return "You should read relevant data before writing. " +
		"Call read tools (gmail.listEmails, calendar.listEvents, drive.listFiles, etc.) first, " +
		"then use the results to compose your write action.", true
}

// SplitWorkspaceReadPrefix returns the leading workspace read calls before the
// first workspace write, plus the deferred suffix, when a mixed read/write
// response should be split into a read iteration followed by an LLM
// continuation. This prevents the model from pre-composing a write before it
// has seen the read results.
func SplitWorkspaceReadPrefix(
	proposedCalls []providers.ToolCall,
	previousResults []contracts.ToolResult,
	registry *tools.ToolRegistry,
) (readPrefix []providers.ToolCall, deferred []providers.ToolCall, ok bool) {
	if HasSuccessfulWorkspaceRead(previousResults, registry) {
		return nil, nil, false
	}
	firstWriteIdx := -1
	for i, call := range proposedCalls {
		if IsWorkspaceWriteTool(call.Name, registry) {
			firstWriteIdx = i
			break
		}
	}
	if firstWriteIdx <= 0 {
		return nil, nil, false
	}
	for _, call := range proposedCalls[:firstWriteIdx] {
		if !IsWorkspaceReadTool(call.Name, registry) {
			return nil, nil, false
		}
	}
	readPrefix = cloneProviderToolCalls(proposedCalls[:firstWriteIdx])
	deferred = cloneProviderToolCalls(proposedCalls[firstWriteIdx:])
	return readPrefix, deferred, len(readPrefix) > 0 && len(deferred) > 0
}

// HasSuccessfulWorkspaceRead reports whether results contain at least one
// successful execution of a workspace read tool.
func HasSuccessfulWorkspaceRead(results []contracts.ToolResult, registry *tools.ToolRegistry) bool {
	for _, r := range results {
		if r.Success && IsWorkspaceReadTool(r.ToolName, registry) {
			return true
		}
	}
	return false
}

// isCreateFromScratch detects whether a set of proposed workspace write
// calls are "create-from-scratch" — the user provided all the data in
// the message and the LLM does not need to read workspace content first.
//
// A write call is considered create-from-scratch when:
//   - It does not reference an existing resource ID (no documentId,
//     spreadsheetId, messageId, eventId, etc. in arguments).
//   - Its tool name is a "create" or "send" action, not an "update",
//     "delete", "modify", "replace", "append", or "move".
//
// If ANY write call in the batch is NOT create-from-scratch, the whole
// batch is considered data-dependent and requires a prior read.
func isCreateFromScratch(writeCalls []providers.ToolCall) bool {
	if len(writeCalls) == 0 {
		return false
	}

	// Resource-ID argument keys that indicate the call modifies an
	// existing resource (and therefore needs a read to discover the ID).
	resourceIDKeys := map[string]bool{
		"documentId":    true,
		"spreadsheetId": true,
		"messageId":     true,
		"eventId":       true,
		"fileId":        true,
		"draftId":       true,
		"space":         true,
		"spaceId":       true,
		"memberId":      true,
		"permissionId":  true,
	}

	// Action verbs that always operate on existing resources.
	modifyVerbs := []string{
		"update", "delete", "modify", "replace", "append",
		"move", "trash", "untrash", "revoke", "remove", "clear",
		"rename", "respond", "batch",
	}

	for _, call := range writeCalls {
		// Check for resource ID arguments.
		for key := range call.Arguments {
			if resourceIDKeys[key] {
				return false
			}
		}

		// Check for modify-verb in tool name (e.g. "docs.appendText",
		// "calendar.updateEvent", "drive.moveFile").
		nameLower := strings.ToLower(call.Name)
		for _, verb := range modifyVerbs {
			if strings.Contains(nameLower, verb) {
				return false
			}
		}
	}
	return true
}

// WorkspaceWriteCalls filters proposedCalls to only workspace write tools.
func WorkspaceWriteCalls(proposedCalls []providers.ToolCall, registry *tools.ToolRegistry) []providers.ToolCall {
	var writes []providers.ToolCall
	for _, call := range proposedCalls {
		if IsWorkspaceWriteTool(call.Name, registry) {
			writes = append(writes, call)
		}
	}
	return writes
}

// PriorSessionContextAllowsWorkspaceWrite reports whether a workspace write can
// safely proceed using artifacts from previous turns in the same session. The
// write still goes through normal risk/approval handling; this only prevents the
// read-before-write guard from blocking continuation requests such as appending
// an already-extracted PDF Markdown file into a Google Docs document that was
// created in the previous turn.
func PriorSessionContextAllowsWorkspaceWrite(
	proposedCalls []providers.ToolCall,
	memory sessions.SessionMemory,
	registry *tools.ToolRegistry,
) bool {
	writes := WorkspaceWriteCalls(proposedCalls, registry)
	if len(writes) == 0 {
		return false
	}
	for _, call := range writes {
		if !priorSessionContextAllowsWorkspaceWriteCall(call, memory) {
			return false
		}
	}
	return true
}

func priorSessionContextAllowsWorkspaceWriteCall(call providers.ToolCall, memory sessions.SessionMemory) bool {
	switch call.Name {
	case "docs.appendMarkdown":
		documentID := stringArgument(call.Arguments, "documentId")
		if documentID == "" || !memoryHasDocsDocument(memory, documentID) {
			return false
		}
		if stringArgument(call.Arguments, "markdown") != "" {
			return true
		}
		return memoryHasLocalFileRef(memory, stringArgument(call.Arguments, "localPath"))
	case "docs.appendText":
		documentID := stringArgument(call.Arguments, "documentId")
		if documentID == "" || !memoryHasDocsDocument(memory, documentID) {
			return false
		}
		if stringArgument(call.Arguments, "text") != "" {
			return true
		}
		return memoryHasLocalFileRef(memory, stringArgument(call.Arguments, "localPath"))
	default:
		return false
	}
}

func memoryHasDocsDocument(memory sessions.SessionMemory, documentID string) bool {
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return false
	}
	for _, result := range memory.LastActionResults {
		if result.Artifact == nil {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(result.Artifact.Kind))
		if result.Artifact.ID == documentID && (strings.Contains(kind, "docs.document") || strings.Contains(kind, "docs")) {
			return true
		}
	}
	return false
}

func memoryHasLocalFileRef(memory sessions.SessionMemory, localPath string) bool {
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return false
	}
	for _, ref := range memory.FileRefs {
		if strings.EqualFold(strings.TrimSpace(ref.Path), localPath) {
			return true
		}
	}
	for _, result := range memory.LastActionResults {
		if result.Artifact == nil {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(result.Artifact.Kind))
		if kind == "file" && strings.EqualFold(strings.TrimSpace(result.Artifact.URI), localPath) {
			return true
		}
	}
	return false
}
