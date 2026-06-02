package sandbox

import (
	"context"
	"strings"

	"vclaw/internal/tools"
)

const (
	ToolNameRunPython = "sandbox.runPython"
	ToolNameRunShell  = "sandbox.runShell"
)

type RunPythonTool struct{}

func (RunPythonTool) Name() string { return ToolNameRunPython }

func (RunPythonTool) Description() string {
	return "Run Python code inside the configured sandbox. This code execution action requires approval."
}

func (RunPythonTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"code":       map[string]any{"type": "string"},
			"workingDir": map[string]any{"type": "string"},
		},
		"required":             []string{"code"},
		"additionalProperties": false,
	}
}

func (RunPythonTool) Capability() tools.Capability { return tools.CapabilityMutating }

func (RunPythonTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelCodeExecution }

func (RunPythonTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	return sandboxNotConfigured(call)
}

type RunShellTool struct{}

func (RunShellTool) Name() string { return ToolNameRunShell }

func (RunShellTool) Description() string {
	return "Run a shell command inside the configured sandbox. This code execution action requires approval."
}

func (RunShellTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"command":    map[string]any{"type": "string"},
			"workingDir": map[string]any{"type": "string"},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	}
}

func (RunShellTool) Capability() tools.Capability { return tools.CapabilityMutating }

func (RunShellTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelCodeExecution }

func (RunShellTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	return sandboxNotConfigured(call)
}

func RegisterTools(registry *tools.ToolRegistry) error {
	if err := registry.RegisterWithEntry(RunPythonTool{}, tools.ToolRegistryEntry{Owner: "agent_core"}); err != nil {
		return err
	}
	if err := registry.RegisterWithEntry(RunShellTool{}, tools.ToolRegistryEntry{Owner: "agent_core"}); err != nil {
		return err
	}
	return nil
}

func sandboxNotConfigured(call tools.ToolCall) tools.ToolResult {
	message := "sandbox runner is not configured"
	if strings.TrimSpace(call.Name) != "" {
		message = call.Name + ": " + message
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  message,
		ContentForUser: message,
		Error: &tools.ToolError{
			Code:    tools.ErrorExecutionFailed,
			Message: message,
		},
	}
}
