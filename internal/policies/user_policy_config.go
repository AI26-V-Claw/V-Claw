package policies

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vclaw/internal/contracts"
)

type UserPolicyConfig struct {
	AutoAllow       []contracts.RiskLevel `json:"auto_allow,omitempty"`
	RequireApproval []contracts.RiskLevel `json:"require_approval,omitempty"`
	AlwaysBlock     []contracts.RiskLevel `json:"always_block,omitempty"`
}

func DefaultUserPolicyPath(dataDir string) string {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		dataDir = "./data"
	}
	return filepath.Join(dataDir, "user-policy.json")
}

func LoadUserPolicyConfig(path string) (UserPolicyConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return UserPolicyConfig{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return UserPolicyConfig{}, nil
		}
		return UserPolicyConfig{}, fmt.Errorf("read user policy config: %w", err)
	}
	var cfg UserPolicyConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return UserPolicyConfig{}, fmt.Errorf("decode user policy config: %w", err)
	}
	return normalizeUserPolicyConfig(cfg)
}

func SaveUserPolicyConfig(path string, cfg UserPolicyConfig) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("user policy config path is required")
	}
	normalized, err := normalizeUserPolicyConfig(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create user policy config directory: %w", err)
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return fmt.Errorf("encode user policy config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write user policy config: %w", err)
	}
	return nil
}

func normalizeUserPolicyConfig(cfg UserPolicyConfig) (UserPolicyConfig, error) {
	autoAllow, err := normalizeRiskLevels(cfg.AutoAllow)
	if err != nil {
		return UserPolicyConfig{}, fmt.Errorf("autoAllow: %w", err)
	}
	requireApproval, err := normalizeRiskLevels(cfg.RequireApproval)
	if err != nil {
		return UserPolicyConfig{}, fmt.Errorf("requireApproval: %w", err)
	}
	alwaysBlock, err := normalizeRiskLevels(cfg.AlwaysBlock)
	if err != nil {
		return UserPolicyConfig{}, fmt.Errorf("alwaysBlock: %w", err)
	}
	return UserPolicyConfig{
		AutoAllow:       autoAllow,
		RequireApproval: requireApproval,
		AlwaysBlock:     alwaysBlock,
	}, nil
}

func normalizeRiskLevels(levels []contracts.RiskLevel) ([]contracts.RiskLevel, error) {
	seen := make(map[contracts.RiskLevel]struct{}, len(levels))
	result := make([]contracts.RiskLevel, 0, len(levels))
	for _, level := range levels {
		level = contracts.RiskLevel(strings.TrimSpace(string(level)))
		if level == "" {
			continue
		}
		if !isKnownRiskLevel(level) {
			return nil, fmt.Errorf("unknown risk level %q", level)
		}
		if _, ok := seen[level]; ok {
			continue
		}
		seen[level] = struct{}{}
		result = append(result, level)
	}
	return result, nil
}

func isKnownRiskLevel(level contracts.RiskLevel) bool {
	switch level {
	case contracts.RiskLevelSafeRead,
		contracts.RiskLevelSafeCompute,
		contracts.RiskLevelSensitiveRead,
		contracts.RiskLevelExternalWrite,
		contracts.RiskLevelLocalWrite,
		contracts.RiskLevelCodeExecution,
		contracts.RiskLevelDestructive:
		return true
	default:
		return false
	}
}

func containsRiskLevel(level contracts.RiskLevel, levels []contracts.RiskLevel) bool {
	for _, candidate := range levels {
		if candidate == level {
			return true
		}
	}
	return false
}
