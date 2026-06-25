// Package filesystem provides file system tools (list, read, write, info)
// for the V-Claw agent. All operations are restricted to allowed workspace
// directories via PathGuard.
package filesystem

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vclaw/internal/filesafety"
	"vclaw/internal/tools"
)

const (
	ToolNameListDir   = "filesystem.listDir"
	ToolNameReadFile  = "filesystem.readFile"
	ToolNameFileInfo  = "filesystem.fileInfo"
	ToolNameWriteFile = "filesystem.writeFile"

	maxReadFileChars  = 8000
	maxReadFileBytes  = int64(1024 * 1024)
	maxListEntries    = 200
	maxRecursiveDepth = 5
)

// Config holds configuration for filesystem tools registration.
type Config struct {
	// AllowedRoots restricts file access to these directories.
	// If empty, all paths are allowed (for testing only).
	AllowedRoots []string
}

// ─── ListDir ─────────────────────────────────────────────────────────────────

// ListDirTool lists files and subdirectories at a given path.
// Supports glob pattern filtering and recursive traversal with depth limit.
type ListDirTool struct {
	guard PathGuard
}

func NewListDirTool(guard PathGuard) ListDirTool {
	return ListDirTool{guard: guard}
}

func (ListDirTool) Name() string { return ToolNameListDir }

func (ListDirTool) Description() string {
	return "List files and subdirectories at the given path. Requires a directory path — do not pass a filename. Returns name, size, type, and last modified time for each entry."
}

func (ListDirTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"path":      map[string]any{"type": "string", "description": "Directory path to list. Can be relative to workspace."},
			"pattern":   map[string]any{"type": "string", "description": "Optional glob pattern to filter entries (e.g. *.go, *.xlsx)."},
			"recursive": map[string]any{"type": "boolean", "description": "If true, list entries recursively."},
			"max_depth": map[string]any{"type": "integer", "description": "Maximum depth for recursive listing. Default 3."},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func (ListDirTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (ListDirTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSafeRead }

func (t ListDirTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	path := stringArg(call.Arguments, "path")
	pattern := stringArg(call.Arguments, "pattern")
	recursive := boolArg(call.Arguments, "recursive")
	maxDepth := intArg(call.Arguments, "max_depth")
	if maxDepth <= 0 {
		maxDepth = 3
	}
	if maxDepth > maxRecursiveDepth {
		maxDepth = maxRecursiveDepth
	}

	resolved, err := t.guard.Resolve(path)
	if err != nil {
		return inputError(call, err.Error())
	}

	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return inputError(call, fmt.Sprintf("directory not found: %s", path))
		}
		return execError(call, err)
	}
	if !info.IsDir() {
		return inputError(call, fmt.Sprintf("%s is not a directory", path))
	}

	var entries []string
	count := 0

	if recursive {
		err = filepath.WalkDir(resolved, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil // skip errors
			}
			rel, _ := filepath.Rel(resolved, p)
			if rel == "." {
				return nil
			}
			depth := strings.Count(filepath.ToSlash(rel), "/") + 1
			if depth > maxDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if pattern != "" {
				matched, _ := filepath.Match(pattern, d.Name())
				if !matched && !d.IsDir() {
					return nil
				}
			}
			count++
			if count > maxListEntries {
				return fmt.Errorf("too many entries")
			}
			info, infoErr := d.Info()
			if infoErr != nil {
				return nil
			}
			entries = append(entries, formatEntry(rel, info))
			return nil
		})
	} else {
		dirEntries, readErr := os.ReadDir(resolved)
		if readErr != nil {
			return execError(call, readErr)
		}
		for _, d := range dirEntries {
			if pattern != "" {
				matched, _ := filepath.Match(pattern, d.Name())
				if !matched {
					continue
				}
			}
			count++
			if count > maxListEntries {
				break
			}
			info, infoErr := d.Info()
			if infoErr != nil {
				continue
			}
			entries = append(entries, formatEntry(d.Name(), info))
		}
	}

	if len(entries) == 0 {
		content := fmt.Sprintf("Directory %s is empty.", resolved)
		if pattern != "" {
			content = fmt.Sprintf("No entries matching %q in %s.", pattern, resolved)
		}
		return tools.ToolResult{
			ToolCallID: call.ID, ToolName: call.Name, Success: true,
			ContentForLLM: content, ContentForUser: content,
			ArtifactRef: &tools.ToolArtifactRef{Kind: "file", URI: resolved, Label: path},
			Metadata:    map[string]any{"entry_count": 0},
		}
	}

	header := fmt.Sprintf("Contents of %s", resolved)
	if pattern != "" {
		header += fmt.Sprintf(" (pattern: %s)", pattern)
	}
	content := header + "\n" + strings.Join(entries, "\n")
	truncatedList := count > maxListEntries
	if truncatedList {
		content += fmt.Sprintf("\n[truncated — showing %d of %d+ entries]", maxListEntries, count)
	}

	displayCount := len(entries)
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
		Truncated:      truncatedList,
		ArtifactRef:    &tools.ToolArtifactRef{Kind: "file", URI: resolved, Label: path},
		Metadata:       map[string]any{"entry_count": displayCount},
	}
}

// ─── ReadFile ────────────────────────────────────────────────────────────────

// ReadFileTool reads text content from a file with optional line range.
// Output is truncated to 8000 chars to avoid overwhelming the LLM context.
type ReadFileTool struct {
	guard PathGuard
}

func NewReadFileTool(guard PathGuard) ReadFileTool {
	return ReadFileTool{guard: guard}
}

func (ReadFileTool) Name() string { return ToolNameReadFile }

func (ReadFileTool) Description() string {
	return "Read the text content of a plain-text file (e.g. .txt, .md, .json, .csv, .go, .py). Returns the file content, optionally limited to a line range. For binary files such as PDF, Excel (.xlsx), or Word (.docx), use sandbox.runPython instead (e.g. pdfplumber for PDF, openpyxl for Excel)."
}

func (ReadFileTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"path":       map[string]any{"type": "string", "description": "File path to read. Can be relative to workspace."},
			"start_line": map[string]any{"type": "integer", "description": "Optional 1-based start line number."},
			"end_line":   map[string]any{"type": "integer", "description": "Optional 1-based end line number."},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func (ReadFileTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (ReadFileTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSafeRead }

func (t ReadFileTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	path := stringArg(call.Arguments, "path")
	startLine := intArg(call.Arguments, "start_line")
	endLine := intArg(call.Arguments, "end_line")

	resolved, err := t.guard.Resolve(path)
	if err != nil {
		return inputError(call, err.Error())
	}

	decision, err := filesafety.ScanPath(resolved, filesafety.Input{
		Filename:     filepath.Base(resolved),
		Origin:       "local_workspace",
		SourceTool:   ToolNameReadFile,
		MaxSizeBytes: maxReadFileBytes,
	})
	if err != nil {
		return execError(call, err)
	}
	if !decision.Allowed() {
		return tools.ToolResult{
			ToolCallID:     call.ID,
			ToolName:       call.Name,
			Success:        false,
			ContentForLLM:  "File blocked by safety gate: " + decision.ReasonUser,
			ContentForUser: decision.ReasonUser,
			Error:          &tools.ToolError{Code: tools.ErrorBlockedByPolicy, Message: decision.ReasonUser},
			Metadata:       map[string]any{"file_safety": decision.Metadata()},
		}
	}

	data, err := readFileLimited(resolved, maxReadFileBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return inputError(call, fmt.Sprintf("file not found: %s", path))
		}
		return execError(call, err)
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	// Apply line range if specified
	if startLine > 0 || endLine > 0 {
		if startLine <= 0 {
			startLine = 1
		}
		if endLine <= 0 || endLine > totalLines {
			endLine = totalLines
		}
		if startLine > totalLines {
			return inputError(call, fmt.Sprintf("start_line %d exceeds total lines %d", startLine, totalLines))
		}
		if startLine > endLine {
			return inputError(call, "start_line must be <= end_line")
		}
		lines = lines[startLine-1 : endLine]
		content = strings.Join(lines, "\n")
	}

	truncated := false
	if len(content) > maxReadFileChars {
		content = content[:maxReadFileChars-20] + "\n[content truncated]"
		truncated = true
	}

	header := fmt.Sprintf("File: %s (%d lines)", resolved, totalLines)
	if startLine > 0 && endLine > 0 {
		header = fmt.Sprintf("File: %s (lines %d-%d of %d)", resolved, startLine, endLine, totalLines)
	}
	if truncated {
		header += " [truncated]"
	}

	resultContent := header + "\n" + content
	meta := map[string]any{
		"total_lines": totalLines,
		"size_bytes":  len(data),
		"file_safety": decision.Metadata(),
	}
	if startLine > 0 || endLine > 0 {
		meta["start_line"] = startLine
		meta["end_line"] = endLine
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  resultContent,
		ContentForUser: resultContent,
		Truncated:      truncated,
		ArtifactRef:    &tools.ToolArtifactRef{Kind: "file", URI: resolved, Label: filepath.Base(resolved)},
		Metadata:       meta,
	}
}

// ─── FileInfo ────────────────────────────────────────────────────────────────

// FileInfoTool returns metadata about a file or directory:
// size, type, last modified time, permissions, and extension.
type FileInfoTool struct {
	guard PathGuard
}

func NewFileInfoTool(guard PathGuard) FileInfoTool {
	return FileInfoTool{guard: guard}
}

func (FileInfoTool) Name() string { return ToolNameFileInfo }

func (FileInfoTool) Description() string {
	return "Get metadata about a file or directory. Pass just a filename to locate it anywhere in the workspace. If not found, call filesystem.listDir with path='.' to list all workspace files."
}

func (FileInfoTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Path to the file or directory."},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func (FileInfoTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (FileInfoTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSafeRead }

func (t FileInfoTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	path := stringArg(call.Arguments, "path")

	resolved, err := t.guard.Resolve(path)
	if err != nil {
		return inputError(call, err.Error())
	}

	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return inputError(call, fmt.Sprintf("not found: %s", path))
		}
		return execError(call, err)
	}

	fileType := "file"
	if info.IsDir() {
		fileType = "directory"
	}

	meta := map[string]any{
		"type":        fileType,
		"size_bytes":  info.Size(),
		"modified_at": info.ModTime().Format(time.RFC3339),
		"permissions": info.Mode().String(),
	}

	content := fmt.Sprintf("Path: %s\nType: %s\nSize: %s\nModified: %s\nPermissions: %s",
		resolved, fileType, formatSize(info.Size()), info.ModTime().Format(time.RFC3339), info.Mode().String())

	if !info.IsDir() {
		ext := filepath.Ext(info.Name())
		content += fmt.Sprintf("\nExtension: %s", ext)
		meta["extension"] = ext
	} else {
		entries, err := os.ReadDir(resolved)
		if err == nil {
			dirs, files := 0, 0
			for _, e := range entries {
				if e.IsDir() {
					dirs++
				} else {
					files++
				}
			}
			content += fmt.Sprintf("\nContains: %d files, %d directories", files, dirs)
			meta["file_count"] = files
			meta["dir_count"] = dirs
		}
	}

	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
		ArtifactRef:    &tools.ToolArtifactRef{Kind: "file", URI: resolved, Label: filepath.Base(resolved)},
		Metadata:       meta,
	}
}

// ─── WriteFile ───────────────────────────────────────────────────────────────

// WriteFileTool writes content to a file with create, overwrite, or append modes.
// Classified as mutating + local_write risk, so it requires HITL approval.
type WriteFileTool struct {
	guard PathGuard
}

func NewWriteFileTool(guard PathGuard) WriteFileTool {
	return WriteFileTool{guard: guard}
}

func (WriteFileTool) Name() string { return ToolNameWriteFile }

func (WriteFileTool) Description() string {
	return "Write content to a file. Can create new files or overwrite/append to existing ones. This action requires approval."
}

func (WriteFileTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "File path to write to. Can be relative to workspace."},
			"content": map[string]any{"type": "string", "description": "Content to write to the file."},
			"mode":    map[string]any{"type": "string", "enum": []string{"create", "overwrite", "append"}, "description": "Write mode. 'create' fails if file exists. 'overwrite' replaces content. 'append' adds to end."},
		},
		"required":             []string{"path", "content"},
		"additionalProperties": false,
	}
}

func (WriteFileTool) Capability() tools.Capability { return tools.CapabilityMutating }
func (WriteFileTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelLocalWrite }

func (t WriteFileTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	path := stringArg(call.Arguments, "path")
	content, _ := call.Arguments["content"].(string) // preserve whitespace in content
	mode := stringArg(call.Arguments, "mode")
	if mode == "" {
		mode = "create"
	}

	if content == "" {
		return inputError(call, "content is required")
	}

	resolved, err := t.guard.Resolve(path)
	if err != nil {
		return inputError(call, err.Error())
	}

	// Ensure parent directory exists
	dir := filepath.Dir(resolved)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return execError(call, fmt.Errorf("create directory: %w", err))
	}

	switch mode {
	case "create":
		if _, err := os.Stat(resolved); err == nil {
			return inputError(call, fmt.Sprintf("file already exists: %s (use mode 'overwrite' or 'append')", path))
		}
		if err := os.WriteFile(resolved, []byte(content), 0644); err != nil {
			return execError(call, err)
		}
	case "overwrite":
		if err := os.WriteFile(resolved, []byte(content), 0644); err != nil {
			return execError(call, err)
		}
	case "append":
		f, err := os.OpenFile(resolved, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return execError(call, err)
		}
		defer f.Close()
		if _, err := f.WriteString(content); err != nil {
			return execError(call, err)
		}
	default:
		return inputError(call, fmt.Sprintf("invalid mode %q: must be create, overwrite, or append", mode))
	}

	summary := fmt.Sprintf("Successfully wrote %d bytes to %s (mode: %s)", len(content), path, mode)
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  summary,
		ContentForUser: summary,
		ArtifactRef:    &tools.ToolArtifactRef{Kind: "file", URI: resolved, Label: filepath.Base(resolved)},
		Metadata:       map[string]any{"bytes_written": len(content), "mode": mode},
	}
}

// ─── Registration ────────────────────────────────────────────────────────────

// RegisterTools registers all filesystem tools into the given registry.
// All tools share the same PathGuard configured via Config.
func RegisterTools(registry *tools.ToolRegistry, config Config) error {
	guard := NewPathGuard(config.AllowedRoots)

	allTools := []tools.Tool{
		NewListDirTool(guard),
		NewReadFileTool(guard),
		NewFileInfoTool(guard),
		NewWriteFileTool(guard),
	}

	for _, tool := range allTools {
		entry := tools.ToolRegistryEntry{
			Owner: "agent_core",
			Group: "filesystem",
		}
		if err := registry.RegisterWithEntry(tool, entry); err != nil {
			return err
		}
	}
	return nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// formatEntry formats a single directory entry as a human-readable line.
func formatEntry(name string, info fs.FileInfo) string {
	fileType := "FILE"
	if info.IsDir() {
		fileType = "DIR "
	}
	return fmt.Sprintf("  %s  %8s  %s  %s",
		fileType, formatSize(info.Size()), info.ModTime().Format("2006-01-02 15:04"), name)
}

// formatSize converts bytes to a human-readable size string (e.g. 1.5KB, 3.2MB).
func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

func readFileLimited(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, limit+1))
}

func stringArg(args map[string]any, name string) string {
	if args == nil {
		return ""
	}
	value, _ := args[name].(string)
	return strings.TrimSpace(value)
}

func intArg(args map[string]any, name string) int {
	if args == nil {
		return 0
	}
	switch v := args[name].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func boolArg(args map[string]any, name string) bool {
	if args == nil {
		return false
	}
	v, _ := args[name].(bool)
	return v
}

// inputError returns a standardized error result for invalid tool arguments.
func inputError(call tools.ToolCall, message string) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  "Invalid input: " + message,
		ContentForUser: message,
		Error: &tools.ToolError{
			Code:    tools.ErrorInvalidArgument,
			Message: message,
		},
	}
}

// execError returns a standardized error result for runtime/execution failures.
func execError(call tools.ToolCall, err error) tools.ToolResult {
	message := err.Error()
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  "Execution error: " + message,
		ContentForUser: message,
		Error: &tools.ToolError{
			Code:    tools.ErrorExecutionFailed,
			Message: message,
		},
	}
}
