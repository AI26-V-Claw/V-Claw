package risk

import (
	"fmt"

	"vclaw/internal/agent/intent"
)

// Classifier determines the risk level and approval requirements for tool calls.
// This implements the safety layer described in docs/03-contracts.md.
type Classifier struct {
	// Policy maps tool names to their default risk levels.
	policy map[string]Level
	// denied marks tools that must be blocked. Block is a decision, not a risk level.
	denied map[string]bool
}

// NewClassifier creates a new risk classifier with default policies.
func NewClassifier() *Classifier {
	return &Classifier{
		policy: buildDefaultPolicy(),
		denied: buildDeniedTools(),
	}
}

// Assess evaluates the risk of a tool call and returns an Assessment.
// This is the main entry point for the safety layer.
func (c *Classifier) Assess(toolName string, intentType intent.IntentType) (*Assessment, error) {
	riskLevel, ok := c.policy[toolName]
	if !ok {
		return &Assessment{
			ToolName:         toolName,
			RiskLevel:        Destructive,
			Decision:         Block,
			RequiresApproval: false,
			ReasonVi:         fmt.Sprintf("Tool %q không được đăng ký trong hệ thống. Hành động bị chặn vì lý do an toàn.", toolName),
		}, nil
	}

	decision, requiresApproval := c.decideAction(toolName, riskLevel, intentType)
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
func (c *Classifier) decideAction(toolName string, riskLevel Level, intentType intent.IntentType) (Decision, bool) {
	if c.denied[toolName] {
		return Block, false
	}

	switch riskLevel {
	case SafeRead, SafeCompute:
		return Allow, false
	case SensitiveRead, ExternalWrite, LocalWrite, CodeExecution, Destructive:
		return RequiresApproval, true
	default:
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
		"gmail.listEmails":          SafeRead,
		"gmail.getEmail":            SensitiveRead,
		"gmail.listLabels":          SafeRead,
		"gmail.getProfile":          SafeRead,
		"gmail.listThreads":         SafeRead,
		"gmail.getThread":           SafeRead,
		"gmail.listDrafts":          SafeRead,
		"gmail.getDraft":            SafeRead,
		"calendar.listEvents":       SafeRead,
		"chat.listSpaces":           SafeRead,
		"chat.listMembers":          SafeRead,
		"chat.findSpacesByMembers":  SafeRead,
		"chat.listMessages":         SafeRead,
		"people.searchDirectory":    SafeRead,
		"gmail.createDraft":         ExternalWrite,
		"gmail.updateDraft":         ExternalWrite,
		"gmail.sendDraft":           ExternalWrite,
		"gmail.deleteDraft":         Destructive,
		"gmail.replyDraft":          ExternalWrite,
		"gmail.forwardDraft":        ExternalWrite,
		"gmail.downloadAttachments": LocalWrite,
		"gmail.modifyMessage":       ExternalWrite,
		"gmail.batchModifyMessages": ExternalWrite,
		"gmail.trashMessage":        Destructive,
		"gmail.untrashMessage":      ExternalWrite,
		"calendar.createEvent":      ExternalWrite,
		"calendar.updateEvent":      ExternalWrite,
		"calendar.deleteEvent":      Destructive,
		"chat.sendMessage":          ExternalWrite,
		"chat.updateMessage":        ExternalWrite,
		"chat.deleteMessage":        Destructive,
		"chat.createSpace":          ExternalWrite,
		"chat.addMember":            ExternalWrite,
		"chat.removeMember":         Destructive,
		"sandbox.runPython":         CodeExecution,
		"sandbox.runShell":          CodeExecution,
		"format_disk":               Destructive,
		"shutdown_system":           Destructive,
	}
}

func buildDeniedTools() map[string]bool {
	return map[string]bool{
		"format_disk":     true,
		"shutdown_system": true,
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
