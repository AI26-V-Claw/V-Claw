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

	// Drive/Docs/Sheets tools
	"list_drive_files":           "drive.listFiles",
	"search_drive":               "drive.listFiles",
	"get_drive_file":             "drive.getFile",
	"export_drive_file":          "drive.exportFile",
	"download_drive_file":        "drive.downloadFile",
	"create_drive_folder":        "drive.createFolder",
	"create_drive_file":          "drive.createFile",
	"upload_drive_file":          "drive.uploadFile",
	"update_drive_file_metadata": "drive.updateFileMetadata",
	"share_drive_file":           "drive.shareFile",
	"list_drive_permissions":     "drive.listPermissions",
	"revoke_drive_permission":    "drive.revokePermission",
	"move_drive_file":            "drive.moveFile",
	"trash_drive_file":           "drive.trashFile",
	"untrash_drive_file":         "drive.untrashFile",
	"get_document":               "docs.getDocument",
	"create_document":            "docs.createDocument",
	"append_document_text":       "docs.appendText",
	"replace_document_text":      "docs.replaceText",
	"insert_document_text":       "docs.insertText",
	"delete_document_content":    "docs.deleteContent",
	"get_spreadsheet":            "sheets.getSpreadsheet",
	"read_sheet_values":          "sheets.readValues",
	"batch_get_sheet_values":     "sheets.batchGetValues",
	"create_spreadsheet":         "sheets.createSpreadsheet",
	"update_sheet_values":        "sheets.updateValues",
	"batch_update_sheet_values":  "sheets.batchUpdateValues",
	"append_sheet_values":        "sheets.appendValues",
	"clear_sheet_values":         "sheets.clearValues",
	"add_sheet":                  "sheets.addSheet",
	"rename_sheet":               "sheets.renameSheet",
	"delete_sheet":               "sheets.deleteSheet",
	"duplicate_sheet":            "sheets.duplicateSheet",

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
