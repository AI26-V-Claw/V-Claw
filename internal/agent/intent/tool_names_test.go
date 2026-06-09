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
		{"drive_search", "drive.searchFiles"},
		{"download_drive", "drive.downloadFile"},
		{"create_drive_file", "drive.createTextFile"},
		{"share_drive_file", "drive.shareFile"},
		{"read_doc", "docs.getDocument"},
		{"create_doc", "docs.createDocument"},
		{"append_doc_text", "docs.appendText"},
		{"get_spreadsheet", "sheets.getSpreadsheet"},
		{"read_sheet", "sheets.readRange"},
		{"append_sheet_rows", "sheets.appendRows"},

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
