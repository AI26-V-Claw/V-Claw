package risk

import (
	"fmt"

	"vclaw/internal/agent/intent"
)

// Classifier determines the risk level and approval requirements for tool calls.
// This implements the safety layer described in docs/03-contracts.md.
type Classifier struct {
	// Policy maps tool names to their default risk levels
	policy map[string]Level
}

// NewClassifier creates a new risk classifier with default policies.
func NewClassifier() *Classifier {
	return &Classifier{
		policy: buildDefaultPolicy(),
	}
}

// Assess evaluates the risk of a tool call and returns an Assessment.
// This is the main entry point for the safety layer.
func (c *Classifier) Assess(toolName string, intentType intent.IntentType) (*Assessment, error) {
	// Look up the tool's risk level
	riskLevel, ok := c.policy[toolName]
	if !ok {
		// Unknown tool → block by default
		return &Assessment{
			ToolName:         toolName,
			RiskLevel:        Blocked,
			Decision:         Block,
			RequiresApproval: false,
			ReasonVi:         fmt.Sprintf("Tool %q không được đăng ký trong hệ thống. Hành động bị chặn vì lý do an toàn.", toolName),
		}, nil
	}

	// Determine decision based on risk level
	decision, requiresApproval := c.decideAction(riskLevel, intentType)

	// Generate Vietnamese explanation
	reasonVi := c.generateReason(toolName, riskLevel, decision)

	return &Assessment{
		ToolName:         toolName,
		RiskLevel:        riskLevel,
		Decision:         decision,
		RequiresApproval: requiresApproval,
		ReasonVi:         reasonVi,
	}, nil
}

// decideAction determines whether to allow, require approval, or block an action.
func (c *Classifier) decideAction(riskLevel Level, intentType intent.IntentType) (Decision, bool) {
	switch riskLevel {
	case SafeRead, SafeCompute:
		// Safe operations → allow immediately
		return Allow, false

	case SensitiveRead:
		// Sensitive reads (e.g., credentials, private data) → require approval
		return RequiresApproval, true

	case ExternalWrite, LocalWrite:
		// Write operations → require approval
		return RequiresApproval, true

	case CodeExecution:
		// Code execution → always require approval
		return RequiresApproval, true

	case Destructive:
		// Destructive operations (delete, drop) → require approval
		return RequiresApproval, true

	case Blocked:
		// Explicitly blocked → deny
		return Block, false

	default:
		// Unknown risk level → block by default
		return Block, false
	}
}

// generateReason creates a Vietnamese explanation for the risk decision.
func (c *Classifier) generateReason(toolName string, riskLevel Level, decision Decision) string {
	switch decision {
	case Allow:
		return fmt.Sprintf("Tool %q được phép thực thi ngay lập tức vì thuộc nhóm an toàn (%s).", toolName, riskLevel)

	case RequiresApproval:
		switch riskLevel {
		case ExternalWrite:
			return fmt.Sprintf("Tool %q cần xác nhận vì sẽ thay đổi dữ liệu bên ngoài (email, calendar, chat).", toolName)
		case LocalWrite:
			return fmt.Sprintf("Tool %q cần xác nhận vì sẽ ghi/sửa file trên hệ thống.", toolName)
		case CodeExecution:
			return fmt.Sprintf("Tool %q cần xác nhận vì sẽ chạy code/lệnh hệ thống.", toolName)
		case Destructive:
			return fmt.Sprintf("Tool %q cần xác nhận vì có thể xóa dữ liệu không thể khôi phục.", toolName)
		case SensitiveRead:
			return fmt.Sprintf("Tool %q cần xác nhận vì truy cập thông tin nhạy cảm.", toolName)
		default:
			return fmt.Sprintf("Tool %q cần xác nhận trước khi thực thi.", toolName)
		}

	case Block:
		return fmt.Sprintf("Tool %q bị chặn vì vi phạm chính sách an toàn.", toolName)

	default:
		return fmt.Sprintf("Không thể xác định quyết định cho tool %q.", toolName)
	}
}

// buildDefaultPolicy creates the default risk policy mapping.
// This aligns with the Tool Registry in docs/03-contracts.md.
func buildDefaultPolicy() map[string]Level {
	return map[string]Level{
		// ── Safe Read Tools ──────────────────────────────────────────
		"read_file":            SafeRead,
		"list_directory":       SafeRead,
		"web_search":           SafeRead,
		"gmail.listEmails":     SafeRead,
		"gmail.getEmail":       SafeRead,
		"calendar.listEvents":  SafeRead,
		"chat.listMessages":    SafeRead,

		// ── Sensitive Read Tools ─────────────────────────────────────
		// (Currently none, but could include reading credentials, etc.)

		// ── External Write Tools ─────────────────────────────────────
		"gmail.sendEmail":      ExternalWrite,
		"calendar.createEvent": ExternalWrite,
		"calendar.updateEvent": ExternalWrite,
		"chat.sendMessage":     ExternalWrite,
		"send_email":           ExternalWrite,

		// ── Local Write Tools ────────────────────────────────────────
		"write_file":           LocalWrite,
		"create_file":          LocalWrite,
		"modify_file":          LocalWrite,

		// ── Destructive Tools ────────────────────────────────────────
		"delete_file":          Destructive,
		"calendar.deleteEvent": Destructive,
		"drop_database":        Destructive,

		// ── Code Execution Tools ─────────────────────────────────────
		"exec":                 CodeExecution,
		"sandbox.runPython":    CodeExecution,
		"sandbox.runShell":     CodeExecution,
		"run_command":          CodeExecution,

		// ── Blocked Tools ────────────────────────────────────────────
		// (Tools that should never be executed)
		"format_disk":          Blocked,
		"shutdown_system":      Blocked,
	}
}

// UpdatePolicy allows runtime updates to the risk policy.
// This is useful for testing or dynamic policy adjustments.
func (c *Classifier) UpdatePolicy(toolName string, riskLevel Level) {
	c.policy[toolName] = riskLevel
}

// GetPolicy returns the current risk level for a tool.
func (c *Classifier) GetPolicy(toolName string) (Level, bool) {
	level, ok := c.policy[toolName]
	return level, ok
}
