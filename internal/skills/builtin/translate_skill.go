package builtin

import (
	"context"
	"fmt"
	"strings"

	"vclaw/internal/skills"
	"vclaw/internal/tools"
)

// TranslateSkill dich van ban sang ngon ngu khac.
type TranslateSkill struct{}

func NewTranslateSkill() *TranslateSkill { return &TranslateSkill{} }

func (s *TranslateSkill) Definition() skills.SkillDefinition {
	return skills.SkillDefinition{
		Name:        "skill.translate",
		Description: "Dich van ban sang ngon ngu khac. Dung khi nguoi dung yeu cau dich mot doan van, cau, hoac tu sang tieng Anh, tieng Viet, tieng Nhat, tieng Han, hoac bat ky ngon ngu nao khac.",
		Scope:       []string{"text", "document", "email"},
		Permissions: []string{},
		Fallback:    "Khong the dich noi dung nay. Vui long cung cap van ban cu the hon.",
		RiskLevel:   tools.RiskLevelSafeCompute,
		Enabled:     true,
		Parameters: tools.ToolSchema{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "Van ban can dich",
				},
				"target_language": map[string]any{
					"type":        "string",
					"description": "Ngon ngu dich sang, vi du: English, Vietnamese, Japanese, Korean, French",
				},
				"source_language": map[string]any{
					"type":        "string",
					"description": "Ngon ngu goc (tuy chon, mac dinh: tu dong nhan dien)",
				},
			},
			"required":             []string{"text", "target_language"},
			"additionalProperties": false,
		},
	}
}

func (s *TranslateSkill) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	text, ok := call.Arguments["text"].(string)
	if !ok || strings.TrimSpace(text) == "" {
		return tools.ToolResult{
			ToolCallID:    call.ID,
			ToolName:      call.Name,
			Success:       false,
			ContentForLLM: "SKILL_ERROR: text is required",
			ContentForUser: "Vui long cung cap van ban can dich.",
			Error:          &tools.ToolError{Code: "TOOL_INPUT_INVALID", Message: "text is required"},
		}
	}

	targetLang, ok := call.Arguments["target_language"].(string)
	if !ok || strings.TrimSpace(targetLang) == "" {
		return tools.ToolResult{
			ToolCallID:    call.ID,
			ToolName:      call.Name,
			Success:       false,
			ContentForLLM: "SKILL_ERROR: target_language is required",
			ContentForUser: "Vui long cho biet ban muon dich sang ngon ngu nao.",
			Error:          &tools.ToolError{Code: "TOOL_INPUT_INVALID", Message: "target_language is required"},
		}
	}

	sourceLang := stringArg(call.Arguments, "source_language")

	// Xay dung instruction cho LLM de dich
	// TranslateSkill tra ve prompt de agent/LLM tu xu ly dich thuat
	// Day la pattern "skill as prompt builder" — skill chuan bi context, LLM thuc hien
	var instruction string
	if strings.TrimSpace(sourceLang) != "" {
		instruction = fmt.Sprintf(
			"[TRANSLATE from %s to %s]\n%s",
			sourceLang, targetLang, strings.TrimSpace(text),
		)
	} else {
		instruction = fmt.Sprintf(
			"[TRANSLATE to %s]\n%s",
			targetLang, strings.TrimSpace(text),
		)
	}

	result := fmt.Sprintf("Dich sang %s:\n%s", targetLang, strings.TrimSpace(text))
	return tools.ToolResult{
		ToolCallID:    call.ID,
		ToolName:      call.Name,
		Success:       true,
		ContentForLLM: instruction,
		ContentForUser: result,
	}
}
