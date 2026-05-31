package agent

import (
	"fmt"
	"regexp"
)

// ToolRegistry holds all registered tools
var ToolRegistry = map[string]ToolDefinition{
	"read_file": {
		Name:            "read_file",
		Category:        ToolCategorySafeRead,
		Description:     "Read content from a file",
		Dangerous:       false,
		RequiresConfirm: false,
		Timeout:         30,
		Parameters: []ParameterDef{
			{Name: "path", Type: "path", Required: true, Description: "File path to read"},
		},
	},
	"list_directory": {
		Name:            "list_directory",
		Category:        ToolCategorySafeRead,
		Description:     "List files in a directory",
		Dangerous:       false,
		RequiresConfirm: false,
		Timeout:         30,
		Parameters: []ParameterDef{
			{Name: "path", Type: "path", Required: true, Description: "Directory path to list"},
		},
	},
	"web_search": {
		Name:            "web_search",
		Category:        ToolCategorySafeRead,
		Description:     "Search the web for information",
		Dangerous:       false,
		RequiresConfirm: false,
		Timeout:         45,
		Parameters: []ParameterDef{
			{Name: "query", Type: "string", Required: true, Description: "Search query"},
		},
	},
	"delete_file": {
		Name:            "delete_file",
		Category:        ToolCategoryDangerousWrite,
		Description:     "Delete a file from the filesystem",
		Dangerous:       true,
		RequiresConfirm: true,
		Timeout:         60,
		Parameters: []ParameterDef{
			{Name: "path", Type: "path", Required: true, Description: "File path to delete"},
			{Name: "confirm", Type: "bool", Required: true, Description: "Confirmation flag"},
		},
	},
	"write_file": {
		Name:            "write_file",
		Category:        ToolCategoryDangerousWrite,
		Description:     "Write content to a file",
		Dangerous:       true,
		RequiresConfirm: true,
		Timeout:         60,
		Parameters: []ParameterDef{
			{Name: "path", Type: "path", Required: true, Description: "File path to write"},
			{Name: "content", Type: "string", Required: true, Description: "Content to write"},
		},
	},
	"exec": {
		Name:            "exec",
		Category:        ToolCategoryExecution,
		Description:     "Execute a shell command",
		Dangerous:       true,
		RequiresConfirm: true,
		Timeout:         120,
		Parameters: []ParameterDef{
			{Name: "command", Type: "string", Required: true, Description: "Command to execute"},
			{Name: "cwd", Type: "path", Required: false, Description: "Working directory"},
		},
	},
	"send_email": {
		Name:            "send_email",
		Category:        ToolCategoryCommunication,
		Description:     "Send an email",
		Dangerous:       true,
		RequiresConfirm: true,
		Timeout:         60,
		Parameters: []ParameterDef{
			{Name: "to", Type: "email", Required: true, Description: "Recipient email address"},
			{Name: "subject", Type: "string", Required: true, Description: "Email subject"},
			{Name: "body", Type: "string", Required: true, Description: "Email body"},
		},
	},
}

// GetTool retrieves a tool definition by name
func GetTool(name string) (ToolDefinition, error) {
	tool, exists := ToolRegistry[name]
	if !exists {
		return ToolDefinition{}, fmt.Errorf("tool %q not found in registry", name)
	}
	return tool, nil
}

// GetToolsByCategory returns all tools in a specific category
func GetToolsByCategory(category ToolCategory) []ToolDefinition {
	var tools []ToolDefinition
	for _, tool := range ToolRegistry {
		if tool.Category == category {
			tools = append(tools, tool)
		}
	}
	return tools
}

// IsDangerousTool checks if a tool is marked as dangerous
func IsDangerousTool(name string) bool {
	tool, err := GetTool(name)
	if err != nil {
		return false
	}
	return tool.Dangerous
}

// ValidateEmail validates an email address format
func ValidateEmail(email interface{}) error {
	emailStr, ok := email.(string)
	if !ok {
		return fmt.Errorf("email must be a string")
	}

	// Simple email validation regex
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(emailStr) {
		return fmt.Errorf("invalid email format: %s", emailStr)
	}
	return nil
}

// ValidatePath validates a file path (basic check)
func ValidatePath(path interface{}) error {
	pathStr, ok := path.(string)
	if !ok {
		return fmt.Errorf("path must be a string")
	}

	if pathStr == "" {
		return fmt.Errorf("path cannot be empty")
	}

	// Check for dangerous patterns
	dangerousPatterns := []string{
		"..",      // Directory traversal
		"~",       // Home directory expansion (should be explicit)
		"$",       // Variable expansion
		"|",       // Pipe
		";",       // Command separator
		"&",       // Background execution
		">",       // Redirection
		"<",       // Redirection
	}

	for _, pattern := range dangerousPatterns {
		if regexp.MustCompile(regexp.QuoteMeta(pattern)).MatchString(pathStr) {
			return fmt.Errorf("path contains dangerous pattern: %s", pattern)
		}
	}

	return nil
}
