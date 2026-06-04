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
	"web.search": {
		Name: "web.search", Category: CategorySafeRead,
		Description: "Search the public web for current information",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "query", Type: "string", Required: true, Description: "Search query"}},
	},
	"web.fetch": {
		Name: "web.fetch", Category: CategorySafeRead,
		Description: "Fetch and extract readable content from a public web page URL",
		Dangerous:   false, RequiresConfirm: false, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "url", Type: "string", Required: true, Description: "HTTP or HTTPS URL"}},
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
			{Name: "code", Type: "string", Required: false, Description: "Python code to run"},
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
		Parameters: []ParamDef{{Name: "id", Type: "string", Required: true, Description: "Draft ID"}, {Name: "to", Type: "email", Required: false, Description: "Recipient email address"}, {Name: "subject", Type: "string", Required: false, Description: "Email subject"}, {Name: "body", Type: "string", Required: false, Description: "Email body"}},
	},
	"gmail.sendDraft": {
		Name: "gmail.sendDraft", Category: CategoryCommunication,
		Description: "Send an existing Gmail draft",
		Dangerous:   true, RequiresConfirm: true, TimeoutMs: 60000,
		Parameters: []ParamDef{{Name: "id", Type: "string", Required: true, Description: "Draft ID"}},
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
	"chat.sendMessage": {
		Name: "chat.sendMessage", Category: CategoryCommunication,
		Description: "Send a chat message",
		Dangerous:   true, RequiresApproval: true, RequiresConfirm: true, TimeoutMs: 30000,
		Parameters: []ParamDef{{Name: "recipient", Type: "string", Required: true, Description: "Recipient user or space"}, {Name: "message", Type: "string", Required: true, Description: "Message content"}},
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
		if name == "calendar.deleteEvent" {
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
