package skills

import (
	"context"
	"vclaw/internal/tools"
)

// SkillDefinition mô tả metadata đầy đủ của một skill/plugin.
type SkillDefinition struct {
	// Name là định danh duy nhất của skill, dùng làm tool name (e.g. "skill.summarize")
	Name string `json:"name"`
	// Description mô tả skill cho LLM biết khi nào dùng skill này
	Description string `json:"description"`
	// Scope giới hạn domain mà skill được phép hoạt động (e.g. ["email", "calendar"])
	Scope []string `json:"scope"`
	// Permissions liệt kê các quyền cần thiết (e.g. ["gmail.read"])
	Permissions []string `json:"permissions"`
	// Fallback là message trả về khi skill không xử lý được request
	Fallback string `json:"fallback"`
	// Parameters là JSON schema cho input của skill
	Parameters tools.ToolSchema `json:"parameters"`
	// RiskLevel khai báo mức độ rủi ro
	RiskLevel tools.RiskLevel `json:"risk_level"`
	// Enabled cho phép bật/tắt skill mà không cần xóa
	Enabled bool `json:"enabled"`
}

// SkillPlugin là interface mà mọi skill/plugin phải implement.
type SkillPlugin interface {
	// Definition trả về metadata đầy đủ của skill
	Definition() SkillDefinition
	// Execute thực thi skill với input đã cho
	Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult
}
