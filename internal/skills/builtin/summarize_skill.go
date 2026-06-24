package builtin

import (
	"context"
	"fmt"
	"strings"

	"vclaw/internal/skills"
	"vclaw/internal/tools"
)

// SummarizeSkill là skill mẫu: tóm tắt nội dung văn bản được truyền vào.
// Đây là skill builtin có logic Execute thật (không dùng fallback).
type SummarizeSkill struct{}

func NewSummarizeSkill() *SummarizeSkill {
	return &SummarizeSkill{}
}

func (s *SummarizeSkill) Definition() skills.SkillDefinition {
	return skills.SkillDefinition{
		Name:        "skill.summarize",
		Description: "Tóm tắt nội dung văn bản. Dùng khi người dùng yêu cầu tóm tắt email, tài liệu, hoặc đoạn văn bất kỳ.",
		Scope:       []string{"email", "document", "text"},
		Permissions: []string{},
		Fallback:    "Không thể tóm tắt nội dung này. Vui lòng cung cấp văn bản cụ thể hơn.",
		RiskLevel:   tools.RiskLevelSafeCompute,
		Enabled:     true,
		Parameters: tools.ToolSchema{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{
					"type":        "string",
					"description": "Nội dung cần tóm tắt",
				},
				"max_sentences": map[string]any{
					"type":        "integer",
					"description": "Số câu tối đa trong bản tóm tắt (mặc định: 3)",
					"default":     3,
				},
			},
			"required":             []string{"content"},
			"additionalProperties": false,
		},
	}
}

func (s *SummarizeSkill) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	content, ok := call.Arguments["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return tools.ToolResult{
			ToolCallID:     call.ID,
			ToolName:       call.Name,
			Success:        false,
			ContentForLLM:  "SKILL_ERROR: content is required and must be a non-empty string",
			ContentForUser: "Vui lòng cung cấp nội dung cần tóm tắt.",
			Error: &tools.ToolError{
				Code:    "TOOL_INPUT_INVALID",
				Message: "content is required",
			},
		}
	}
	maxSentences := 3
	if v, ok := call.Arguments["max_sentences"]; ok {
		switch n := v.(type) {
		case float64:
			maxSentences = int(n)
		case int:
			maxSentences = n
		}
	}
	if maxSentences < 1 {
		maxSentences = 1
	}
	if maxSentences > 10 {
		maxSentences = 10
	}

	// Logic tóm tắt đơn giản: lấy N câu đầu tiên
	sentences := splitSentences(content)
	if len(sentences) > maxSentences {
		sentences = sentences[:maxSentences]
	}
	summary := strings.Join(sentences, " ")
	result := fmt.Sprintf("[Tóm tắt — %d câu]: %s", len(sentences), summary)

	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  result,
		ContentForUser: result,
	}
}

// splitSentences tách văn bản thành các câu theo dấu chấm, chấm hỏi, chấm than.
func splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder
	for _, r := range text {
		current.WriteRune(r)
		if r == '.' || r == '?' || r == '!' || r == '。' {
			s := strings.TrimSpace(current.String())
			if s != "" {
				sentences = append(sentences, s)
			}
			current.Reset()
		}
	}
	if tail := strings.TrimSpace(current.String()); tail != "" {
		sentences = append(sentences, tail)
	}
	return sentences
}
