package intent

import "testing"

func TestNormalizeToolName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Gmail
		{"send_email", "gmail.sendEmail"},
		{"list_emails", "gmail.listEmails"},
		{"gmail.sendEmail", "gmail.sendEmail"}, // already compliant

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
		{"send_message", "chat.sendMessage"},
		{"chat.sendMessage", "chat.sendMessage"}, // already compliant

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
		{"gmail.sendEmail", true},
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
		{".sendEmail", false},     // empty domain
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
