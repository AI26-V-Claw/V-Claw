package intent

import "testing"

func TestNormalizeToolName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Gmail
		{"send_email", "gmail.createDraft"},
		{"list_emails", "gmail.listEmails"},
		{"list_labels", "gmail.listLabels"},
		{"get_profile", "gmail.getProfile"},
		{"list_threads", "gmail.listThreads"},
		{"get_thread", "gmail.getThread"},
		{"list_drafts", "gmail.listDrafts"},
		{"get_draft", "gmail.getDraft"},
		{"create_draft", "gmail.createDraft"},
		{"update_draft", "gmail.updateDraft"},
		{"send_draft", "gmail.sendDraft"},
		{"delete_draft", "gmail.deleteDraft"},
		{"reply_draft", "gmail.replyDraft"},
		{"forward_draft", "gmail.forwardDraft"},
		{"download_attachments", "gmail.downloadAttachments"},
		{"modify_message", "gmail.modifyMessage"},
		{"batch_modify_messages", "gmail.batchModifyMessages"},
		{"trash_message", "gmail.trashMessage"},
		{"untrash_message", "gmail.untrashMessage"},
		{"gmail.createDraft", "gmail.createDraft"}, // already compliant

		// Calendar
		{"create_event", "calendar.createEvent"},
		{"calendar.listEvents", "calendar.listEvents"}, // already compliant

		// Sandbox
		{"exec", "sandbox.runShell"},
		{"run_python", "sandbox.runPython"},
		{"sandbox.runPython", "sandbox.runPython"}, // already compliant

		// System
		{"read_file", "sandbox.runShell"},
		{"delete_file", "sandbox.runShell"},

		// Chat
		{"list_spaces", "chat.listSpaces"},
		{"list_members", "chat.listMembers"},
		{"find_spaces_by_members", "chat.findSpacesByMembers"},
		{"send_message", "chat.sendMessage"},
		{"update_message", "chat.updateMessage"},
		{"delete_message", "chat.deleteMessage"},
		{"create_space", "chat.createSpace"},
		{"add_member", "chat.addMember"},
		{"remove_member", "chat.removeMember"},
		{"chat.sendMessage", "chat.sendMessage"}, // already compliant

		// People
		{"search_directory", "people.searchDirectory"},

		// Drive/Docs/Sheets
		{"list_drive_files", "drive.listFiles"},
		{"export_drive_file", "drive.exportFile"},
		{"share_drive_file", "drive.shareFile"},
		{"revoke_drive_permission", "drive.revokePermission"},
		{"move_drive_file", "drive.moveFile"},
		{"trash_drive_file", "drive.trashFile"},
		{"untrash_drive_file", "drive.untrashFile"},
		{"get_document", "docs.getDocument"},
		{"append_document_text", "docs.appendText"},
		{"replace_document_text", "docs.replaceText"},
		{"read_sheet_values", "sheets.readValues"},
		{"batch_get_sheet_values", "sheets.batchGetValues"},
		{"append_sheet_values", "sheets.appendValues"},
		{"delete_sheet", "sheets.deleteSheet"},

		// Unknown (no alias)
		{"unknown_tool", "unknown_tool"},
		{"custom.tool", "custom.tool"}, // already in domain.action format
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeToolName(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeToolName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsContractCompliant(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		// Compliant
		{"gmail.createDraft", true},
		{"calendar.createEvent", true},
		{"sandbox.runPython", true},
		{"gmail.getEmail", true},
		{"chat.sendMessage", true},
		{"custom.action", true},

		// Non-compliant
		{"send_email", false},     // no domain
		{"exec", false},           // no domain
		{"read_file", false},      // no domain
		{"gmail.", false},         // empty action
		{".createDraft", false},   // empty domain
		{"", false},               // empty
		{"tool.with.dots", false}, // too many dots
		{"nodot", false},          // no dot
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsContractCompliant(tt.name)
			if result != tt.expected {
				t.Errorf("IsContractCompliant(%q) = %v, want %v", tt.name, result, tt.expected)
			}
		})
	}
}

// TestContractDrift_AllToolsNormalized ensures all tools in Registry
// can be normalized to contract-compliant format
func TestContractDrift_AllToolsNormalized(t *testing.T) {
	for toolName := range Registry {
		t.Run(toolName, func(t *testing.T) {
			normalized := NormalizeToolName(toolName)

			// After normalization, must be contract-compliant
			if !IsContractCompliant(normalized) {
				t.Errorf("Tool %q normalizes to %q which is not contract-compliant (<domain>.<action>)",
					toolName, normalized)
			}
		})
	}
}

func TestDriveDocsSheetsRiskMetadata(t *testing.T) {
	tests := []struct {
		name             string
		dangerous        bool
		requiresApproval bool
	}{
		{"drive.listFiles", false, false},
		{"drive.getFile", false, false},
		{"drive.exportFile", false, false},
		{"drive.downloadFile", false, false},
		{"drive.listPermissions", false, false},
		{"docs.getDocument", false, false},
		{"sheets.getSpreadsheet", false, false},
		{"sheets.readValues", false, false},
		{"sheets.batchGetValues", false, false},
		{"drive.createFolder", true, true},
		{"drive.createFile", true, true},
		{"drive.uploadFile", true, true},
		{"drive.updateFileMetadata", true, true},
		{"drive.shareFile", true, true},
		{"drive.revokePermission", true, true},
		{"drive.moveFile", true, true},
		{"drive.trashFile", true, true},
		{"drive.untrashFile", true, true},
		{"docs.createDocument", true, true},
		{"docs.appendText", true, true},
		{"docs.replaceText", true, true},
		{"docs.insertText", true, true},
		{"docs.deleteContent", true, true},
		{"sheets.createSpreadsheet", true, true},
		{"sheets.updateValues", true, true},
		{"sheets.batchUpdateValues", true, true},
		{"sheets.appendValues", true, true},
		{"sheets.clearValues", true, true},
		{"sheets.addSheet", true, true},
		{"sheets.renameSheet", true, true},
		{"sheets.deleteSheet", true, true},
		{"sheets.duplicateSheet", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, err := LookupTool(tt.name)
			if err != nil {
				t.Fatalf("LookupTool(%q): %v", tt.name, err)
			}
			if tool.Dangerous != tt.dangerous {
				t.Fatalf("Dangerous = %t, want %t", tool.Dangerous, tt.dangerous)
			}
			if tool.RequiresApproval != tt.requiresApproval {
				t.Fatalf("RequiresApproval = %t, want %t", tool.RequiresApproval, tt.requiresApproval)
			}
		})
	}
}
