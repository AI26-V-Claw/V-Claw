package policies

import (
	"fmt"
	"sync"

	"vclaw/internal/contracts"
)

type UserPolicyStore struct {
	mu   sync.RWMutex
	path string
	cfg  UserPolicyConfig
}

func NewUserPolicyStore(path string) (*UserPolicyStore, error) {
	cfg, err := LoadUserPolicyConfig(path)
	if err != nil {
		return nil, err
	}
	return &UserPolicyStore{
		path: path,
		cfg:  cfg,
	}, nil
}

func (s *UserPolicyStore) Path() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.path
}

func (s *UserPolicyStore) Snapshot() UserPolicyConfig {
	if s == nil {
		return UserPolicyConfig{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneUserPolicyConfig(s.cfg)
}

func (s *UserPolicyStore) Reload() (UserPolicyConfig, error) {
	if s == nil {
		return UserPolicyConfig{}, fmt.Errorf("user policy store is nil")
	}
	cfg, err := LoadUserPolicyConfig(s.Path())
	if err != nil {
		return UserPolicyConfig{}, err
	}
	s.mu.Lock()
	s.cfg = cloneUserPolicyConfig(cfg)
	s.mu.Unlock()
	return cloneUserPolicyConfig(cfg), nil
}

func cloneUserPolicyConfig(cfg UserPolicyConfig) UserPolicyConfig {
	return UserPolicyConfig{
		AutoAllow:       append([]contracts.RiskLevel(nil), cfg.AutoAllow...),
		RequireApproval: append([]contracts.RiskLevel(nil), cfg.RequireApproval...),
		AlwaysBlock:     append([]contracts.RiskLevel(nil), cfg.AlwaysBlock...),
	}
}
