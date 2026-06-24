package builtin

import (
	"context"
	"fmt"
	"strings"

	"vclaw/internal/skills"
	"vclaw/internal/tools"
)

// BulletPointsSkill chuyen doan van thanh danh sach bullet points.
type BulletPointsSkill struct{}

func NewBulletPointsSkill() *BulletPointsSkill { return &BulletPointsSkill{} }

func (s *BulletPointsSkill) Definition() skills.SkillDefinition {
	return skills.SkillDefinition{
		Name:        "skill.bullet_points",
		Description: "Chuyen doan van, danh sach y tuong hoac noi dung dai thanh cac bullet points ngan gon, ro rang. Dung khi nguoi dung muon tom tat y chinh hoac tao danh sach tu van ban.",
		Scope:       []string{"text", "document", "email"},
		Permissions: []string{},
		Fallback:    "Khong the chuyen noi dung nay thanh bullet points.",
		RiskLevel:   tools.RiskLevelSafeCompute,
		Enabled:     true,
		Parameters: tools.ToolSchema{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{
					"type":        "string",
					"description": "Noi dung can chuyen thanh bullet points",
				},
				"max_points": map[string]any{
					"type":        "integer",
					"description": "So bullet points toi da (mac dinh: 5, toi da: 10)",
					"default":     5,
				},
				"prefix": map[string]any{
					"type":        "string",
					"description": "Ky tu prefix cho moi bullet (mac dinh: •)",
					"default":     "•",
				},
			},
			"required":             []string{"content"},
			"additionalProperties": false,
		},
	}
}

func (s *BulletPointsSkill) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	content, ok := call.Arguments["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return tools.ToolResult{
			ToolCallID:     call.ID,
			ToolName:       call.Name,
			Success:        false,
			ContentForLLM:  "SKILL_ERROR: content is required",
			ContentForUser: "Vui long cung cap noi dung can chuyen.",
			Error:          &tools.ToolError{Code: "TOOL_INPUT_INVALID", Message: "content is required"},
		}
	}

	maxPoints := 5
	if v, ok := call.Arguments["max_points"]; ok {
		switch n := v.(type) {
		case float64:
			maxPoints = int(n)
		case int:
			maxPoints = n
		}
	}
	if maxPoints < 1 {
		maxPoints = 1
	}
	if maxPoints > 10 {
		maxPoints = 10
	}

	prefix := "•"
	if v, ok := call.Arguments["prefix"]; ok {
		if p, ok := v.(string); ok && strings.TrimSpace(p) != "" {
			prefix = strings.TrimSpace(p)
		}
	}

	// Tach cac diem chinh: uu tien theo cau, fallback theo dong
	points := extractPoints(content, maxPoints)
	if len(points) == 0 {
		msg := "Khong the trich xuat diem chinh tu noi dung nay."
		return tools.ToolResult{
			ToolCallID:     call.ID,
			ToolName:       call.Name,
			Success:        true,
			ContentForLLM:  msg,
			ContentForUser: msg,
		}
	}

	lines := make([]string, len(points))
	for i, p := range points {
		lines[i] = fmt.Sprintf("%s %s", prefix, p)
	}
	result := strings.Join(lines, "\n")
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "[Bullet points]:\n" + result,
		ContentForUser: result,
	}
}

// extractPoints tach van ban thanh cac diem chinh ngan gon.
func extractPoints(text string, max int) []string {
	// Thu tach theo dong truoc
	lines := strings.Split(text, "\n")
	var points []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "-•*·>")
		line = strings.TrimSpace(line)
		if len(line) > 5 {
			points = append(points, truncatePoint(line))
		}
		if len(points) >= max {
			return points
		}
	}
	// Neu it hon 2 dong, tach theo cau
	if len(points) < 2 {
		points = nil
		sentences := splitSentences(text)
		for _, s := range sentences {
			s = strings.TrimSpace(s)
			if len(s) > 5 {
				points = append(points, truncatePoint(s))
			}
			if len(points) >= max {
				break
			}
		}
	}
	return points
}

// truncatePoint gioi han chieu dai moi bullet point.
func truncatePoint(s string) string {
	const maxLen = 120
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
