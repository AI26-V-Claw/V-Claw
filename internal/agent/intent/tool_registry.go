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

// Registry maps tool names to contract-compliant definitions from docs/03-contracts.md.
var Registry = map[string]ToolDefinition{
	"gmail.listEmails": {
		Name: "gmail.listEmails", Category: CategorySafeRead,
		Description: "List emails from Gmail",
		Dangerous:   false, RequiresConfirm: false, Timeout: 30,
		Parameters: []ParamDef{{Name: "query", Type: "string", Required: false, Description: "Email search query"}},
	},
	"gmail.getEmail": {
		Name: "gmail.getEmail", Category: CategorySafeRead,
		Description: "Get an email from Gmail",
		Dangerous:   false, RequiresConfirm: false, Timeout: 30,
		Parameters: []ParamDef{{Name: "id", Type: "string", Required: false, Description: "Email ID"}},
	},
	"calendar.listEvents": {
		Name: "calendar.listEvents", Category: CategorySafeRead,
		Description: "List events from Google Calendar",
		Dangerous:   false, RequiresConfirm: false, Timeout: 30,
		Parameters: []ParamDef{{Name: "date", Type: "string", Required: false, Description: "Date to query (ISO-8601)"}},
	},
	"chat.listMessages": {
		Name: "chat.listMessages", Category: CategorySafeRead,
		Description: "List Google Chat messages",
		Dangerous:   false, RequiresConfirm: false, Timeout: 30,
		Parameters: []ParamDef{{Name: "space", Type: "string", Required: false, Description: "Chat space"}},
	},
	"calendar.createEvent": {
		Name: "calendar.createEvent", Category: CategoryDangerousWrite,
		Description: "Create a new calendar event",
		Dangerous:   true, RequiresConfirm: true, Timeout: 60,
		Parameters: []ParamDef{{Name: "title", Type: "string", Required: true, Description: "Event title"}, {Name: "start", Type: "string", Required: true, Description: "Start time (ISO-8601)"}},
	},
	"calendar.updateEvent": {
		Name: "calendar.updateEvent", Category: CategoryDangerousWrite,
		Description: "Update a calendar event",
		Dangerous:   true, RequiresConfirm: true, Timeout: 60,
		Parameters: []ParamDef{{Name: "eventId", Type: "string", Required: true, Description: "Event ID"}},
	},
	"calendar.deleteEvent": {
		Name: "calendar.deleteEvent", Category: CategoryDangerousWrite,
		Description: "Delete a calendar event",
		Dangerous:   true, RequiresConfirm: true, Timeout: 60,
		Parameters: []ParamDef{{Name: "eventId", Type: "string", Required: true, Description: "Event ID"}},
	},
	"sandbox.runPython": {
		Name: "sandbox.runPython", Category: CategoryExecution,
		Description: "Run Python code in sandbox",
		Dangerous:   true, RequiresConfirm: true, Timeout: 120,
		Parameters: []ParamDef{{Name: "code", Type: "string", Required: true, Description: "Python code to run"}},
	},
	"sandbox.runShell": {
		Name: "sandbox.runShell", Category: CategoryExecution,
		Description: "Run shell command in sandbox",
		Dangerous:   true, RequiresConfirm: true, Timeout: 120,
		Parameters: []ParamDef{{Name: "command", Type: "string", Required: true, Description: "Shell command to run"}, {Name: "cwd", Type: "path", Required: false, Description: "Working directory"}},
	},
	"gmail.sendEmail": {
		Name: "gmail.sendEmail", Category: CategoryCommunication,
		Description: "Send an email via Gmail",
		Dangerous:   true, RequiresConfirm: true, Timeout: 60,
		Parameters: []ParamDef{{Name: "to", Type: "email", Required: true, Description: "Recipient email address"}, {Name: "subject", Type: "string", Required: true, Description: "Email subject"}, {Name: "body", Type: "string", Required: true, Description: "Email body"}},
	},
	"chat.sendMessage": {
		Name: "chat.sendMessage", Category: CategoryCommunication,
		Description: "Send a chat message",
		Dangerous:   true, RequiresConfirm: true, Timeout: 30,
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
