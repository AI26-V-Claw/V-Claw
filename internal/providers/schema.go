package providers

import "vclaw/internal/tools"

func ToolDefinitionsFromRegistry(definitions []tools.ToolDefinition) []ToolDefinition {
	out := make([]ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if !definition.Enabled {
			continue
		}
		out = append(out, ToolDefinition{
			Name:        definition.Name,
			Description: definition.Description,
			Parameters:  cloneSchema(definition.Parameters),
		})
	}
	return out
}

func cloneSchema(schema tools.ToolSchema) map[string]any {
	if schema == nil {
		return nil
	}
	cloned := make(map[string]any, len(schema))
	for key, value := range schema {
		cloned[key] = value
	}
	return cloned
}
