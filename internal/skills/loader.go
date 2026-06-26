package skills

import (
	"encoding/json"
	"fmt"
	"context"
	"log/slog"
	"os"
	"strings"

	"vclaw/internal/tools"
)

// SkillManifestEntry là cấu trúc JSON cho mỗi skill trong file manifest.
type SkillManifestEntry struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Scope       []string         `json:"scope"`
	Permissions []string         `json:"permissions"`
	Fallback    string           `json:"fallback"`
	Parameters  tools.ToolSchema `json:"parameters"`
	RiskLevel   string           `json:"risk_level"`
	Enabled     bool             `json:"enabled"`
}

// SkillManifest là cấu trúc của file configs/skills.json.
type SkillManifest struct {
	Skills []SkillManifestEntry `json:"skills"`
}

// manifestPlugin là một SkillPlugin được load từ manifest (không có Execute logic thật).
// Dùng cho demo: Execute luôn trả fallback, thể hiện skill đã đăng ký nhưng chưa có impl.
type manifestPlugin struct {
	def SkillDefinition
}

func (p *manifestPlugin) Definition() SkillDefinition { return p.def }

func (p *manifestPlugin) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	return FallbackResult(call, p.def)
}

// LoadSkillsFromFile đọc manifest JSON tại path và trả về danh sách SkillPlugin.
// Nếu file không tồn tại, trả về danh sách rỗng (không lỗi).
// Nếu file tồn tại nhưng parse lỗi, trả về lỗi.
func LoadSkillsFromFile(path string, logger *slog.Logger) ([]SkillPlugin, error) {
	if logger == nil {
		logger = slog.Default()
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		logger.Info("skills manifest not found, skipping", "path", path)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read skills manifest %q: %w", path, err)
	}
	var manifest SkillManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse skills manifest %q: %w", path, err)
	}
	plugins := make([]SkillPlugin, 0, len(manifest.Skills))
	for _, entry := range manifest.Skills {
		if strings.TrimSpace(entry.Name) == "" {
			logger.Warn("skills manifest: skipping entry with empty name")
			continue
		}
		riskLevel := tools.RiskLevel(strings.TrimSpace(entry.RiskLevel))
		if riskLevel == "" {
			riskLevel = tools.RiskLevelSafeRead
		}
		def := SkillDefinition{
			Name:        entry.Name,
			Description: entry.Description,
			Scope:       entry.Scope,
			Permissions: entry.Permissions,
			Fallback:    entry.Fallback,
			Parameters:  entry.Parameters,
			RiskLevel:   riskLevel,
			Enabled:     entry.Enabled,
		}
		plugins = append(plugins, &manifestPlugin{def: def})
		logger.Info("loaded skill from manifest", "name", def.Name, "enabled", def.Enabled)
	}
	return plugins, nil
}

// RegisterSkillsFromFile load và register tất cả skills từ file manifest vào registry.
// Không lỗi nếu file không tồn tại.
func RegisterSkillsFromFile(registry *tools.ToolRegistry, path string, logger *slog.Logger) error {
	plugins, err := LoadSkillsFromFile(path, logger)
	if err != nil {
		return err
	}
	return RegisterSkills(registry, plugins)
}

// CacheSkillRecord mirrors skill_manage.SkillRecord for loading auto-learned skills.
// Defined here to avoid circular import with tools/skill_manage package.
type CacheSkillRecord struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Content string `json:"content"`
	Enabled bool   `json:"enabled"`
}

// LoadSkillsFromCacheDir loads auto-learned skills from cache/skills/manifest.json.
// Returns empty list (no error) if the directory or manifest does not exist.
func LoadSkillsFromCacheDir(cacheDir string, logger *slog.Logger) ([]SkillPlugin, error) {
	if logger == nil {
		logger = slog.Default()
	}
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		return nil, nil
	}
	manifestPath := cacheDir + "/manifest.json"
	data, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read cache skills manifest %q: %w", manifestPath, err)
	}
	var records []CacheSkillRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parse cache skills manifest: %w", err)
	}
	plugins := make([]SkillPlugin, 0, len(records))
	for _, r := range records {
		if strings.TrimSpace(r.Name) == "" {
			continue
		}
		def := SkillDefinition{
			Name:        r.Name,
			Version:     r.Version,
			Description: extractDescriptionFromContent(r.Content),
			Fallback:    "Auto-learned skill " + r.Name + " could not handle this request.",
			RiskLevel:   tools.RiskLevelSafeCompute,
			Enabled:     true,
		}
		plugins = append(plugins, &manifestPlugin{def: def})
		logger.Info("loaded auto-learned skill from cache", "name", def.Name, "version", r.Version)
	}
	return plugins, nil
}

// RegisterSkillsFromCacheDir loads and registers auto-learned skills from cache dir.
func RegisterSkillsFromCacheDir(registry *tools.ToolRegistry, cacheDir string, logger *slog.Logger) error {
	plugins, err := LoadSkillsFromCacheDir(cacheDir, logger)
	if err != nil {
		return err
	}
	return RegisterSkills(registry, plugins)
}

// extractDescriptionFromContent tries to extract the description line from SKILL.md content.
// Looks for "description:" in YAML frontmatter. Returns empty string if not found.
func extractDescriptionFromContent(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "description:") {
			desc := strings.TrimPrefix(line, "description:")
			desc = strings.Trim(strings.TrimSpace(desc), `"`)
			return desc
		}
	}
	return ""
}
