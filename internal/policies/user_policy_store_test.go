package policies

import (
	"path/filepath"
	"testing"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/tools"
)

func TestUserPolicyStoreReloadUpdatesToolPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user-policy.json")
	if err := SaveUserPolicyConfig(path, UserPolicyConfig{
		AutoAllow: []contracts.RiskLevel{contracts.RiskLevelSafeRead},
	}); err != nil {
		t.Fatalf("save initial policy: %v", err)
	}

	store, err := NewUserPolicyStore(path)
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	policy := NewToolPolicyWithStore(store)
	allow := policy.DecideToolCall("call_1", tools.ToolDefinition{
		Name:       "gmail.listEmails",
		Capability: tools.CapabilityReadOnly,
		RiskLevel:  tools.RiskLevelSafeRead,
		Enabled:    true,
	}, true, time.Now())
	if allow.Decision != contracts.RiskDecisionAllow {
		t.Fatalf("expected allow from initial policy, got %#v", allow)
	}

	if err := SaveUserPolicyConfig(path, UserPolicyConfig{
		AlwaysBlock: []contracts.RiskLevel{contracts.RiskLevelSafeRead},
	}); err != nil {
		t.Fatalf("save updated policy: %v", err)
	}
	if _, err := store.Reload(); err != nil {
		t.Fatalf("reload store: %v", err)
	}

	block := policy.DecideToolCall("call_2", tools.ToolDefinition{
		Name:       "gmail.listEmails",
		Capability: tools.CapabilityReadOnly,
		RiskLevel:  tools.RiskLevelSafeRead,
		Enabled:    true,
	}, true, time.Now())
	if block.Decision != contracts.RiskDecisionBlock {
		t.Fatalf("expected block after reload, got %#v", block)
	}
}
