package skills

import (
	"context"
	"fmt"
	"strings"

	"vclaw/internal/tools"
)

// skillAdapter wraps SkillPlugin thành tools.Tool.
type skillAdapter struct {
	plugin SkillPlugin
	def    SkillDefinition
}

func (a *skillAdapter) Name() string                 { return a.def.Name }
func (a *skillAdapter) Description() string          { return a.def.Description }
func (a *skillAdapter) Parameters() tools.ToolSchema { return a.def.Parameters }
func (a *skillAdapter) Capability() tools.Capability {
	if a.def.RiskLevel == tools.RiskLevelSafeRead || a.def.RiskLevel == tools.RiskLevelSafeCompute {
		return tools.CapabilityReadOnly
	}
	return tools.CapabilityMutating
}
func (a *skillAdapter) RiskLevel() tools.RiskLevel { return a.def.RiskLevel }
func (a *skillAdapter) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	if !a.def.Enabled {
		fallback := a.def.Fallback
		if strings.TrimSpace(fallback) == "" {
			fallback = fmt.Sprintf("Skill %q hien khong kha dung.", a.def.Name)
		}
		return tools.ToolResult{
			ToolCallID:     call.ID,
			ToolName:       call.Name,
			Success:        false,
			ContentForLLM:  "SKILL_DISABLED: " + fallback,
			ContentForUser: fallback,
			Error: &tools.ToolError{
				Code:    "SKILL_DISABLED",
				Message: fallback,
			},
		}
	}

	// Scope enforcement: neu skill co khai bao scope va caller truyen _domain,
	// kiem tra domain co nam trong scope khong.
	// _domain la optional — neu khong co, bo qua scope check.
	if domain, ok := skillDomainFromCall(call); ok {
		if !ScopeAllowed(a.def, domain) {
			msg := fmt.Sprintf(
				"Skill %q khong ho tro domain %q. Pham vi cho phep: %s.",
				a.def.Name, domain, strings.Join(a.def.Scope, ", "),
			)
			if strings.TrimSpace(a.def.Fallback) != "" {
				msg = a.def.Fallback
			}
			return tools.ToolResult{
				ToolCallID:     call.ID,
				ToolName:       call.Name,
				Success:        false,
				ContentForLLM:  "SKILL_OUT_OF_SCOPE: " + msg,
				ContentForUser: msg,
				Error: &tools.ToolError{
					Code:    "SKILL_OUT_OF_SCOPE",
					Message: msg,
				},
			}
		}
	}

	return a.plugin.Execute(ctx, call)
}

// skillDomainFromCall trich domain tu arguments cua tool call.
// Tra ve ("", false) neu khong co _domain argument.
func skillDomainFromCall(call tools.ToolCall) (string, bool) {
	v, ok := call.Arguments["_domain"]
	if !ok {
		return "", false
	}
	domain, ok := v.(string)
	if !ok || strings.TrimSpace(domain) == "" {
		return "", false
	}
	return strings.TrimSpace(domain), true
}

// RegisterSkill đăng ký một SkillPlugin vào ToolRegistry với Group="skill".
// Trả về lỗi nếu skill bị disabled hoặc tên rỗng.
func RegisterSkill(registry *tools.ToolRegistry, plugin SkillPlugin) error {
	def := plugin.Definition()
	if strings.TrimSpace(def.Name) == "" {
		return fmt.Errorf("skill name must not be empty")
	}
	if !def.Enabled {
		// Vẫn register nhưng adapter sẽ trả fallback — agent biết skill tồn tại nhưng disabled
	}
	adapter := &skillAdapter{plugin: plugin, def: def}
	return registry.RegisterWithEntry(adapter, tools.ToolRegistryEntry{
		Name:        def.Name,
		Group:       "skill",
		Owner:       "skill_loader",
		Description: def.Description,
		Parameters:  def.Parameters,
		RiskLevel:   def.RiskLevel,
		Enabled:     def.Enabled,
		Disabled:    !def.Enabled,
	})
}

// RegisterSkills đăng ký nhiều skills cùng lúc. Dừng và trả lỗi ngay nếu có skill lỗi.
func RegisterSkills(registry *tools.ToolRegistry, plugins []SkillPlugin) error {
	for _, plugin := range plugins {
		if err := RegisterSkill(registry, plugin); err != nil {
			return fmt.Errorf("register skill %q: %w", plugin.Definition().Name, err)
		}
	}
	return nil
}
