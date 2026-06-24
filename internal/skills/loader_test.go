package skills_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"vclaw/internal/skills"
	"vclaw/internal/tools"
)

func TestLoadSkillsFromFile_FileNotExist(t *testing.T) {
	plugins, err := skills.LoadSkillsFromFile("/nonexistent/path/skills.json", nil)
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(plugins) != 0 {
		t.Fatalf("expected empty plugins for missing file, got %d", len(plugins))
	}
}

func TestLoadSkillsFromFile_EmptyPath(t *testing.T) {
	plugins, err := skills.LoadSkillsFromFile("", nil)
	if err != nil {
		t.Fatalf("expected no error for empty path, got: %v", err)
	}
	if plugins != nil {
		t.Fatalf("expected nil plugins for empty path, got %v", plugins)
	}
}

func TestLoadSkillsFromFile_ValidManifest(t *testing.T) {
	manifest := map[string]any{
		"skills": []map[string]any{
			{
				"name":        "skill.test_read",
				"description": "A test read skill",
				"scope":       []string{"email"},
				"permissions": []string{"gmail.read"},
				"fallback":    "Cannot handle this.",
				"risk_level":  "safe_read",
				"enabled":     true,
			},
			{
				"name":        "skill.test_disabled",
				"description": "A disabled skill",
				"risk_level":  "safe_read",
				"enabled":     false,
			},
		},
	}
	path := writeManifest(t, manifest)
	plugins, err := skills.LoadSkillsFromFile(path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(plugins))
	}
	def0 := plugins[0].Definition()
	if def0.Name != "skill.test_read" {
		t.Errorf("expected skill.test_read, got %q", def0.Name)
	}
	if !def0.Enabled {
		t.Errorf("expected skill.test_read to be enabled")
	}
	if len(def0.Scope) != 1 || def0.Scope[0] != "email" {
		t.Errorf("expected scope [email], got %v", def0.Scope)
	}
	def1 := plugins[1].Definition()
	if def1.Enabled {
		t.Errorf("expected skill.test_disabled to be disabled")
	}
}

func TestLoadSkillsFromFile_SkipsEmptyName(t *testing.T) {
	manifest := map[string]any{
		"skills": []map[string]any{
			{"name": "", "description": "no name", "enabled": true},
			{"name": "skill.valid", "description": "valid", "risk_level": "safe_read", "enabled": true},
		},
	}
	path := writeManifest(t, manifest)
	plugins, err := skills.LoadSkillsFromFile(path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin (empty name skipped), got %d", len(plugins))
	}
	if plugins[0].Definition().Name != "skill.valid" {
		t.Errorf("expected skill.valid, got %q", plugins[0].Definition().Name)
	}
}

func TestRegisterSkill_DisabledReturnsFallback(t *testing.T) {
	registry := tools.NewToolRegistry()
	manifest := map[string]any{
		"skills": []map[string]any{
			{
				"name":        "skill.disabled_demo",
				"description": "demo disabled",
				"fallback":    "Skill này đang tắt.",
				"risk_level":   "safe_read",
				"enabled":     false,
			},
		},
	}
	path := writeManifest(t, manifest)
	plugins, err := skills.LoadSkillsFromFile(path, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := skills.RegisterSkills(registry, plugins); err != nil {
		t.Fatalf("register: %v", err)
	}
	result := registry.Execute(context.Background(), tools.ToolCall{
		ID:   "tc-1",
		Name: "skill.disabled_demo",
	})
	if result.Success {
		t.Errorf("expected failure for disabled skill, got success")
	}
	if result.Error == nil || result.Error.Code != "SKILL_DISABLED" {
		t.Errorf("expected SKILL_DISABLED error, got %v", result.Error)
	}
}

func TestScopeAllowed(t *testing.T) {
	def := skills.SkillDefinition{Name: "skill.x", Scope: []string{"email", "calendar"}}
	if !skills.ScopeAllowed(def, "email") {
		t.Error("email should be allowed")
	}
	if !skills.ScopeAllowed(def, "EMAIL") {
		t.Error("EMAIL (uppercase) should be allowed")
	}
	if skills.ScopeAllowed(def, "drive") {
		t.Error("drive should not be allowed")
	}
	// empty scope = allow all
	defAll := skills.SkillDefinition{Name: "skill.all"}
	if !skills.ScopeAllowed(defAll, "anything") {
		t.Error("empty scope should allow all domains")
	}
}

func writeManifest(t *testing.T, data any) string {
	t.Helper()
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "skills.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}
