package tools

import (
	"context"
	"fmt"
	"sort"
)

type ToolDefinition struct {
	Name        string
	Description string
	Parameters  ToolSchema
	Capability  Capability
	RiskLevel   RiskLevel
}

type ToolRegistry struct {
	tools map[string]Tool
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]Tool)}
}

func (r *ToolRegistry) Register(tool Tool) error {
	if tool == nil {
		return fmt.Errorf("tool is nil")
	}
	if tool.Name() == "" {
		return fmt.Errorf("tool name is required")
	}
	if _, exists := r.tools[tool.Name()]; exists {
		return fmt.Errorf("tool already registered: %s", tool.Name())
	}

	r.tools[tool.Name()] = tool
	return nil
}

func (r *ToolRegistry) GetTool(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

func (r *ToolRegistry) ListTools() []ToolDefinition {
	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		defs = append(defs, ToolDefinition{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Parameters(),
			Capability:  tool.Capability(),
			RiskLevel:   tool.RiskLevel(),
		})
	}

	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Name < defs[j].Name
	})

	return defs
}

func (r *ToolRegistry) Execute(ctx context.Context, call ToolCall) ToolResult {
	tool, ok := r.GetTool(call.Name)
	if !ok {
		return ToolNotFoundResult(call)
	}

	return tool.Execute(ctx, call)
}
