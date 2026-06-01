package intent

import (
	"fmt"
	"regexp"
)

// ToolCategory classifies tools by their risk level.
type ToolCategory string

const (
	CategorySafeRead       ToolCategory = "SAFE_READ"
	CategoryDangerousWrite ToolCategory = "DANGEROUS_WRITE"
	CategoryExecution      ToolCategory = "EXECUTION"
	CategoryCommunication  ToolCategory = "COMMUNICATION"
)

// ToolDefinition defines the schema and metadata for a registered tool.
type ToolDefinition struct {
	Name            string
	Category        ToolCategory
	Description     string
	Parameters      []ParamDef
	Dangerous       bool
	RequiresConfirm bool
	Timeout         int // seconds
}

// ParamDef defines a single parameter for a tool.
type ParamDef struct {
	Name        string
	Type        string // "string", "int", "bool", "path", "email"
	Required    bool
	Description string
}

// Registry maps tool names to their definitions.
// This aligns with the Tool Registry in docs/03-contracts.md.
var Registry = map[string]ToolDefinition{
	// ── Safe Read Tools ──────────────────────────────────────────
	"read_file": {
		Name: "read_file", Category: CategorySafeRead,
		Description: "Read content from a file",
		Dangerous: false, RequiresConfirm: false, Timeout: 30,
		Parameters: []ParamDef{
			{Name: "path", Type: "path", Required: true, Description: "File path to read"},
		},
	},
	"list_directory": {
		Name: "list_directory", Category: CategorySafeRead,
		Description: "List files in a directory",
		Dangerous: false, RequiresConfirm: false, Timeout: 30,
		Parameters: []ParamDef{
			{Name: "path", Type: "path", Required: true, Description: "Directory path to list"},
		},
	},
	"web_search": {
		Name: "web_search", Category: CategorySafeRead,
		Description: "Search the web for information",
		Dangerous: false, RequiresConfirm: false, Timeout: 45,
		Parameters: []ParamDef{
			{Name: "query", Type: "string", Required: true, Description: "Search query"},
		},
	},
	// Maps to gmail.listEmails / calendar.listEvents in contracts
	"gmail.listEmails": {
		Name: "gmail.listEmails", Category: CategorySafeRead,
		Description: "List emails from Gmail",
		Dangerous: false, RequiresConfirm: false, Timeout: 30,
		Parameters: []ParamDef{
			{Name: "query", Type: "string", Required: false, Description: "Email search query"},
		},
	},
	"calendar.listEvents": {
		Name: "calendar.listEvents", Category: CategorySafeRead,
		Description: "List events from Google Calendar",
		Dangerous: false, RequiresConfirm: false, Timeout: 30,
		Parameters: []ParamDef{
			{Name: "date", Type: "string", Required: false, Description: "Date to query (ISO-8601)"},
		},
	},

	// ── Dangerous Write Tools ────────────────────────────────────
	"delete_file": {
		Name: "delete_file", Category: CategoryDangerousWrite,
		Description: "Delete a file from the filesystem",
		Dangerous: true, RequiresConfirm: true, Timeout: 60,
		Parameters: []ParamDef{
			{Name: "path", Type: "path", Required: true, Description: "File path to delete"},
			{Name: "confirm", Type: "bool", Required: true, Description: "Confirmation flag"},
		},
	},
	"write_file": {
		Name: "write_file", Category: CategoryDangerousWrite,
		Description: "Write content to a file",
		Dangerous: true, RequiresConfirm: true, Timeout: 60,
		Parameters: []ParamDef{
			{Name: "path", Type: "path", Required: true, Description: "File path to write"},
			{Name: "content", Type: "string", Required: true, Description: "Content to write"},
		},
	},
	// Maps to calendar.createEvent in contracts
	"calendar.createEvent": {
		Name: "calendar.createEvent", Category: CategoryDangerousWrite,
		Description: "Create a new calendar event",
		Dangerous: true, RequiresConfirm: true, Timeout: 60,
		Parameters: []ParamDef{
			{Name: "title", Type: "string", Required: true, Description: "Event title"},
			{Name: "start", Type: "string", Required: true, Description: "Start time (ISO-8601)"},
		},
	},

	// ── Execution Tools ──────────────────────────────────────────
	"exec": {
		Name: "exec", Category: CategoryExecution,
		Description: "Execute a shell command",
		Dangerous: true, RequiresConfirm: true, Timeout: 120,
		Parameters: []ParamDef{
			{Name: "command", Type: "string", Required: true, Description: "Command to execute"},
			{Name: "cwd", Type: "path", Required: false, Description: "Working directory"},
		},
	},
	// Maps to sandbox.runPython / sandbox.runShell in contracts
	"sandbox.runPython": {
		Name: "sandbox.runPython", Category: CategoryExecution,
		Description: "Run Python code in sandbox",
		Dangerous: true, RequiresConfirm: true, Timeout: 120,
		Parameters: []ParamDef{
			{Name: "code", Type: "string", Required: true, Description: "Python code to run"},
		},
	},
	"sandbox.runShell": {
		Name: "sandbox.runShell", Category: CategoryExecution,
		Description: "Run shell command in sandbox",
		Dangerous: true, RequiresConfirm: true, Timeout: 120,
		Parameters: []ParamDef{
			{Name: "command", Type: "string", Required: true, Description: "Shell command to run"},
		},
	},

	// ── Communication Tools ──────────────────────────────────────
	"send_email": {
		Name: "send_email", Category: CategoryCommunication,
		Description: "Send an email",
		Dangerous: true, RequiresConfirm: true, Timeout: 60,
		Parameters: []ParamDef{
			{Name: "to", Type: "email", Required: true, Description: "Recipient email address"},
			{Name: "subject", Type: "string", Required: true, Description: "Email subject"},
			{Name: "body", Type: "string", Required: true, Description: "Email body"},
		},
	},
	// Maps to gmail.sendEmail in contracts
	"gmail.sendEmail": {
		Name: "gmail.sendEmail", Category: CategoryCommunication,
		Description: "Send an email via Gmail",
		Dangerous: true, RequiresConfirm: true, Timeout: 60,
		Parameters: []ParamDef{
			{Name: "to", Type: "email", Required: true, Description: "Recipient email address"},
			{Name: "subject", Type: "string", Required: true, Description: "Email subject"},
			{Name: "body", Type: "string", Required: true, Description: "Email body"},
		},
	},
	// Maps to chat.sendMessage in contracts
	"chat.sendMessage": {
		Name: "chat.sendMessage", Category: CategoryCommunication,
		Description: "Send a chat message",
		Dangerous: true, RequiresConfirm: true, Timeout: 30,
		Parameters: []ParamDef{
			{Name: "recipient", Type: "string", Required: true, Description: "Recipient user or space"},
			{Name: "message", Type: "string", Required: true, Description: "Message content"},
		},
	},
}

// LookupTool retrieves a tool definition by name.
func LookupTool(name string) (ToolDefinition, error) {
	tool, ok := Registry[name]
	if !ok {
		return ToolDefinition{}, fmt.Errorf("tool %q not found in registry", name)
	}
	return tool, nil
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
