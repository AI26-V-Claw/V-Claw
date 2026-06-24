package builtin

import (
	"context"
	"fmt"
	"strings"

	"vclaw/internal/skills"
	"vclaw/internal/tools"
)

// FormatEmailSkill format nội dung thô thành email chuẩn có subject, greeting, body, closing.
type FormatEmailSkill struct{}

func NewFormatEmailSkill() *FormatEmailSkill { return &FormatEmailSkill{} }

func (s *FormatEmailSkill) Definition() skills.SkillDefinition {
	return skills.SkillDefinition{
		Name:        "skill.format_email",
		Description: "Format nội dung thô thành email chuẩn với tiêu đề, lời chào, nội dung chính và lời kết. Dùng khi người dùng muốn soạn email chuyên nghiệp từ ý tưởng thô.",
		Scope:       []string{"email"},
		Permissions: []string{},
		Fallback:    "Không thể format email. Vui lòng cung cấp nội dung cụ thể hơn.",
		RiskLevel:   tools.RiskLevelSafeCompute,
		Enabled:     true,
		Parameters: tools.ToolSchema{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{
					"type":        "string",
					"description": "Nội dung thô cần format thành email",
				},
				"recipient": map[string]any{
					"type":        "string",
					"description": "Tên người nhận (e.g. 'anh Nam', 'team')",
				},
				"sender": map[string]any{
					"type":        "string",
					"description": "Tên người gửi để ký tên cuối email",
				},
				"tone": map[string]any{
					"type":        "string",
					"enum":        []string{"formal", "friendly"},
					"description": "Giọng văn: formal (trang trọng) hoặc friendly (thân thiện). Mặc định: formal.",
				},
			},
			"required":             []string{"content"},
			"additionalProperties": false,
		},
	}
}

func (s *FormatEmailSkill) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	content, ok := call.Arguments["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return tools.ToolResult{
			ToolCallID:     call.ID,
			ToolName:       call.Name,
			Success:        false,
			ContentForLLM:  "SKILL_ERROR: content is required",
			ContentForUser: "Vui lòng cung cấp nội dung email.",
			Error:          &tools.ToolError{Code: "TOOL_INPUT_INVALID", Message: "content is required"},
		}
	}
	recipient := strings.TrimSpace(stringArg(call.Arguments, "recipient"))
	sender := strings.TrimSpace(stringArg(call.Arguments, "sender"))
	tone := strings.TrimSpace(stringArg(call.Arguments, "tone"))
	if tone == "" {
		tone = "formal"
	}

	greeting := buildGreeting(recipient, tone)
	closing := buildClosing(sender, tone)
	body := strings.TrimSpace(content)

	result := fmt.Sprintf("%s\n\n%s\n\n%s", greeting, body, closing)
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "[Email đã format]:\n" + result,
		ContentForUser: result,
	}
}

func buildGreeting(recipient, tone string) string {
	if recipient == "" {
		if tone == "friendly" {
			return "Chào bạn,"
		}
		return "Kính gửi,"
	}
	if tone == "friendly" {
		return fmt.Sprintf("Chào %s,", recipient)
	}
	return fmt.Sprintf("Kính gửi %s,", recipient)
}

func buildClosing(sender, tone string) string {
	var lines []string
	if tone == "friendly" {
		lines = append(lines, "Cảm ơn bạn!")
		lines = append(lines, "Trân trọng,")
	} else {
		lines = append(lines, "Trân trọng,")
	}
	if sender != "" {
		lines = append(lines, sender)
	}
	return strings.Join(lines, "\n")
}

func stringArg(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
