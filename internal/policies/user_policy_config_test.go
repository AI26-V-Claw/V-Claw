package policies

import (
	"path/filepath"
	"testing"

	"vclaw/internal/contracts"
)

func TestUserPolicyConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user-policy.json")
	cfg := UserPolicyConfig{
		AutoAllow:       []contracts.RiskLevel{contracts.RiskLevelSafeRead, contracts.RiskLevelSafeCompute},
		RequireApproval: []contracts.RiskLevel{contracts.RiskLevelExternalWrite},
		AlwaysBlock:     []contracts.RiskLevel{contracts.RiskLevelDestructive},
	}

	if err := SaveUserPolicyConfig(path, cfg); err != nil {
		t.Fatalf("save user policy config: %v", err)
	}

	loaded, err := LoadUserPolicyConfig(path)
	if err != nil {
		t.Fatalf("load user policy config: %v", err)
	}
	if len(loaded.AutoAllow) != 2 || len(loaded.RequireApproval) != 1 || len(loaded.AlwaysBlock) != 1 {
		t.Fatalf("unexpected loaded config: %#v", loaded)
	}
}

func TestLoadUserPolicyConfigMissingFileReturnsDefault(t *testing.T) {
	cfg, err := LoadUserPolicyConfig(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("load missing user policy config: %v", err)
	}
	if len(cfg.AutoAllow) != 0 || len(cfg.RequireApproval) != 0 || len(cfg.AlwaysBlock) != 0 {
		t.Fatalf("expected empty default config, got %#v", cfg)
	}
}
