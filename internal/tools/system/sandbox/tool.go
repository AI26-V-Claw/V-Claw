package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/sandbox/runtime"
	"vclaw/internal/tools"
	pytool "vclaw/internal/tools/os/python"
	shtool "vclaw/internal/tools/os/shell"
)

const (
	ToolNameRunPython = "sandbox.runPython"
	ToolNameRunShell  = "sandbox.runShell"
)

const (
	defaultSessionID = "agent"
	defaultUserID    = "agent"
)

type Config struct {
	Runner              runtime.Runner
	DefaultWorkspaceDir string
	DefaultSessionID    string
	DefaultUserID       string
}

type RunPythonTool struct {
	cfg Config
}

func NewRunPythonTool(cfg Config) RunPythonTool {
	return RunPythonTool{cfg: normalizeConfig(cfg)}
}

func (RunPythonTool) Name() string { return ToolNameRunPython }

func (RunPythonTool) Description() string {
	return "Run Python code or a workspace-relative Python script inside the configured sandbox. This code execution action requires approval."
}

func (RunPythonTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"code":            map[string]any{"type": "string", "description": "Inline Python code to run. Provide exactly one of code or script_path."},
			"script_path":     map[string]any{"type": "string", "description": "Workspace-relative Python script path to run. Provide exactly one of code or script_path."},
			"workingDir":      map[string]any{"type": "string"},
			"workspace_dir":   map[string]any{"type": "string"},
			"timeout_seconds": map[string]any{"type": "integer"},
		},
		"additionalProperties": false,
	}
}

func (RunPythonTool) Capability() tools.Capability { return tools.CapabilityMutating }

func (RunPythonTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelCodeExecution }

func (t RunPythonTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	if t.cfg.Runner == nil {
		return sandboxNotConfigured(call)
	}

	input := pytool.Input{
		RequestID:      requestID(call),
		SessionID:      stringArgumentOr(call.Arguments, defaultIfEmpty(t.cfg.DefaultSessionID, defaultSessionID), "session_id", "sessionId"),
		UserID:         stringArgumentOr(call.Arguments, defaultIfEmpty(t.cfg.DefaultUserID, defaultUserID), "user_id", "userId"),
		WorkspaceDir:   workspaceDir(call.Arguments, t.cfg.DefaultWorkspaceDir),
		Code:           stringArgument(call.Arguments, "code"),
		ScriptPath:     stringArgument(call.Arguments, "script_path", "scriptPath"),
		TimeoutSeconds: intArgument(call.Arguments, "timeout_seconds", "timeoutSeconds"),
		UserIntent:     stringArgument(call.Arguments, "user_intent", "userIntent"),
	}

	output, err := pytool.RunPython(ctx, input, t.cfg.Runner)
	return pythonToolResult(call, output, err)
}

type RunShellTool struct {
	cfg Config
}

func NewRunShellTool(cfg Config) RunShellTool {
	return RunShellTool{cfg: normalizeConfig(cfg)}
}

func (RunShellTool) Name() string { return ToolNameRunShell }

func (RunShellTool) Description() string {
	return "Run a shell command inside the configured sandbox. This code execution action requires approval."
}

func (RunShellTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"command":         map[string]any{"type": "string"},
			"workingDir":      map[string]any{"type": "string"},
			"workspace_dir":   map[string]any{"type": "string"},
			"timeout_seconds": map[string]any{"type": "integer"},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	}
}

func (RunShellTool) Capability() tools.Capability { return tools.CapabilityMutating }

func (RunShellTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelCodeExecution }

func (t RunShellTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	if t.cfg.Runner == nil {
		return sandboxNotConfigured(call)
	}

	input := shtool.Input{
		RequestID:      requestID(call),
		SessionID:      stringArgumentOr(call.Arguments, defaultIfEmpty(t.cfg.DefaultSessionID, defaultSessionID), "session_id", "sessionId"),
		UserID:         stringArgumentOr(call.Arguments, defaultIfEmpty(t.cfg.DefaultUserID, defaultUserID), "user_id", "userId"),
		WorkspaceDir:   workspaceDir(call.Arguments, t.cfg.DefaultWorkspaceDir),
		Command:        stringArgument(call.Arguments, "command"),
		TimeoutSeconds: intArgument(call.Arguments, "timeout_seconds", "timeoutSeconds"),
		UserIntent:     stringArgument(call.Arguments, "user_intent", "userIntent"),
	}

	output, err := shtool.RunShell(ctx, input, t.cfg.Runner)
	return shellToolResult(call, output, err)
}

func RegisterTools(registry *tools.ToolRegistry) error {
	return RegisterToolsWithConfig(registry, Config{})
}

func RegisterToolsWithConfig(registry *tools.ToolRegistry, cfg Config) error {
	if err := registry.RegisterWithEntry(NewRunPythonTool(cfg), registryEntry(2*time.Minute)); err != nil {
		return err
	}
	if err := registry.RegisterWithEntry(NewRunShellTool(cfg), registryEntry(2*time.Minute)); err != nil {
		return err
	}
	return nil
}

func registryEntry(timeout time.Duration) tools.ToolRegistryEntry {
	return tools.ToolRegistryEntry{
		Owner:            "agent_core",
		RequiresApproval: true,
		Timeout:          timeout,
	}
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

func normalizeConfig(cfg Config) Config {
	cfg.DefaultSessionID = defaultIfEmpty(cfg.DefaultSessionID, defaultSessionID)
	cfg.DefaultUserID = defaultIfEmpty(cfg.DefaultUserID, defaultUserID)
	return cfg
}

func requestID(call tools.ToolCall) string {
	if strings.TrimSpace(call.ID) != "" {
		return call.ID
	}
	return "tool_call"
}

func workspaceDir(args map[string]any, fallback string) string {
	return stringArgumentOr(args, fallback, "workspace_dir", "workingDir", "cwd")
}

func stringArgument(args map[string]any, names ...string) string {
	return stringArgumentOr(args, "", names...)
}

func stringArgumentOr(args map[string]any, fallback string, names ...string) string {
	for _, name := range names {
		if args == nil {
			continue
		}
		value, ok := args[name]
		if !ok {
			continue
		}
		if s, ok := value.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return fallback
}

func intArgument(args map[string]any, names ...string) int {
	for _, name := range names {
		if args == nil {
			continue
		}
		value, ok := args[name]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case int:
			return v
		case int32:
			return int(v)
		case int64:
			return int(v)
		case float64:
			return int(v)
		case json.Number:
			i, _ := v.Int64()
			return int(i)
		}
	}
	return 0
}

func defaultIfEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func pythonToolResult(call tools.ToolCall, output pytool.Output, err error) tools.ToolResult {
	content := formatPythonOutput(output)
	if err != nil {
		return tools.ToolResult{
			ToolCallID:     call.ID,
			ToolName:       call.Name,
			Success:        false,
			ContentForLLM:  content,
			ContentForUser: err.Error(),
			Error: &tools.ToolError{
				Code:    errorCodeForStatus(output.Status),
				Message: err.Error(),
			},
		}
	}
	if output.Status != string(runtime.JobSuccess) {
		message := failureMessage(output.Status, output.Stderr)
		return tools.ToolResult{
			ToolCallID:     call.ID,
			ToolName:       call.Name,
			Success:        false,
			ContentForLLM:  content,
			ContentForUser: userSummary(output.Status, output.Stdout, output.Stderr),
			Error: &tools.ToolError{
				Code:    errorCodeForStatus(output.Status),
				Message: message,
			},
		}
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: userSummary(output.Status, output.Stdout, output.Stderr),
	}
}

func shellToolResult(call tools.ToolCall, output shtool.Output, err error) tools.ToolResult {
	content := formatShellOutput(output)
	if err != nil {
		return tools.ToolResult{
			ToolCallID:     call.ID,
			ToolName:       call.Name,
			Success:        false,
			ContentForLLM:  content,
			ContentForUser: err.Error(),
			Error: &tools.ToolError{
				Code:    errorCodeForStatus(output.Status),
				Message: err.Error(),
			},
		}
	}
	if output.Status != string(runtime.JobSuccess) {
		message := failureMessage(output.Status, output.Stderr)
		return tools.ToolResult{
			ToolCallID:     call.ID,
			ToolName:       call.Name,
			Success:        false,
			ContentForLLM:  content,
			ContentForUser: userSummary(output.Status, output.Stdout, output.Stderr),
			Error: &tools.ToolError{
				Code:    errorCodeForStatus(output.Status),
				Message: message,
			},
		}
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: userSummary(output.Status, output.Stdout, output.Stderr),
	}
}

func formatPythonOutput(output pytool.Output) string {
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Sprintf("sandbox.runPython status=%s stdout=%q stderr=%q", output.Status, output.Stdout, output.Stderr)
	}
	return string(data)
}

func formatShellOutput(output shtool.Output) string {
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Sprintf("sandbox.runShell status=%s stdout=%q stderr=%q", output.Status, output.Stdout, output.Stderr)
	}
	return string(data)
}

func userSummary(status, stdout, stderr string) string {
	parts := []string{"sandbox status: " + defaultIfEmpty(status, "unknown")}
	if strings.TrimSpace(stdout) != "" {
		parts = append(parts, "stdout:\n"+stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		parts = append(parts, "stderr:\n"+stderr)
	}
	return strings.Join(parts, "\n")
}

func errorCodeForStatus(status string) string {
	switch status {
	case string(runtime.JobBlocked), string(runtime.JobRejected):
		return tools.ErrorBlockedByPolicy
	case string(runtime.JobTimeout):
		return tools.ErrorTimeout
	default:
		return tools.ErrorExecutionFailed
	}
}

func failureMessage(status, stderr string) string {
	if strings.TrimSpace(stderr) != "" {
		return strings.TrimSpace(stderr)
	}
	return "sandbox job finished with status " + defaultIfEmpty(status, "unknown")
}
