package intent

import "strings"

// toolNameAliases maps flat tool names to contract-compliant names
// Format: <domain>.<action>
var toolNameAliases = map[string]string{
	// Gmail tools
	"list_emails":  "gmail.listEmails",
	"get_email":    "gmail.getEmail",
	"send_email":   "gmail.sendEmail",
	"search_email": "gmail.searchEmail",
	
	// Calendar tools
	"get_calendar":    "calendar.listEvents",
	"list_events":     "calendar.listEvents",
	"create_event":    "calendar.createEvent",
	"update_event":    "calendar.updateEvent",
	"delete_event":    "calendar.deleteEvent",
	"check_calendar":  "calendar.listEvents",
	
	// Chat tools
	"list_messages": "chat.listMessages",
	"send_message":  "chat.sendMessage",
	
	// Sandbox tools
	"exec":        "sandbox.runShell",
	"run":         "sandbox.runShell",
	"run_python":  "sandbox.runPython",
	"run_shell":   "sandbox.runShell",
	
	// File operations (could be system.* or sandbox.*)
	"read_file":      "system.readFile",
	"write_file":     "system.writeFile",
	"delete_file":    "system.deleteFile",
	"list_directory": "system.listDirectory",
	
	// Web tools
	"web_search": "web.search",
	"search":     "web.search",
	
	// Package management
	"install_package": "system.installPackage",
	
	// Service management
	"restart_service":       "system.restartService",
	"check_service_status":  "system.checkServiceStatus",
}

// NormalizeToolName converts tool names to contract-compliant format
// Examples:
//   - send_email -> gmail.sendEmail
//   - gmail.sendEmail -> gmail.sendEmail (already compliant)
//   - exec -> sandbox.runShell
func NormalizeToolName(name string) string {
	// Already in domain.action format
	if strings.Contains(name, ".") {
		return name
	}
	
	// Check alias map
	if normalized, ok := toolNameAliases[name]; ok {
		return normalized
	}
	
	// If no alias found, return as-is
	// (caller should handle unknown tools)
	return name
}

// IsContractCompliant checks if a tool name follows the <domain>.<action> format
func IsContractCompliant(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) != 2 {
		return false
	}
	
	// Both domain and action must be non-empty
	return parts[0] != "" && parts[1] != ""
}
