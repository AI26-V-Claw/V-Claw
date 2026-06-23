// Package memory provides agent-callable tools for user-facing memory management.
// Users can view, edit, and reset their long-term memory (USER.md + NOTES.md)
// through the agent. All mutations are audit-logged.
package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/audit"
	"vclaw/internal/longmem"
	"vclaw/internal/tools"
)

const (
	ToolNameGetUserMemory  = "memory.getUserMemory"
	ToolNameEditUserMemory = "memory.editUserMemory"
	ToolNameResetMemory    = "memory.resetMemory"
)

// RegistryEntries exposes tool metadata for contract drift tests.
var RegistryEntries = []struct {
	Name             string
	Owner            string
	Capability       tools.Capability
	RiskLevel        tools.RiskLevel
	RequiresApproval bool
}{
	{Name: ToolNameGetUserMemory, Owner: "agent_core", Capability: tools.CapabilityReadOnly, RiskLevel: tools.RiskLevelSafeRead, RequiresApproval: false},
	{Name: ToolNameEditUserMemory, Owner: "agent_core", Capability: tools.CapabilityMutating, RiskLevel: tools.RiskLevelLocalWrite, RequiresApproval: true},
	{Name: ToolNameResetMemory, Owner: "agent_core", Capability: tools.CapabilityMutating, RiskLevel: tools.RiskLevelDestructive, RequiresApproval: true},
}

// --- Service ---

// Service holds shared dependencies for memory tools.
type Service struct {
	memoryDir  string
	auditLogger audit.AuditEventLogger
}

// NewService creates a memory Service reading from memoryDir.
func NewService(memoryDir string) *Service {
	return &Service{memoryDir: memoryDir}
}

// WithAuditLogger sets the audit logger for mutation tools.
func (s *Service) WithAuditLogger(logger audit.AuditEventLogger) *Service {
	s.auditLogger = logger
	return s
}

func (s *Service) auditLog(toolName, preview string) {
	if s.auditLogger == nil {
		return
	}
	s.auditLogger.Log(audit.AuditEvent{
		EventType:      audit.EventToolRequest,
		Timestamp:      time.Now().UTC(),
		Tool:           toolName,
		ActionType:     audit.ActionFileWrite,
		Status:         audit.StatusExecuted,
		CommandPreview: preview,
	})
}

// --- getUserMemory tool ---

type getUserMemoryTool struct{ service *Service }

func (t *getUserMemoryTool) Name() string                  { return ToolNameGetUserMemory }
func (t *getUserMemoryTool) Description() string            { return "Xem bộ nhớ dài hạn hiện tại (USER.md + NOTES.md) của người dùng." }
func (t *getUserMemoryTool) Capability() tools.Capability   { return tools.CapabilityReadOnly }
func (t *getUserMemoryTool) RiskLevel() tools.RiskLevel     { return tools.RiskLevelSafeRead }

func (t *getUserMemoryTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *getUserMemoryTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	userMD, notesMD, err := longmem.ReadFiles(t.service.memoryDir)
	if err != nil {
		return tools.ExecutionErrorResult(call, err)
	}

	var parts []string
	if strings.TrimSpace(userMD) != "" {
		parts = append(parts, "=== USER.md ===\n"+strings.TrimSpace(userMD))
	}
	if strings.TrimSpace(notesMD) != "" {
		parts = append(parts, "=== NOTES.md ===\n"+strings.TrimSpace(notesMD))
	}

	if len(parts) == 0 {
		return tools.ToolResult{
			ToolCallID:     call.ID,
			ToolName:       call.Name,
			Success:        true,
			ContentForLLM:  "Bộ nhớ trống — chưa có dữ liệu nào.",
			ContentForUser: "Bộ nhớ trống — chưa có dữ liệu nào.",
		}
	}

	content := strings.Join(parts, "\n\n")
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
	}
}

// --- editUserMemory tool ---

type editUserMemoryTool struct{ service *Service }

func (t *editUserMemoryTool) Name() string                  { return ToolNameEditUserMemory }
func (t *editUserMemoryTool) Description() string            { return "Thêm hoặc xóa một fact trong bộ nhớ dài hạn. Cần approval trước khi thực thi." }
func (t *editUserMemoryTool) Capability() tools.Capability   { return tools.CapabilityMutating }
func (t *editUserMemoryTool) RiskLevel() tools.RiskLevel     { return tools.RiskLevelLocalWrite }

func (t *editUserMemoryTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"add", "remove"},
				"description": "Hành động: 'add' thêm fact mới, 'remove' xóa fact theo từ khóa.",
			},
			"target": map[string]any{
				"type":        "string",
				"enum":        []string{"user", "notes"},
				"description": "File memory: 'user' cho USER.md, 'notes' cho NOTES.md.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Nội dung fact (add) hoặc từ khóa cần xóa (remove).",
			},
			"category": map[string]any{
				"type":        "string",
				"enum":        []string{"Thông tin cơ bản", "Sở thích làm việc", "Người quen thuộc", "Quy tắc làm việc"},
				"description": "Nhóm trong USER.md. Bắt buộc khi target=user và action=add.",
			},
		},
		"required": []string{"action", "target", "content"},
	}
}

func (t *editUserMemoryTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	action := strings.ToLower(strings.TrimSpace(stringArg(call.Arguments, "action")))
	target := strings.ToLower(strings.TrimSpace(stringArg(call.Arguments, "target")))
	content := strings.TrimSpace(stringArg(call.Arguments, "content"))
	category := strings.TrimSpace(stringArg(call.Arguments, "category"))

	if action != "add" && action != "remove" {
		return invalidArgumentResult(call, "action phải là 'add' hoặc 'remove'")
	}
	if target != "user" && target != "notes" {
		return invalidArgumentResult(call, "target phải là 'user' hoặc 'notes'")
	}
	if content == "" {
		return invalidArgumentResult(call, "content không được để trống")
	}
	if action == "add" && target == "user" && category == "" {
		return invalidArgumentResult(call, "category bắt buộc khi thêm fact vào USER.md")
	}
	// Enforce the data contract (docs/03-contracts.md §9.1): never write
	// credentials/tokens/secrets into long-term memory. Applies to 'add' only;
	// for 'remove', content is a search keyword, not stored data.
	if action == "add" {
		if err := longmem.ValidateMemoryContent(content); err != nil {
			return invalidArgumentResult(call, err.Error())
		}
	}

	var msg, preview string

	switch {
	case action == "add" && target == "user":
		if err := longmem.AddUserFact(t.service.memoryDir, category, content); err != nil {
			return tools.ExecutionErrorResult(call, err)
		}
		msg = fmt.Sprintf("Đã thêm fact vào USER.md (%s): %s", category, content)
		preview = fmt.Sprintf("add user/%s: %s", category, content)

	case action == "add" && target == "notes":
		if err := longmem.AddNotesFact(t.service.memoryDir, content); err != nil {
			return tools.ExecutionErrorResult(call, err)
		}
		msg = fmt.Sprintf("Đã thêm fact vào NOTES.md: %s", content)
		preview = fmt.Sprintf("add notes: %s", content)

	case action == "remove" && target == "user":
		removed, err := longmem.RemoveUserFact(t.service.memoryDir, content)
		if err != nil {
			return tools.ExecutionErrorResult(call, err)
		}
		if !removed {
			msg = fmt.Sprintf("Không tìm thấy fact nào chứa '%s' trong USER.md", content)
		} else {
			msg = fmt.Sprintf("Đã xóa fact chứa '%s' khỏi USER.md", content)
		}
		preview = fmt.Sprintf("remove user: %s", content)

	case action == "remove" && target == "notes":
		removed, err := longmem.RemoveNotesFact(t.service.memoryDir, content)
		if err != nil {
			return tools.ExecutionErrorResult(call, err)
		}
		if !removed {
			msg = fmt.Sprintf("Không tìm thấy fact nào chứa '%s' trong NOTES.md", content)
		} else {
			msg = fmt.Sprintf("Đã xóa fact chứa '%s' khỏi NOTES.md", content)
		}
		preview = fmt.Sprintf("remove notes: %s", content)
	}

	t.service.auditLog(call.Name, preview)

	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  msg,
		ContentForUser: msg,
	}
}

// --- resetMemory tool ---

type resetMemoryTool struct{ service *Service }

func (t *resetMemoryTool) Name() string                  { return ToolNameResetMemory }
func (t *resetMemoryTool) Description() string            { return "Xóa toàn bộ bộ nhớ dài hạn và tạo lại skeleton mặc định. Cần approval." }
func (t *resetMemoryTool) Capability() tools.Capability   { return tools.CapabilityMutating }
func (t *resetMemoryTool) RiskLevel() tools.RiskLevel     { return tools.RiskLevelDestructive }

func (t *resetMemoryTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *resetMemoryTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	if err := longmem.ResetAll(t.service.memoryDir); err != nil {
		return tools.ExecutionErrorResult(call, err)
	}

	t.service.auditLog(call.Name, "reset all memory")

	msg := "Đã xóa toàn bộ bộ nhớ dài hạn. USER.md và NOTES.md đã được tạo lại với skeleton mặc định."
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  msg,
		ContentForUser: msg,
	}
}

// --- Registration ---

// RegisterTools registers all memory management tools into the given registry.
func RegisterTools(registry *tools.ToolRegistry, memoryDir string, auditLogger audit.AuditEventLogger) error {
	svc := NewService(memoryDir)
	if auditLogger != nil {
		svc = svc.WithAuditLogger(auditLogger)
	}

	entry := tools.ToolRegistryEntry{Owner: "agent_core", Group: "memory"}
	for _, t := range []tools.Tool{
		&getUserMemoryTool{service: svc},
		&editUserMemoryTool{service: svc},
		&resetMemoryTool{service: svc},
	} {
		if err := registry.RegisterWithEntry(t, entry); err != nil {
			return err
		}
	}
	return nil
}

// --- Helpers ---

func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func invalidArgumentResult(call tools.ToolCall, message string) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  "Invalid tool arguments: " + message,
		ContentForUser: "Tham số tool không hợp lệ: " + message,
		Error: &tools.ToolError{
			Code:    tools.ErrorInvalidArgument,
			Message: message,
		},
	}
}
