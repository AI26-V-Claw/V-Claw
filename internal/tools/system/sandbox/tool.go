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
	// DefaultSessionID is the session ID used when none is provided in the tool call arguments.
	// filesystem tools should use the same session workspace path as their AllowedRoot.
	DefaultSessionID = "agent"
	defaultSessionID = DefaultSessionID
)

type Config struct {
	Runner              runtime.Runner
	Guard               *runtime.WorkspaceGuard
	DefaultWorkspaceDir string
	DefaultSessionID    string
}

type RunPythonTool struct {
	cfg Config
}

func NewRunPythonTool(cfg Config) RunPythonTool {
	return RunPythonTool{cfg: normalizeConfig(cfg)}
}

func (RunPythonTool) Name() string { return ToolNameRunPython }

func (RunPythonTool) Description() string {
	return "Run Python code or a workspace-relative Python script inside the configured sandbox. Use this tool whenever you need to process a file that cannot be read as plain text — for example: extract text from a PDF using fitz/PyMuPDF or pdfplumber, parse Excel/CSV data with pandas, or read a Word document with python-docx. For PDF or long-document summarization, do not print the full extracted text; keep stdout bounded, preferably under 4000 characters, by printing page counts, total extracted length, and short page/section snippets or chunks. If full extraction is needed, write it to a workspace file and print only the file path plus a short preview. Available libraries: fitz/PyMuPDF, pdfplumber, pandas, numpy, openpyxl, xlrd, python-docx, chardet, PyYAML. The result includes workspace_dir — the absolute host path where files persist between runs. To reference a file created or found in the workspace (e.g. for chat.sendMessage attachments), use workspace_dir + filename. This code execution action requires approval."
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
		"required":             []string{"code"},
		"additionalProperties": false,
	}
}

func (RunPythonTool) Capability() tools.Capability { return tools.CapabilityMutating }

func (RunPythonTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelCodeExecution }

func (t RunPythonTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	if t.cfg.Runner == nil {
		return sandboxNotConfigured(call)
	}

	sessionID := stringArgumentOr(call.Arguments, defaultIfEmpty(t.cfg.DefaultSessionID, defaultSessionID), "session_id", "sessionId")
	workspaceDir, err := resolveWorkspaceDir(call.Arguments, t.cfg, sessionID)
	if err != nil {
		return sandboxInputError(call, err)
	}

	input := pytool.Input{
		RequestID:      requestID(call),
		SessionID:      sessionID,
		WorkspaceDir:   workspaceDir,
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
	return "Run a shell command inside the configured sandbox (Docker/Linux). Use relative filenames in commands (e.g. \"rm data.txt\"), not Windows absolute paths. For directories: \"rm -r dirname\". The result includes workspace_dir — the absolute host path for file references in other tools. Requires approval."
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

	sessionID := stringArgumentOr(call.Arguments, defaultIfEmpty(t.cfg.DefaultSessionID, defaultSessionID), "session_id", "sessionId")
	workspaceDir, err := resolveWorkspaceDir(call.Arguments, t.cfg, sessionID)
	if err != nil {
		return sandboxInputError(call, err)
	}

	input := shtool.Input{
		RequestID:      requestID(call),
		SessionID:      sessionID,
		WorkspaceDir:   workspaceDir,
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
		Group:            "sandbox",
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
	return cfg
}

func requestID(call tools.ToolCall) string {
	if strings.TrimSpace(call.ID) != "" {
		return call.ID
	}
	return "tool_call"
}

func resolveWorkspaceDir(args map[string]any, cfg Config, sessionID string) (string, error) {
	if cfg.Guard != nil {
		dir, _, err := cfg.Guard.PrepareSessionWorkspace(sessionID)
		if err != nil {
			return "", err
		}
		return dir, nil
	}
	return workspaceDir(args, cfg.DefaultWorkspaceDir), nil
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

func sandboxInputError(call tools.ToolCall, err error) tools.ToolResult {
	message := err.Error()
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
			Code:    tools.ErrorInvalidArgument,
			Message: message,
		},
	}
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
	return appendWorkspaceFilesNote(string(data), output.WorkspaceFiles)
}

func formatShellOutput(output shtool.Output) string {
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Sprintf("sandbox.runShell status=%s stdout=%q stderr=%q", output.Status, output.Stdout, output.Stderr)
	}
	return appendWorkspaceFilesNote(string(data), output.WorkspaceFiles)
}

// appendWorkspaceFilesNote adds a plain-text section listing absolute host paths
// of all workspace files. LLMs must use these exact paths when passing files to
// other tools (e.g. chat.sendMessage attachments) — never construct paths manually.
func appendWorkspaceFilesNote(base string, files []string) string {
	if len(files) == 0 {
		return base
	}
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\nFiles in workspace (use these exact absolute paths for attachments or other tools — do not modify them):\n")
	for _, f := range files {
		b.WriteString("  ")
		b.WriteString(f)
		b.WriteString("\n")
	}
	return b.String()
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
