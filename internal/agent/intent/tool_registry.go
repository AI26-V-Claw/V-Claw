package intent

import (
	"fmt"
	"regexp"

	"vclaw/internal/contracts"
)

// ToolCategory classifies tools by their risk level.
type ToolCategory string

const (
	CategorySafeRead       ToolCategory = "SAFE_READ"
	CategoryDangerousWrite ToolCategory = "DANGEROUS_WRITE"
	CategoryExecution      ToolCategory = "EXECUTION"
	CategoryCommunication  ToolCategory = "COMMUNICATION"
)

// ToolDefinition defines classifier-facing metadata for a tool.
// It intentionally mirrors the contract fields that affect safety decisions
// so this helper does not drift from docs/03-contracts.md.
type ToolDefinition struct {
	Name             string
	Owner            string
	Category         ToolCategory
	Description      string
	DefaultRiskLevel contracts.RiskLevel
	RequiresApproval bool
	Parameters       []ParamDef
	Dangerous        bool
	RequiresConfirm  bool
	TimeoutMs        int
}

// ParamDef defines a single parameter for a tool.
type ParamDef struct {
	Name        string
	Type        string // "string", "int", "bool", "path", "email"
	Required    bool
	Description string
}

// Registry maps tool names to classifier-facing definitions for the G3 intent scope.
// LookupTool normalizes the contract fields that affect safety decisions.
var Registry = map[string]ToolDefinition{
	"gmail.listEmails": {
		Name: "gmail.listEmails", Category: CategorySafeRead,
		Description: "List emails from Gmail",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "query", Type: "string", Required: false, Description: "Email search query"}},
	},
	"gmail.getEmail": {
		Name: "gmail.getEmail", Category: CategorySafeRead,
		Description: "Get an email from Gmail",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "id", Type: "string", Required: false, Description: "Email ID"}},
	},
	"gmail.listLabels": {
		Name: "gmail.listLabels", Category: CategorySafeRead,
		Description: "List Gmail labels",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{},
	},
	"gmail.getProfile": {
		Name: "gmail.getProfile", Category: CategorySafeRead,
		Description: "Read the Gmail account profile",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{},
	},
	"gmail.listThreads": {
		Name: "gmail.listThreads", Category: CategorySafeRead,
		Description: "List Gmail threads",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "query", Type: "string", Required: false, Description: "Gmail thread search query"}},
	},
	"gmail.getThread": {
		Name: "gmail.getThread", Category: CategorySafeRead,
		Description: "Get a Gmail thread",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "id", Type: "string", Required: false, Description: "Gmail thread ID"}},
	},
	"gmail.listDrafts": {
		Name: "gmail.listDrafts", Category: CategorySafeRead,
		Description: "List Gmail drafts",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "maxResults", Type: "int", Required: false, Description: "Maximum drafts to list"}},
	},
	"gmail.getDraft": {
		Name: "gmail.getDraft", Category: CategorySafeRead,
		Description: "Read a Gmail draft",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "draftId", Type: "string", Required: true, Description: "Draft ID"}},
	},
	"calendar.listEvents": {
		Name: "calendar.listEvents", Category: CategorySafeRead,
		Description: "List events from Google Calendar",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "date", Type: "string", Required: false, Description: "Date to query (ISO-8601)"}},
	},
	"chat.listMessages": {
		Name: "chat.listMessages", Category: CategorySafeRead,
		Description: "List Google Chat messages",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "space", Type: "string", Required: false, Description: "Chat space"}},
	},
	"chat.listSpaces": {
		Name: "chat.listSpaces", Category: CategorySafeRead,
		Description: "List Google Chat spaces",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "pageSize", Type: "int", Required: false, Description: "Maximum spaces to list"}},
	},
	"chat.listMembers": {
		Name: "chat.listMembers", Category: CategorySafeRead,
		Description: "List members in a Google Chat space",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "space", Type: "string", Required: true, Description: "Chat space resource name"}},
	},
	"chat.findSpacesByMembers": {
		Name: "chat.findSpacesByMembers", Category: CategorySafeRead,
		Description: "Find Google Chat spaces containing resolved member resource names",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "members", Type: "string", Required: true, Description: "Comma-separated users/... member resources"}},
	},
	"people.searchDirectory": {
		Name: "people.searchDirectory", Category: CategorySafeRead,
		Description: "Search Google Workspace directory profiles",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "query", Type: "string", Required: true, Description: "Name or email to search"}},
	},
	"drive.listFiles": {
		Name: "drive.listFiles", Category: CategorySafeRead,
		Description: "List Google Drive files",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "query", Type: "string", Required: false, Description: "Drive search query"}},
	},
	"drive.getFile": {
		Name: "drive.getFile", Category: CategorySafeRead,
		Description: "Read Google Drive file metadata",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "fileId", Type: "string", Required: true, Description: "Drive file ID"}},
	},
	"drive.exportFile": {
		Name: "drive.exportFile", Category: CategorySafeRead,
		Description: "Export Google Workspace Drive file content with a size cap",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "fileId", Type: "string", Required: true, Description: "Drive file ID"}, {Name: "mimeType", Type: "string", Required: false, Description: "Export MIME type"}},
	},
	"drive.downloadFile": {
		Name: "drive.downloadFile", Category: CategorySafeRead,
		Description: "Download Drive file content with a size cap into the tool response",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "fileId", Type: "string", Required: true, Description: "Drive file ID"}, {Name: "maxBytes", Type: "int", Required: false, Description: "Maximum bytes to return"}},
	},
	"drive.listPermissions": {
		Name: "drive.listPermissions", Category: CategorySafeRead,
		Description: "List Google Drive file sharing permissions",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "fileId", Type: "string", Required: true, Description: "Drive file ID"}},
	},
	"docs.getDocument": {
		Name: "docs.getDocument", Category: CategorySafeRead,
		Description: "Read a Google Docs document",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "documentId", Type: "string", Required: true, Description: "Document ID"}},
	},
	"sheets.getSpreadsheet": {
		Name: "sheets.getSpreadsheet", Category: CategorySafeRead,
		Description: "Read Google Sheets spreadsheet metadata",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "spreadsheetId", Type: "string", Required: true, Description: "Spreadsheet ID"}},
	},
	"sheets.readValues": {
		Name: "sheets.readValues", Category: CategorySafeRead,
		Description: "Read values from a Google Sheets range",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "spreadsheetId", Type: "string", Required: true, Description: "Spreadsheet ID"}, {Name: "range", Type: "string", Required: true, Description: "A1 range"}},
	},
	"sheets.batchGetValues": {
		Name: "sheets.batchGetValues", Category: CategorySafeRead,
		Description: "Read values from multiple Google Sheets ranges",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "spreadsheetId", Type: "string", Required: true, Description: "Spreadsheet ID"}, {Name: "ranges", Type: "string", Required: true, Description: "A1 ranges"}},
	},
	"web.search": {
		Name: "web.search", Category: CategorySafeRead,
		Description: "Search the public web through the configured web provider",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "query", Type: "string", Required: true, Description: "Public web search query"}},
	},
	"web.fetch": {
		Name: "web.fetch", Category: CategorySafeRead,
		Description: "Fetch readable content from a public HTTP or HTTPS URL",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "url", Type: "string", Required: true, Description: "Public URL to fetch"}},
	},
	"calendar.createEvent": {
		Name: "calendar.createEvent", Category: CategoryDangerousWrite,
		Description: "Create a new calendar event",
		Dangerous:   true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "title", Type: "string", Required: true, Description: "Event title"}, {Name: "start", Type: "string", Required: true, Description: "Start time (ISO-8601)"}},
	},
	"calendar.updateEvent": {
		Name: "calendar.updateEvent", Category: CategoryDangerousWrite,
		Description: "Update a calendar event",
		Dangerous:   true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "eventId", Type: "string", Required: true, Description: "Event ID"}},
	},
	"calendar.deleteEvent": {
		Name: "calendar.deleteEvent", Category: CategoryDangerousWrite,
		Description: "Delete a calendar event",
		Dangerous:   true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "eventId", Type: "string", Required: true, Description: "Event ID"}},
	},
	"sandbox.runPython": {
		Name: "sandbox.runPython", Category: CategoryExecution,
		Description: "Run Python code or a workspace-relative Python script in sandbox",
		Dangerous:   true, RequiresConfirm: true, TimeoutMs: 120000,
		Parameters: []ParamDef{
			{Name: "code", Type: "string", Required: true, Description: "Python code to run"},
			{Name: "script_path", Type: "path", Required: false, Description: "Workspace-relative Python script path"},
			{Name: "workingDir", Type: "path", Required: false, Description: "Sandbox workspace directory"},
			{Name: "timeout_seconds", Type: "int", Required: false, Description: "Execution timeout in seconds"},
		},
	},
	"sandbox.runShell": {
		Name: "sandbox.runShell", Category: CategoryExecution,
		Description: "Run shell command in sandbox",
		Dangerous:   true, RequiresConfirm: true, TimeoutMs: 120000,
		Parameters: []ParamDef{
			{Name: "command", Type: "string", Required: true, Description: "Shell command to run"},
			{Name: "workingDir", Type: "path", Required: false, Description: "Sandbox workspace directory"},
			{Name: "timeout_seconds", Type: "int", Required: false, Description: "Execution timeout in seconds"},
		},
	},
	"gmail.createDraft": {
		Name: "gmail.createDraft", Category: CategoryCommunication,
		Description: "Create a Gmail draft",
		Dangerous:   true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "to", Type: "email", Required: true, Description: "Recipient email address"}, {Name: "subject", Type: "string", Required: true, Description: "Email subject"}, {Name: "body", Type: "string", Required: true, Description: "Email body"}},
	},
	"gmail.updateDraft": {
		Name: "gmail.updateDraft", Category: CategoryCommunication,
		Description: "Update a Gmail draft",
		Dangerous:   true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "id", Type: "string", Required: true, Description: "Draft ID"}, {Name: "to", Type: "email", Required: false, Description: "Recipient email address"}, {Name: "subject", Type: "string", Required: true, Description: "Email subject"}, {Name: "body", Type: "string", Required: false, Description: "Email body"}},
	},
	"gmail.sendDraft": {
		Name: "gmail.sendDraft", Category: CategoryCommunication,
		Description: "Send an existing Gmail draft",
		Dangerous:   true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "id", Type: "string", Required: true, Description: "Draft ID"}},
	},
	"gmail.deleteDraft": {
		Name: "gmail.deleteDraft", Category: CategoryDangerousWrite,
		Description:      "Delete an existing Gmail draft",
		DefaultRiskLevel: contracts.RiskLevelDestructive,
		Dangerous:        true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "draftId", Type: "string", Required: true, Description: "Draft ID"}},
	},
	"gmail.replyDraft": {
		Name: "gmail.replyDraft", Category: CategoryCommunication,
		Description: "Create a Gmail reply draft",
		Dangerous:   true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "id", Type: "string", Required: true, Description: "Message ID"}, {Name: "body", Type: "string", Required: true, Description: "Reply body"}},
	},
	"gmail.forwardDraft": {
		Name: "gmail.forwardDraft", Category: CategoryCommunication,
		Description: "Create a Gmail forward draft",
		Dangerous:   true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "id", Type: "string", Required: true, Description: "Message ID"}, {Name: "to", Type: "email", Required: true, Description: "Recipient email address"}},
	},
	"gmail.downloadAttachments": {
		Name: "gmail.downloadAttachments", Category: CategoryDangerousWrite,
		Description:      "Download Gmail attachments to local files",
		DefaultRiskLevel: contracts.RiskLevelLocalWrite,
		Dangerous:        true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "id", Type: "string", Required: true, Description: "Message ID"}, {Name: "outputDir", Type: "path", Required: true, Description: "Local output directory"}},
	},
	"gmail.modifyMessage": {
		Name: "gmail.modifyMessage", Category: CategoryDangerousWrite,
		Description: "Modify Gmail message labels or state",
		Dangerous:   true, RequiresConfirm: true, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "id", Type: "string", Required: true, Description: "Message ID"}, {Name: "action", Type: "string", Required: true, Description: "markRead, markUnread, archive, star, unstar, addLabels, removeLabels"}},
	},
	"gmail.batchModifyMessages": {
		Name: "gmail.batchModifyMessages", Category: CategoryDangerousWrite,
		Description: "Modify Gmail labels or state for multiple messages",
		Dangerous:   true, RequiresConfirm: true, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "messageIds", Type: "string", Required: true, Description: "Message IDs"}, {Name: "action", Type: "string", Required: true, Description: "markRead, markUnread, archive, star, unstar, addLabels, removeLabels"}},
	},
	"gmail.trashMessage": {
		Name: "gmail.trashMessage", Category: CategoryDangerousWrite,
		Description:      "Move a Gmail message to trash",
		DefaultRiskLevel: contracts.RiskLevelDestructive,
		Dangerous:        true, RequiresConfirm: true, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "messageId", Type: "string", Required: true, Description: "Message ID"}},
	},
	"gmail.untrashMessage": {
		Name: "gmail.untrashMessage", Category: CategoryDangerousWrite,
		Description: "Restore a Gmail message from trash",
		Dangerous:   true, RequiresConfirm: true, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "messageId", Type: "string", Required: true, Description: "Message ID"}},
	},
	"drive.createFolder": {
		Name: "drive.createFolder", Category: CategoryDangerousWrite,
		Description: "Create a Google Drive folder",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "name", Type: "string", Required: true, Description: "Folder name"}},
	},
	"drive.createFile": {
		Name: "drive.createFile", Category: CategoryDangerousWrite,
		Description: "Create a Google Drive file from provided content",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "name", Type: "string", Required: true, Description: "File name"}, {Name: "content", Type: "string", Required: true, Description: "File content"}},
	},
	"drive.uploadFile": {
		Name: "drive.uploadFile", Category: CategoryDangerousWrite,
		Description: "Upload a local file to Google Drive",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "localPath", Type: "path", Required: true, Description: "Local file path"}, {Name: "name", Type: "string", Required: false, Description: "Drive file name"}},
	},
	"drive.updateFileMetadata": {
		Name: "drive.updateFileMetadata", Category: CategoryDangerousWrite,
		Description: "Update Google Drive file metadata only; do not use for moving files or changing folders",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "fileId", Type: "string", Required: true, Description: "Drive file ID"}},
	},
	"drive.shareFile": {
		Name: "drive.shareFile", Category: CategoryDangerousWrite,
		Description: "Share a Google Drive file",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "fileId", Type: "string", Required: true, Description: "Drive file ID"}, {Name: "role", Type: "string", Required: true, Description: "reader, commenter, or writer"}},
	},
	"drive.revokePermission": {
		Name: "drive.revokePermission", Category: CategoryDangerousWrite,
		Description: "Revoke a Google Drive sharing permission",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "fileId", Type: "string", Required: true, Description: "Drive file ID"}, {Name: "permissionId", Type: "string", Required: true, Description: "Permission ID"}},
	},
	"drive.moveFile": {
		Name: "drive.moveFile", Category: CategoryDangerousWrite,
		Description: "Move a Google Drive file or folder into another Drive folder",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "fileId", Type: "string", Required: true, Description: "Drive file ID"}, {Name: "targetParentId", Type: "string", Required: true, Description: "Destination folder ID"}},
	},
	"drive.trashFile": {
		Name: "drive.trashFile", Category: CategoryDangerousWrite,
		Description:      "Move a Google Drive file or folder to trash",
		DefaultRiskLevel: contracts.RiskLevelDestructive,
		Dangerous:        true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "fileId", Type: "string", Required: true, Description: "Drive file ID"}},
	},
	"drive.untrashFile": {
		Name: "drive.untrashFile", Category: CategoryDangerousWrite,
		Description: "Restore a Google Drive file or folder from trash",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "fileId", Type: "string", Required: true, Description: "Drive file ID"}},
	},
	"docs.createDocument": {
		Name: "docs.createDocument", Category: CategoryDangerousWrite,
		Description: "Create a Google Docs document",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "title", Type: "string", Required: true, Description: "Document title"}},
	},
	"docs.appendText": {
		Name: "docs.appendText", Category: CategoryDangerousWrite,
		Description: "Append text to a Google Docs document",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "documentId", Type: "string", Required: true, Description: "Document ID"}, {Name: "text", Type: "string", Required: true, Description: "Text to append"}},
	},
	"docs.replaceText": {
		Name: "docs.replaceText", Category: CategoryDangerousWrite,
		Description: "Replace matching text in a Google Docs document",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "documentId", Type: "string", Required: true, Description: "Document ID"}, {Name: "oldText", Type: "string", Required: true, Description: "Text to find"}, {Name: "newText", Type: "string", Required: true, Description: "Replacement text"}},
	},
	"docs.insertText": {
		Name: "docs.insertText", Category: CategoryDangerousWrite,
		Description: "Insert text at a Google Docs structural index",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "documentId", Type: "string", Required: true, Description: "Document ID"}, {Name: "index", Type: "int", Required: true, Description: "Document structural index"}, {Name: "text", Type: "string", Required: true, Description: "Text to insert"}},
	},
	"docs.deleteContent": {
		Name: "docs.deleteContent", Category: CategoryDangerousWrite,
		Description: "Delete content from a Google Docs structural range",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "documentId", Type: "string", Required: true, Description: "Document ID"}, {Name: "startIndex", Type: "int", Required: true, Description: "Start index"}, {Name: "endIndex", Type: "int", Required: true, Description: "End index"}},
	},
	"sheets.createSpreadsheet": {
		Name: "sheets.createSpreadsheet", Category: CategoryDangerousWrite,
		Description: "Create a Google Sheets spreadsheet",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "title", Type: "string", Required: true, Description: "Spreadsheet title"}},
	},
	"sheets.updateValues": {
		Name: "sheets.updateValues", Category: CategoryDangerousWrite,
		Description: "Update values in a Google Sheets range",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "spreadsheetId", Type: "string", Required: true, Description: "Spreadsheet ID"}, {Name: "range", Type: "string", Required: true, Description: "A1 range"}},
	},
	"sheets.batchUpdateValues": {
		Name: "sheets.batchUpdateValues", Category: CategoryDangerousWrite,
		Description: "Update values in multiple Google Sheets ranges",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "spreadsheetId", Type: "string", Required: true, Description: "Spreadsheet ID"}, {Name: "ranges", Type: "string", Required: true, Description: "Map of A1 ranges to values"}},
	},
	"sheets.appendValues": {
		Name: "sheets.appendValues", Category: CategoryDangerousWrite,
		Description: "Append rows to a Google Sheets range",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "spreadsheetId", Type: "string", Required: true, Description: "Spreadsheet ID"}, {Name: "range", Type: "string", Required: true, Description: "A1 range"}},
	},
	"sheets.clearValues": {
		Name: "sheets.clearValues", Category: CategoryDangerousWrite,
		Description: "Clear values from a Google Sheets range",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "spreadsheetId", Type: "string", Required: true, Description: "Spreadsheet ID"}, {Name: "range", Type: "string", Required: true, Description: "A1 range"}},
	},
	"sheets.addSheet": {
		Name: "sheets.addSheet", Category: CategoryDangerousWrite,
		Description: "Add a sheet tab to a Google Sheets spreadsheet",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "spreadsheetId", Type: "string", Required: true, Description: "Spreadsheet ID"}, {Name: "title", Type: "string", Required: true, Description: "New sheet title"}},
	},
	"sheets.renameSheet": {
		Name: "sheets.renameSheet", Category: CategoryDangerousWrite,
		Description: "Rename a Google Sheets tab",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "spreadsheetId", Type: "string", Required: true, Description: "Spreadsheet ID"}, {Name: "sheetId", Type: "int", Required: true, Description: "Sheet tab ID"}, {Name: "title", Type: "string", Required: true, Description: "New sheet title"}},
	},
	"sheets.deleteSheet": {
		Name: "sheets.deleteSheet", Category: CategoryDangerousWrite,
		Description:      "Delete a Google Sheets tab",
		DefaultRiskLevel: contracts.RiskLevelDestructive,
		Dangerous:        true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "spreadsheetId", Type: "string", Required: true, Description: "Spreadsheet ID"}, {Name: "sheetId", Type: "int", Required: true, Description: "Sheet tab ID"}},
	},
	"sheets.duplicateSheet": {
		Name: "sheets.duplicateSheet", Category: CategoryDangerousWrite,
		Description: "Duplicate a Google Sheets tab",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "spreadsheetId", Type: "string", Required: true, Description: "Spreadsheet ID"}, {Name: "sourceSheetId", Type: "int", Required: true, Description: "Source sheet tab ID"}, {Name: "newTitle", Type: "string", Required: true, Description: "New sheet title"}},
	},
	"chat.sendMessage": {
		Name: "chat.sendMessage", Category: CategoryCommunication,
		Description: "Send a chat message",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "recipient", Type: "string", Required: true, Description: "Recipient user or space"}, {Name: "message", Type: "string", Required: true, Description: "Message content"}},
	},
	"chat.updateMessage": {
		Name: "chat.updateMessage", Category: CategoryCommunication,
		Description: "Update a Google Chat message",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "name", Type: "string", Required: true, Description: "Message resource name"}, {Name: "text", Type: "string", Required: true, Description: "Updated message text"}},
	},
	"chat.deleteMessage": {
		Name: "chat.deleteMessage", Category: CategoryDangerousWrite,
		Description:      "Delete a Google Chat message",
		DefaultRiskLevel: contracts.RiskLevelDestructive,
		Dangerous:        true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "name", Type: "string", Required: true, Description: "Message resource name"}},
	},
	"chat.createSpace": {
		Name: "chat.createSpace", Category: CategoryCommunication,
		Description: "Create or set up a Google Chat space",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "spaceType", Type: "string", Required: false, Description: "SPACE, GROUP_CHAT, or DIRECT_MESSAGE"}, {Name: "memberUsers", Type: "string", Required: false, Description: "Workspace member emails"}},
	},
	"chat.addMember": {
		Name: "chat.addMember", Category: CategoryCommunication,
		Description: "Add a member to a Google Chat space",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "space", Type: "string", Required: true, Description: "Chat space resource name"}, {Name: "user", Type: "email", Required: true, Description: "Workspace user email"}},
	},
	"chat.removeMember": {
		Name: "chat.removeMember", Category: CategoryDangerousWrite,
		Description:      "Remove a member from a Google Chat space",
		DefaultRiskLevel: contracts.RiskLevelDestructive,
		Dangerous:        true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "name", Type: "string", Required: true, Description: "Membership resource name"}},
	},
}

// LookupTool retrieves a tool definition by name.
func LookupTool(name string) (ToolDefinition, error) {
	name = NormalizeToolName(name)
	tool, ok := Registry[name]
	if !ok {
		return ToolDefinition{}, fmt.Errorf("tool %q not found in registry", name)
	}
	return normalizeToolDefinition(tool), nil
}

func normalizeToolDefinition(tool ToolDefinition) ToolDefinition {
	if tool.Owner == "" {
		tool.Owner = ownerForTool(tool.Name)
	}
	if tool.DefaultRiskLevel == "" {
		tool.DefaultRiskLevel = riskLevelForCategory(tool.Category, tool.Name)
	}
	if !tool.RequiresApproval && tool.RequiresConfirm {
		tool.RequiresApproval = true
	}
	if tool.TimeoutMs == 0 {
		tool.TimeoutMs = 30000
	}
	return tool
}

func ownerForTool(name string) string {
	switch {
	case hasToolPrefix(name, "sandbox."):
		return "agent_core"
	default:
		return "integration"
	}
}

func riskLevelForCategory(category ToolCategory, name string) contracts.RiskLevel {
	switch category {
	case CategorySafeRead:
		return contracts.RiskLevelSafeRead
	case CategoryExecution:
		return contracts.RiskLevelCodeExecution
	case CategoryDangerousWrite, CategoryCommunication:
		if name == "calendar.deleteEvent" || name == "gmail.deleteDraft" || name == "gmail.trashMessage" || name == "chat.deleteMessage" || name == "chat.removeMember" || name == "drive.trashFile" {
			return contracts.RiskLevelDestructive
		}
		return contracts.RiskLevelExternalWrite
	default:
		return contracts.RiskLevelDestructive
	}
}

func hasToolPrefix(name, prefix string) bool {
	return len(name) >= len(prefix) && name[:len(prefix)] == prefix
}

// IsDangerous checks if a tool is classified as dangerous.
func IsDangerous(toolName string) bool {
	tool, err := LookupTool(toolName)
	if err != nil {
		return false
	}
	return tool.Dangerous
}

// ValidateEmail validates an email address format.
func ValidateEmail(email interface{}) error {
	emailStr, ok := email.(string)
	if !ok {
		return fmt.Errorf("email must be a string")
	}
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(emailStr) {
		return fmt.Errorf("invalid email format: %s", emailStr)
	}
	return nil
}
