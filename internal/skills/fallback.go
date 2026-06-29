package skills

import (
	"fmt"
	"strings"

	"vclaw/internal/tools"
)

// FallbackResult trả về ToolResult chuẩn khi một skill không thể xử lý request.
// Dùng fallback message từ SkillDefinition nếu có, ngược lại dùng default message.
func FallbackResult(call tools.ToolCall, def SkillDefinition) tools.ToolResult {
	msg := strings.TrimSpace(def.Fallback)
	if msg == "" {
		msg = fmt.Sprintf("Skill %q không thể xử lý yêu cầu này. Vui lòng thử lại hoặc dùng cách khác.", def.Name)
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  "SKILL_FALLBACK: " + msg,
		ContentForUser: msg,
		Error: &tools.ToolError{
			Code:    "SKILL_FALLBACK",
			Message: msg,
		},
	}
}

// ScopeAllowed kiểm tra xem domain có nằm trong Scope của skill không.
// Nếu Scope rỗng, skill được phép xử lý mọi domain.
func ScopeAllowed(def SkillDefinition, domain string) bool {
	if len(def.Scope) == 0 {
		return true
	}
	domain = strings.TrimSpace(strings.ToLower(domain))
	for _, s := range def.Scope {
		if strings.ToLower(strings.TrimSpace(s)) == domain {
			return true
		}
	}
	return false
}

// PermissionsDescription trả về chuỗi mô tả quyền của skill để inject vào prompt.
func PermissionsDescription(def SkillDefinition) string {
	if len(def.Permissions) == 0 {
		return ""
	}
	return fmt.Sprintf("Skill %q yêu cầu quyền: %s", def.Name, strings.Join(def.Permissions, ", "))
}
