package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type ToolDefinition struct {
	Name             string
	Owner            string
	Group            string
	Description      string
	Parameters       ToolSchema
	Capability       Capability
	RiskLevel        RiskLevel
	RequiresApproval bool
	Timeout          time.Duration
	Enabled          bool
}

type ToolRegistryEntry struct {
	Name             string
	Owner            string
	Group            string
	Description      string
	Parameters       ToolSchema
	Capability       Capability
	RiskLevel        RiskLevel
	RequiresApproval bool
	Timeout          time.Duration
	Enabled          bool
	Disabled         bool
}

type ToolRegistry struct {
	tools map[string]registeredTool
}

type registeredTool struct {
	tool  Tool
	entry ToolRegistryEntry
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]registeredTool)}
}

func (r *ToolRegistry) Register(tool Tool) error {
	return r.RegisterWithEntry(tool, ToolRegistryEntry{})
}

func (r *ToolRegistry) RegisterWithEntry(tool Tool, entry ToolRegistryEntry) error {
	if tool == nil {
		return fmt.Errorf("tool is nil")
	}
	if tool.Name() == "" {
		return fmt.Errorf("tool name is required")
	}
	if _, exists := r.tools[tool.Name()]; exists {
		return fmt.Errorf("tool already registered: %s", tool.Name())
	}
	if entry.Name != "" && entry.Name != tool.Name() {
		return fmt.Errorf("tool entry name %q does not match tool name %q", entry.Name, tool.Name())
	}

	entry = normalizeEntry(tool, entry)
	r.tools[tool.Name()] = registeredTool{tool: tool, entry: entry}
	return nil
}

func (r *ToolRegistry) GetTool(name string) (Tool, bool) {
	registered, ok := r.tools[name]
	if !ok {
		return nil, false
	}
	return registered.tool, true
}

func (r *ToolRegistry) GetDefinition(name string) (ToolDefinition, bool) {
	registered, ok := r.tools[name]
	if !ok {
		return ToolDefinition{}, false
	}
	return definitionFromEntry(registered.entry), true
}

func (r *ToolRegistry) ListTools() []ToolDefinition {
	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, registered := range r.tools {
		defs = append(defs, definitionFromEntry(registered.entry))
	}

	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Name < defs[j].Name
	})

	return defs
}

func (r *ToolRegistry) ListToolsByGroup(group string) []ToolDefinition {
	var defs []ToolDefinition
	for _, registered := range r.tools {
		def := definitionFromEntry(registered.entry)
		if def.Group == group {
			defs = append(defs, def)
		}
	}
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Name < defs[j].Name
	})
	return defs
}

func (r *ToolRegistry) SetEnabled(name string, enabled bool) error {
	registered, ok := r.tools[name]
	if !ok {
		return fmt.Errorf("tool not found: %s", name)
	}
	registered.entry.Enabled = enabled
	registered.entry.Disabled = !enabled
	r.tools[name] = registered
	return nil
}

func (r *ToolRegistry) Execute(ctx context.Context, call ToolCall) ToolResult {
	tool, ok := r.GetTool(call.Name)
	if !ok {
		return ToolNotFoundResult(call)
	}

	return tool.Execute(ctx, call)
}

func normalizeEntry(tool Tool, entry ToolRegistryEntry) ToolRegistryEntry {
	if entry.Name == "" {
		entry.Name = tool.Name()
	}
	if entry.Owner == "" {
		entry.Owner = "agent_core"
	}
	if entry.Group == "" {
		entry.Group = inferGroup(entry.Name)
	}
	if entry.Description == "" {
		entry.Description = tool.Description()
	}
	if entry.Parameters == nil {
		entry.Parameters = tool.Parameters()
	}
	if entry.Capability == "" {
		entry.Capability = tool.Capability()
	}
	if entry.RiskLevel == "" {
		entry.RiskLevel = tool.RiskLevel()
	}
	if !entry.RequiresApproval {
		entry.RequiresApproval = requiresApproval(entry.Capability, entry.RiskLevel)
	}
	if entry.Disabled {
		entry.Enabled = false
	} else if !entry.Enabled {
		entry.Enabled = true
	}
	return entry
}

func definitionFromEntry(entry ToolRegistryEntry) ToolDefinition {
	return ToolDefinition{
		Name:             entry.Name,
		Owner:            entry.Owner,
		Group:            entry.Group,
		Description:      entry.Description,
		Parameters:       entry.Parameters,
		Capability:       entry.Capability,
		RiskLevel:        entry.RiskLevel,
		RequiresApproval: entry.RequiresApproval,
		Timeout:          entry.Timeout,
		Enabled:          entry.Enabled,
	}
}

func inferGroup(toolName string) string {
	switch {
	case strings.HasPrefix(toolName, "gmail.") ||
		strings.HasPrefix(toolName, "calendar.") ||
		strings.HasPrefix(toolName, "chat.") ||
		strings.HasPrefix(toolName, "people."):
		return "google_workspace"
	case strings.HasPrefix(toolName, "web."):
		return "web"
	case strings.HasPrefix(toolName, "sandbox."):
		return "sandbox"
	default:
		return "builtin"
	}
}

func requiresApproval(capability Capability, riskLevel RiskLevel) bool {
	if capability != CapabilityReadOnly {
		return true
	}
	switch riskLevel {
	case RiskLevelSafeRead, RiskLevelSafeCompute:
		return false
	default:
		return true
	}
}
