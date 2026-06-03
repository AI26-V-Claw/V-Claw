package intent

import "strings"

// toolNameAliases maps legacy flat tool names to contract-compliant names.
// Contract tool names use <domain>.<action> as defined in docs/03-contracts.md.
var toolNameAliases = map[string]string{
	// Gmail tools
	"list_emails":  "gmail.listEmails",
	"get_email":    "gmail.getEmail",
	"send_email":   "gmail.createDraft",
	"search_email": "gmail.listEmails",

	// Calendar tools
	"get_calendar":   "calendar.listEvents",
	"list_events":    "calendar.listEvents",
	"create_event":   "calendar.createEvent",
	"update_event":   "calendar.updateEvent",
	"delete_event":   "calendar.deleteEvent",
	"check_calendar": "calendar.listEvents",

	// Chat tools
	"list_messages": "chat.listMessages",
	"send_message":  "chat.sendMessage",

	// Local/system operations must cross the sandbox contract boundary.
	"exec":                 "sandbox.runShell",
	"run":                  "sandbox.runShell",
	"run_python":           "sandbox.runPython",
	"run_shell":            "sandbox.runShell",
	"read_file":            "sandbox.runShell",
	"write_file":           "sandbox.runShell",
	"delete_file":          "sandbox.runShell",
	"list_directory":       "sandbox.runShell",
	"install_package":      "sandbox.runShell",
	"restart_service":      "sandbox.runShell",
	"check_service_status": "sandbox.runShell",
}

// NormalizeToolName converts tool names to contract-compliant format.
func NormalizeToolName(name string) string {
	if strings.Contains(name, ".") {
		return name
	}
	if normalized, ok := toolNameAliases[name]; ok {
		return normalized
	}
	return name
}

// IsContractCompliant checks if a tool name follows the <domain>.<action> format.
func IsContractCompliant(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) != 2 {
		return false
	}
	return parts[0] != "" && parts[1] != ""
}
