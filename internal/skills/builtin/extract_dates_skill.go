package builtin

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"vclaw/internal/skills"
	"vclaw/internal/tools"
)

// ExtractDatesSkill trich xuat tat ca ngay/gio tu van ban.
type ExtractDatesSkill struct{}

func NewExtractDatesSkill() *ExtractDatesSkill { return &ExtractDatesSkill{} }

func (s *ExtractDatesSkill) Definition() skills.SkillDefinition {
	return skills.SkillDefinition{
		Name:        "skill.extract_dates",
		Description: "Trich xuat tat ca ngay, gio, thoi gian de cap trong van ban. Dung khi nguoi dung muon tim cac moc thoi gian trong email, tai lieu hoac doan van.",
		Scope:       []string{"email", "document", "text", "calendar"},
		Permissions: []string{},
		Fallback:    "Khong the trich xuat ngay gio tu noi dung nay.",
		RiskLevel:   tools.RiskLevelSafeCompute,
		Enabled:     true,
		Parameters: tools.ToolSchema{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{
					"type":        "string",
					"description": "Van ban can trich xuat ngay gio",
				},
			},
			"required":             []string{"content"},
			"additionalProperties": false,
		},
	}
}

var datePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b\d{1,2}[\/\-]\d{1,2}[\/\-]\d{2,4}\b`),
	regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`),
	regexp.MustCompile(`\b\d{1,2}:\d{2}(?:\s*(?:am|pm|AM|PM))?\b`),
	regexp.MustCompile(`(?i)\b\d{1,2}\s*h\b`),
	regexp.MustCompile(`(?i)\b(?:monday|tuesday|wednesday|thursday|friday|saturday|sunday)\b`),
	regexp.MustCompile(`(?i)\b(?:jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\s+\d{1,2}(?:,\s*\d{4})?\b`),
	regexp.MustCompile(`(?i)\bngay\s+\d{1,2}\b`),
	regexp.MustCompile(`(?i)\bthang\s+\d{1,2}\b`),
	regexp.MustCompile(`(?i)\b(?:hom nay|hom qua|ngay mai|tuan nay|tuan sau|thang nay|thang sau)\b`),
	regexp.MustCompile(`(?i)\b(?:thu\s*(?:hai|ba|tu|nam|sau|bay)|chu\s*nhat)\b`),
}

func (s *ExtractDatesSkill) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	content, ok := call.Arguments["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return tools.ToolResult{
			ToolCallID:     call.ID,
			ToolName:       call.Name,
			Success:        false,
			ContentForLLM:  "SKILL_ERROR: content is required",
			ContentForUser: "Vui long cung cap van ban can trich xuat.",
			Error:          &tools.ToolError{Code: "TOOL_INPUT_INVALID", Message: "content is required"},
		}
	}

	seen := make(map[string]bool)
	var found []string
	for _, re := range datePatterns {
		matches := re.FindAllString(content, -1)
		for _, m := range matches {
			normalized := strings.TrimSpace(m)
			if !seen[normalized] {
				seen[normalized] = true
				found = append(found, normalized)
			}
		}
	}

	if len(found) == 0 {
		msg := "Khong tim thay ngay/gio nao trong van ban."
		return tools.ToolResult{
			ToolCallID:     call.ID,
			ToolName:       call.Name,
			Success:        true,
			ContentForLLM:  msg,
			ContentForUser: msg,
		}
	}

	lines := make([]string, len(found))
	for i, f := range found {
		lines[i] = fmt.Sprintf("- %s", f)
	}
	result := fmt.Sprintf("Tim thay %d moc thoi gian:\n%s", len(found), strings.Join(lines, "\n"))
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  result,
		ContentForUser: result,
	}
}