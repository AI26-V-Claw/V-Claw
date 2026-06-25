package skills

import (
	"context"
	"vclaw/internal/tools"
)

// SkillDefinition describes the full metadata of a skill/plugin.
type SkillDefinition struct {
	// Name is the unique identifier used as the tool name (e.g. "skill.deep_research")
	Name string `json:"name"`
	// Description tells the LLM when and how to use this skill
	Description string `json:"description"`
	// Version follows semver (e.g. "1.0.0")
	Version string `json:"version"`
	// Tags are free-form labels for grouping/filtering (e.g. ["research", "web"])
	Tags []string `json:"tags"`
	// Scope limits the domains this skill may operate in (e.g. ["email", "calendar"])
	Scope []string `json:"scope"`
	// Permissions lists required tool permissions (e.g. ["gmail.read", "web.search"])
	Permissions []string `json:"permissions"`
	// RelatedSkills lists names of skills that complement this one
	RelatedSkills []string `json:"related_skills"`
	// Fallback is the message returned when the skill cannot handle the request
	Fallback string `json:"fallback"`
	// Parameters is the JSON schema for the skill input
	Parameters tools.ToolSchema `json:"parameters"`
	// RiskLevel declares the risk level of this skill
	RiskLevel tools.RiskLevel `json:"risk_level"`
	// Enabled allows toggling the skill without deleting it
	Enabled bool `json:"enabled"`
}

// SkillPlugin is the interface every skill/plugin must implement.
type SkillPlugin interface {
	// Definition returns the full metadata of the skill
	Definition() SkillDefinition
	// Execute runs the skill with the given tool call input
	Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult
}