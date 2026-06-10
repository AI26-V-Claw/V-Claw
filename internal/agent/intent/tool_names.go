package intent

import "strings"

// toolNameAliases maps legacy flat tool names to contract-compliant names.
// Contract tool names use <domain>.<action> as defined in docs/03-contracts.md.
var toolNameAliases = map[string]string{
	// Gmail tools
	"list_emails":           "gmail.listEmails",
	"get_email":             "gmail.getEmail",
	"send_email":            "gmail.createDraft",
	"search_email":          "gmail.listEmails",
	"list_labels":           "gmail.listLabels",
	"get_profile":           "gmail.getProfile",
	"list_threads":          "gmail.listThreads",
	"get_thread":            "gmail.getThread",
	"list_drafts":           "gmail.listDrafts",
	"get_draft":             "gmail.getDraft",
	"create_draft":          "gmail.createDraft",
	"update_draft":          "gmail.updateDraft",
	"send_draft":            "gmail.sendDraft",
	"delete_draft":          "gmail.deleteDraft",
	"reply_draft":           "gmail.replyDraft",
	"forward_draft":         "gmail.forwardDraft",
	"download_attachments":  "gmail.downloadAttachments",
	"modify_message":        "gmail.modifyMessage",
	"batch_modify_messages": "gmail.batchModifyMessages",
	"trash_message":         "gmail.trashMessage",
	"untrash_message":       "gmail.untrashMessage",

	// Calendar tools
	"get_calendar":   "calendar.listEvents",
	"list_events":    "calendar.listEvents",
	"create_event":   "calendar.createEvent",
	"update_event":   "calendar.updateEvent",
	"delete_event":   "calendar.deleteEvent",
	"check_calendar": "calendar.listEvents",

	// Chat tools
	"list_spaces":            "chat.listSpaces",
	"list_members":           "chat.listMembers",
	"find_spaces_by_members": "chat.findSpacesByMembers",
	"list_messages":          "chat.listMessages",
	"send_message":           "chat.sendMessage",
	"update_message":         "chat.updateMessage",
	"delete_message":         "chat.deleteMessage",
	"create_space":           "chat.createSpace",
	"add_member":             "chat.addMember",
	"remove_member":          "chat.removeMember",

	// People tools
	"search_directory": "people.searchDirectory",

	// Drive tools
	"drive_search":      "drive.searchFiles",
	"search_drive":      "drive.searchFiles",
	"get_drive_file":    "drive.getFileMetadata",
	"export_drive_file": "drive.exportFile",
	"download_drive":    "drive.downloadFile",
	"create_drive_file": "drive.createTextFile",
	"update_drive_file": "drive.updateTextFile",
	"rename_drive_file": "drive.renameFile",
	"rename_file":       "drive.renameFile",
	"share_drive_file":  "drive.shareFile",

	// Docs tools
	"get_doc":         "docs.getDocument",
	"read_doc":        "docs.getDocument",
	"create_doc":      "docs.createDocument",
	"append_doc_text": "docs.appendText",

	// Sheets tools
	"get_spreadsheet":    "sheets.getSpreadsheet",
	"list_sheets":        "sheets.listSheets",
	"read_sheet":         "sheets.readRange",
	"create_spreadsheet": "sheets.createSpreadsheet",
	"update_sheet":       "sheets.updateRange",
	"append_sheet_rows":  "sheets.appendRows",

	// Web tools
	"web_search": "web.search",
	"web_fetch":  "web.fetch",
	"search_web": "web.search",
	"fetch_url":  "web.fetch",

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
