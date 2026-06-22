package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vclaw/internal/audit"
	"vclaw/internal/tools"
)

func TestGetUserMemory_Empty(t *testing.T) {
	svc := NewService(t.TempDir())
	tool := &getUserMemoryTool{service: svc}

	call := tools.ToolCall{ID: "call_1", Name: ToolNameGetUserMemory}
	result := tool.Execute(context.Background(), call)

	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if result.ContentForLLM != "Bộ nhớ trống — chưa có dữ liệu nào." {
		t.Errorf("unexpected content: %s", result.ContentForLLM)
	}
}

func TestGetUserMemory_WithData(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "USER.md"), "# Thông tin người dùng\n\n## Thông tin cơ bản\n- Email: test@example.com\n")
	writeFile(t, filepath.Join(dir, "NOTES.md"), "# Ghi chú gần đây\n- Fact 1\n")

	svc := NewService(dir)
	tool := &getUserMemoryTool{service: svc}

	call := tools.ToolCall{ID: "call_1", Name: ToolNameGetUserMemory}
	result := tool.Execute(context.Background(), call)

	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if !contains(result.ContentForLLM, "test@example.com") {
		t.Errorf("expected email in output, got: %s", result.ContentForLLM)
	}
	if !contains(result.ContentForLLM, "Fact 1") {
		t.Errorf("expected note in output, got: %s", result.ContentForLLM)
	}
}

func TestEditUserMemory_AddUserFact(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(dir)
	tool := &editUserMemoryTool{service: svc}

	call := tools.ToolCall{
		ID:   "call_1",
		Name: ToolNameEditUserMemory,
		Arguments: map[string]any{
			"action":   "add",
			"target":   "user",
			"content":  "Số điện thoại: 0901234567",
			"category": "Thông tin cơ bản",
		},
	}
	result := tool.Execute(context.Background(), call)

	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if !contains(result.ContentForLLM, "Đã thêm fact") {
		t.Errorf("unexpected content: %s", result.ContentForLLM)
	}

	// Verify file was updated.
	content := readFile(t, filepath.Join(dir, "USER.md"))
	if !contains(content, "0901234567") {
		t.Errorf("expected fact in USER.md, got:\n%s", content)
	}
}

func TestEditUserMemory_AddNotesFact(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(dir)
	tool := &editUserMemoryTool{service: svc}

	call := tools.ToolCall{
		ID:   "call_1",
		Name: ToolNameEditUserMemory,
		Arguments: map[string]any{
			"action":  "add",
			"target":  "notes",
			"content": "Đang làm dự án V-Claw",
		},
	}
	result := tool.Execute(context.Background(), call)

	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}

	content := readFile(t, filepath.Join(dir, "NOTES.md"))
	if !contains(content, "Đang làm dự án V-Claw") {
		t.Errorf("expected fact in NOTES.md, got:\n%s", content)
	}
}

func TestEditUserMemory_RemoveUserFact(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "USER.md"), "# Thông tin người dùng\n\n## Thông tin cơ bản\n- Email: old@example.com\n- Phone: 090123\n")

	svc := NewService(dir)
	tool := &editUserMemoryTool{service: svc}

	call := tools.ToolCall{
		ID:   "call_1",
		Name: ToolNameEditUserMemory,
		Arguments: map[string]any{
			"action":  "remove",
			"target":  "user",
			"content": "old@example.com",
		},
	}
	result := tool.Execute(context.Background(), call)

	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if !contains(result.ContentForLLM, "Đã xóa") {
		t.Errorf("expected delete confirmation, got: %s", result.ContentForLLM)
	}

	content := readFile(t, filepath.Join(dir, "USER.md"))
	if contains(content, "old@example.com") {
		t.Errorf("expected fact removed, got:\n%s", content)
	}
	if !contains(content, "090123") {
		t.Errorf("expected other facts to remain, got:\n%s", content)
	}
}

func TestEditUserMemory_RemoveNotFound(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "USER.md"), "# Thông tin người dùng\n\n## Thông tin cơ bản\n- Email: keep@example.com\n")

	svc := NewService(dir)
	tool := &editUserMemoryTool{service: svc}

	call := tools.ToolCall{
		ID:   "call_1",
		Name: ToolNameEditUserMemory,
		Arguments: map[string]any{
			"action":  "remove",
			"target":  "user",
			"content": "nonexistent",
		},
	}
	result := tool.Execute(context.Background(), call)

	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if !contains(result.ContentForLLM, "Không tìm thấy") {
		t.Errorf("expected not-found message, got: %s", result.ContentForLLM)
	}
}

func TestEditUserMemory_InvalidAction(t *testing.T) {
	svc := NewService(t.TempDir())
	tool := &editUserMemoryTool{service: svc}

	call := tools.ToolCall{
		ID:   "call_1",
		Name: ToolNameEditUserMemory,
		Arguments: map[string]any{
			"action":  "delete",
			"target":  "user",
			"content": "something",
		},
	}
	result := tool.Execute(context.Background(), call)

	if result.Success {
		t.Fatal("expected failure for invalid action")
	}
	if result.Error == nil || result.Error.Code != tools.ErrorInvalidArgument {
		t.Errorf("expected ErrorInvalidArgument, got: %v", result.Error)
	}
}

func TestEditUserMemory_MissingCategory(t *testing.T) {
	svc := NewService(t.TempDir())
	tool := &editUserMemoryTool{service: svc}

	call := tools.ToolCall{
		ID:   "call_1",
		Name: ToolNameEditUserMemory,
		Arguments: map[string]any{
			"action":  "add",
			"target":  "user",
			"content": "some fact",
		},
	}
	result := tool.Execute(context.Background(), call)

	if result.Success {
		t.Fatal("expected failure for missing category")
	}
	if result.Error == nil || !contains(result.Error.Message, "category") {
		t.Errorf("expected category error, got: %v", result.Error)
	}
}

func TestResetMemory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "USER.md"), "# old\n- fact\n")
	writeFile(t, filepath.Join(dir, "NOTES.md"), "# old notes\n- note\n")

	svc := NewService(dir)
	tool := &resetMemoryTool{service: svc}

	call := tools.ToolCall{ID: "call_1", Name: ToolNameResetMemory}
	result := tool.Execute(context.Background(), call)

	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if !contains(result.ContentForLLM, "Đã xóa toàn bộ") {
		t.Errorf("unexpected content: %s", result.ContentForLLM)
	}

	// Verify skeleton was recreated.
	userMD := readFile(t, filepath.Join(dir, "USER.md"))
	if !contains(userMD, "Thông tin cơ bản") {
		t.Errorf("expected skeleton USER.md, got:\n%s", userMD)
	}
	notesMD := readFile(t, filepath.Join(dir, "NOTES.md"))
	if !contains(notesMD, "Ghi chú gần đây") {
		t.Errorf("expected skeleton NOTES.md, got:\n%s", notesMD)
	}
}

func TestRegisterTools(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry, t.TempDir(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, entry := range RegistryEntries {
		def, ok := registry.GetDefinition(entry.Name)
		if !ok {
			t.Errorf("tool %s not registered", entry.Name)
			continue
		}
		if def.Capability != entry.Capability {
			t.Errorf("tool %s: expected capability %s, got %s", entry.Name, entry.Capability, def.Capability)
		}
		if def.RiskLevel != entry.RiskLevel {
			t.Errorf("tool %s: expected risk %s, got %s", entry.Name, entry.RiskLevel, def.RiskLevel)
		}
		if def.RequiresApproval != entry.RequiresApproval {
			t.Errorf("tool %s: expected approval=%v, got %v", entry.Name, entry.RequiresApproval, def.RequiresApproval)
		}
	}
}

func TestAuditLogger_Called(t *testing.T) {
	dir := t.TempDir()
	logger := audit.NewMemoryLogger()
	svc := NewService(dir).WithAuditLogger(logger)
	tool := &editUserMemoryTool{service: svc}

	call := tools.ToolCall{
		ID:   "call_1",
		Name: ToolNameEditUserMemory,
		Arguments: map[string]any{
			"action":   "add",
			"target":   "user",
			"content":  "Email: audit@test.com",
			"category": "Thông tin cơ bản",
		},
	}
	tool.Execute(context.Background(), call)

	events, err := logger.Query(audit.Filter{})
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected audit event to be logged")
	}
	if events[0].Tool != ToolNameEditUserMemory {
		t.Errorf("expected tool %s, got %s", ToolNameEditUserMemory, events[0].Tool)
	}
	if events[0].Status != audit.StatusExecuted {
		t.Errorf("expected status executed, got %s", events[0].Status)
	}
}

// --- helpers ---

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readFile %s: %v", path, err)
	}
	return string(data)
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
