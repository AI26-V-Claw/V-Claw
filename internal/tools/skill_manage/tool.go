package skill_manage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vclaw/internal/tools"
)

const ToolName = "skill_manage"

// SkillRecord represents a single auto-learned skill entry in manifest.json
type SkillRecord struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Version   string `json:"version"`
	Content   string `json:"content"` // SKILL.md content
}

// SkillManageTool handles create/patch/list of auto-learned skills in cache/skills/
type SkillManageTool struct {
	mu        sync.Mutex
	skillsDir string // default: cache/skills
}

func NewSkillManageTool(skillsDir string) *SkillManageTool {
	if strings.TrimSpace(skillsDir) == "" {
		skillsDir = "cache/skills"
	}
	return &SkillManageTool{skillsDir: skillsDir}
}

func (t *SkillManageTool) Name() string { return ToolName }

func (t *SkillManageTool) Description() string {
	return "Manage auto-learned skills: create a new skill, patch an existing one, or list all auto-learned skills. " +
		"Use create when a new repeatable workflow is detected. Use patch to improve an existing skill. " +
		"Use list to see what skills have been auto-learned so far."
}

func (t *SkillManageTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create", "patch", "list"},
				"description": "Action to perform: create a new skill, patch an existing one, or list all.",
			},
			"skill_name": map[string]any{
				"type":        "string",
				"description": "Skill identifier in snake_case with skill. prefix, e.g. skill.daily_briefing",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Full SKILL.md content for create or patch actions.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Brief reason why this skill is being created or patched.",
			},
		},
		"required":             []string{"action"},
		"additionalProperties": false,
	}
}

func (t *SkillManageTool) Capability() tools.Capability { return tools.CapabilityMutating }
func (t *SkillManageTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelLocalWrite }

func (t *SkillManageTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	action, _ := call.Arguments["action"].(string)
	switch strings.TrimSpace(action) {
	case "create":
		return t.handleCreate(call)
	case "patch":
		return t.handlePatch(call)
	case "list":
		return t.handleList(call)
	default:
		return tools.ToolResult{
			ToolCallID:     call.ID,
			ToolName:       call.Name,
			Success:        false,
			ContentForLLM:  "INVALID_ACTION: action must be one of: create, patch, list",
			ContentForUser: "Invalid action.",
			Error:          &tools.ToolError{Code: "INVALID_ACTION", Message: "action must be one of: create, patch, list"},
		}
	}
}

func (t *SkillManageTool) handleCreate(call tools.ToolCall) tools.ToolResult {
	name, content, reason, err := t.extractArgs(call)
	if err != nil {
		return t.errResult(call, "INVALID_INPUT", err.Error())
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	// Check if already exists
	existing, _ := t.loadRecord(name)
	if existing != nil {
		return t.errResult(call, "ALREADY_EXISTS",
			fmt.Sprintf("Skill %q already exists. Use patch to update it.", name))
	}

	record := SkillRecord{
		Name:      name,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Version:   "1.0.0",
		Content:   content,
	}
	if err := t.saveRecord(record); err != nil {
		return t.errResult(call, "WRITE_FAILED", fmt.Sprintf("failed to save skill: %v", err))
	}

	msg := fmt.Sprintf("Skill %q created. Reason: %s", name, reason)
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "SKILL_CREATED: " + msg,
		ContentForUser: msg,
		ArtifactRef: &tools.ToolArtifactRef{
			Kind:  "file",
			Label: name,
			URI:   filepath.Join(t.skillsDir, name, "SKILL.md"),
		},
	}
}

func (t *SkillManageTool) handlePatch(call tools.ToolCall) tools.ToolResult {
	name, content, reason, err := t.extractArgs(call)
	if err != nil {
		return t.errResult(call, "INVALID_INPUT", err.Error())
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	existing, err := t.loadRecord(name)
	if err != nil || existing == nil {
		return t.errResult(call, "NOT_FOUND",
			fmt.Sprintf("Skill %q not found. Use create to add it.", name))
	}

	existing.Content = content
	existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	// Bump patch version
	existing.Version = bumpPatchVersion(existing.Version)

	if err := t.saveRecord(*existing); err != nil {
		return t.errResult(call, "WRITE_FAILED", fmt.Sprintf("failed to patch skill: %v", err))
	}

	msg := fmt.Sprintf("Skill %q patched to version %s. Reason: %s", name, existing.Version, reason)
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "SKILL_PATCHED: " + msg,
		ContentForUser: msg,
	}
}

func (t *SkillManageTool) handleList(call tools.ToolCall) tools.ToolResult {
	t.mu.Lock()
	defer t.mu.Unlock()

	records, err := t.loadAllRecords()
	if err != nil {
		return t.errResult(call, "READ_FAILED", fmt.Sprintf("failed to list skills: %v", err))
	}
	if len(records) == 0 {
		return tools.ToolResult{
			ToolCallID:     call.ID,
			ToolName:       call.Name,
			Success:        true,
			ContentForLLM:  "No auto-learned skills yet.",
			ContentForUser: "No auto-learned skills yet.",
		}
	}

	var lines []string
	for _, r := range records {
		lines = append(lines, fmt.Sprintf("- %s (v%s, updated %s)", r.Name, r.Version, r.UpdatedAt))
	}
	result := fmt.Sprintf("Auto-learned skills (%d):\n%s", len(records), strings.Join(lines, "\n"))
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  result,
		ContentForUser: result,
	}
}

// --- Storage helpers ---

func (t *SkillManageTool) skillDir(name string) string {
	return filepath.Join(t.skillsDir, name)
}

func (t *SkillManageTool) skillMDPath(name string) string {
	return filepath.Join(t.skillDir(name), "SKILL.md")
}

func (t *SkillManageTool) manifestPath() string {
	return filepath.Join(t.skillsDir, "manifest.json")
}

func (t *SkillManageTool) saveRecord(record SkillRecord) error {
	dir := t.skillDir(record.Name)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("create skill dir: %w", err)
	}
	if err := os.WriteFile(t.skillMDPath(record.Name), []byte(record.Content), 0640); err != nil {
		return fmt.Errorf("write SKILL.md: %w", err)
	}
	return t.updateManifest(record)
}

func (t *SkillManageTool) loadRecord(name string) (*SkillRecord, error) {
	records, err := t.loadAllRecords()
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		if r.Name == name {
			return &r, nil
		}
	}
	return nil, nil
}

func (t *SkillManageTool) loadAllRecords() ([]SkillRecord, error) {
	data, err := os.ReadFile(t.manifestPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []SkillRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return records, nil
}

func (t *SkillManageTool) updateManifest(record SkillRecord) error {
	records, _ := t.loadAllRecords()
	updated := false
	for i, r := range records {
		if r.Name == record.Name {
			records[i] = record
			updated = true
			break
		}
	}
	if !updated {
		records = append(records, record)
	}
	if err := os.MkdirAll(t.skillsDir, 0750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(t.manifestPath(), data, 0640)
}

// --- Argument helpers ---

func (t *SkillManageTool) extractArgs(call tools.ToolCall) (name, content, reason string, err error) {
	name, _ = call.Arguments["skill_name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", "", fmt.Errorf("skill_name is required")
	}
	if !strings.HasPrefix(name, "skill.") {
		return "", "", "", fmt.Errorf("skill_name must start with 'skill.' prefix")
	}
	content, _ = call.Arguments["content"].(string)
	content = strings.TrimSpace(content)
	if content == "" {
		return "", "", "", fmt.Errorf("content is required")
	}
	reason, _ = call.Arguments["reason"].(string)
	if strings.TrimSpace(reason) == "" {
		reason = "auto-detected repeatable workflow"
	}
	return name, content, reason, nil
}

func (t *SkillManageTool) errResult(call tools.ToolCall, code, message string) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  code + ": " + message,
		ContentForUser: message,
		Error:          &tools.ToolError{Code: code, Message: message},
	}
}

// bumpPatchVersion increments the patch number of a semver string.
// "1.0.0" -> "1.0.1", falls back to "1.0.1" on parse error.
func bumpPatchVersion(version string) string {
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) != 3 {
		return "1.0.1"
	}
	patch := 0
	fmt.Sscanf(parts[2], "%d", &patch)
	return fmt.Sprintf("%s.%s.%d", parts[0], parts[1], patch+1)
}

// RegisterTools registers skill_manage tool into the given registry.
func RegisterTools(registry interface {
	RegisterWithEntry(tools.Tool, tools.ToolRegistryEntry) error
}, skillsDir string) error {
	t := NewSkillManageTool(skillsDir)
	return registry.RegisterWithEntry(t, tools.ToolRegistryEntry{
		Name:     ToolName,
		Group:    "skill_management",
		Owner:    "agent_core",
		Enabled:  true,
		Disabled: false,
	})
}
