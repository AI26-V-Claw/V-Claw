package policies

import (
	"fmt"
	"strings"

	"vclaw/internal/contracts"
)

type PolicyGroup string

const (
	PolicyGroupAutoAllow      PolicyGroup = "auto_allow"
	PolicyGroupRequireApprove PolicyGroup = "require_approval"
	PolicyGroupAlwaysBlock    PolicyGroup = "always_block"
)

var policyRiskLevelOrder = []contracts.RiskLevel{
	contracts.RiskLevelSafeRead,
	contracts.RiskLevelSafeCompute,
	contracts.RiskLevelSensitiveRead,
	contracts.RiskLevelExternalWrite,
	contracts.RiskLevelLocalWrite,
	contracts.RiskLevelCodeExecution,
	contracts.RiskLevelDestructive,
}

var defaultPolicyGroupByRiskLevel = map[contracts.RiskLevel]PolicyGroup{
	contracts.RiskLevelSafeRead:      PolicyGroupAutoAllow,
	contracts.RiskLevelSafeCompute:   PolicyGroupAutoAllow,
	contracts.RiskLevelSensitiveRead: PolicyGroupRequireApprove,
	contracts.RiskLevelExternalWrite: PolicyGroupRequireApprove,
	contracts.RiskLevelLocalWrite:    PolicyGroupRequireApprove,
	contracts.RiskLevelCodeExecution: PolicyGroupRequireApprove,
	contracts.RiskLevelDestructive:   PolicyGroupAlwaysBlock,
}

func RiskLevelOrder() []contracts.RiskLevel {
	return append([]contracts.RiskLevel(nil), policyRiskLevelOrder...)
}

func PolicyGroupOrder() []PolicyGroup {
	return []PolicyGroup{
		PolicyGroupAutoAllow,
		PolicyGroupRequireApprove,
		PolicyGroupAlwaysBlock,
	}
}

func PolicyGroupLabel(group PolicyGroup) string {
	switch group {
	case PolicyGroupAutoAllow:
		return "Tự động cho phép"
	case PolicyGroupRequireApprove:
		return "Cần phê duyệt"
	case PolicyGroupAlwaysBlock:
		return "Luôn chặn"
	default:
		return string(group)
	}
}

func RiskLevelLabel(level contracts.RiskLevel) string {
	switch level {
	case contracts.RiskLevelSafeRead:
		return "Đọc thông tin cơ bản"
	case contracts.RiskLevelSafeCompute:
		return "Xử lý nội bộ"
	case contracts.RiskLevelSensitiveRead:
		return "Đọc nội dung riêng tư"
	case contracts.RiskLevelExternalWrite:
		return "Gửi hoặc tạo nội dung"
	case contracts.RiskLevelLocalWrite:
		return "Ghi file xuống máy"
	case contracts.RiskLevelCodeExecution:
		return "Chạy lệnh hoặc script"
	case contracts.RiskLevelDestructive:
		return "Xóa vĩnh viễn"
	default:
		return string(level)
	}
}

func PolicyGroupNext(group PolicyGroup) PolicyGroup {
	switch group {
	case PolicyGroupAutoAllow:
		return PolicyGroupRequireApprove
	case PolicyGroupRequireApprove:
		return PolicyGroupAlwaysBlock
	case PolicyGroupAlwaysBlock:
		return PolicyGroupAutoAllow
	default:
		return PolicyGroupRequireApprove
	}
}

func PolicyGroupFromRiskLevel(level contracts.RiskLevel) PolicyGroup {
	if group, ok := defaultPolicyGroupByRiskLevel[level]; ok {
		return group
	}
	return PolicyGroupRequireApprove
}

func EffectivePolicyAssignments(cfg UserPolicyConfig) map[contracts.RiskLevel]PolicyGroup {
	assignments := make(map[contracts.RiskLevel]PolicyGroup, len(policyRiskLevelOrder))
	for _, level := range policyRiskLevelOrder {
		assignments[level] = PolicyGroupFromRiskLevel(level)
	}
	applyAssignments(assignments, PolicyGroupAutoAllow, cfg.AutoAllow)
	applyAssignments(assignments, PolicyGroupRequireApprove, cfg.RequireApproval)
	applyAssignments(assignments, PolicyGroupAlwaysBlock, cfg.AlwaysBlock)
	return assignments
}

func PolicyConfigFromAssignments(assignments map[contracts.RiskLevel]PolicyGroup) (UserPolicyConfig, error) {
	normalized, err := normalizePolicyAssignments(assignments)
	if err != nil {
		return UserPolicyConfig{}, err
	}
	return UserPolicyConfig{
		AutoAllow:       levelsForGroup(normalized, PolicyGroupAutoAllow),
		RequireApproval: levelsForGroup(normalized, PolicyGroupRequireApprove),
		AlwaysBlock:     levelsForGroup(normalized, PolicyGroupAlwaysBlock),
	}, nil
}

func EffectivePolicyConfig(cfg UserPolicyConfig) UserPolicyConfig {
	assignments := EffectivePolicyAssignments(cfg)
	normalized, err := PolicyConfigFromAssignments(assignments)
	if err != nil {
		return UserPolicyConfig{}
	}
	return normalized
}

func PolicyChangesSummary(before, after UserPolicyConfig) string {
	beforeAssignments := EffectivePolicyAssignments(before)
	afterAssignments := EffectivePolicyAssignments(after)

	var changes []string
	for _, level := range policyRiskLevelOrder {
		beforeGroup := beforeAssignments[level]
		afterGroup := afterAssignments[level]
		if beforeGroup == afterGroup {
			continue
		}
		changes = append(changes, fmt.Sprintf("%s: %s → %s", RiskLevelLabel(level), PolicyGroupLabel(beforeGroup), PolicyGroupLabel(afterGroup)))
	}
	if len(changes) == 0 {
		return "Chính sách không thay đổi."
	}
	return "Đã cập nhật chính sách:\n- " + strings.Join(changes, "\n- ")
}

func PolicySummary(cfg UserPolicyConfig) string {
	cfg = EffectivePolicyConfig(cfg)
	return PolicyAssignmentsSummary(EffectivePolicyAssignments(cfg))
}

func PolicyAssignmentsSummary(assignments map[contracts.RiskLevel]PolicyGroup) string {
	return fmt.Sprintf(
		"%s: %s\n%s: %s\n%s: %s",
		PolicyGroupLabel(PolicyGroupAutoAllow),
		formatRiskLevelsByGroup(assignments, PolicyGroupAutoAllow),
		PolicyGroupLabel(PolicyGroupRequireApprove),
		formatRiskLevelsByGroup(assignments, PolicyGroupRequireApprove),
		PolicyGroupLabel(PolicyGroupAlwaysBlock),
		formatRiskLevelsByGroup(assignments, PolicyGroupAlwaysBlock),
	)
}

func normalizePolicyAssignments(assignments map[contracts.RiskLevel]PolicyGroup) (map[contracts.RiskLevel]PolicyGroup, error) {
	if len(assignments) == 0 {
		return nil, fmt.Errorf("policy assignments are required")
	}
	normalized := make(map[contracts.RiskLevel]PolicyGroup, len(policyRiskLevelOrder))
	for _, level := range policyRiskLevelOrder {
		group, ok := assignments[level]
		if !ok {
			return nil, fmt.Errorf("missing policy assignment for %s", level)
		}
		switch group {
		case PolicyGroupAutoAllow, PolicyGroupRequireApprove, PolicyGroupAlwaysBlock:
		default:
			return nil, fmt.Errorf("unknown policy group %q for %s", group, level)
		}
		if level == contracts.RiskLevelDestructive && group == PolicyGroupAutoAllow {
			return nil, fmt.Errorf("destructive cannot be auto_allowed")
		}
		normalized[level] = group
	}
	return normalized, nil
}

func applyAssignments(assignments map[contracts.RiskLevel]PolicyGroup, group PolicyGroup, levels []contracts.RiskLevel) {
	for _, level := range levels {
		level = contracts.RiskLevel(strings.TrimSpace(string(level)))
		if level == "" {
			continue
		}
		assignments[level] = group
	}
}

func levelsForGroup(assignments map[contracts.RiskLevel]PolicyGroup, group PolicyGroup) []contracts.RiskLevel {
	levels := make([]contracts.RiskLevel, 0, len(policyRiskLevelOrder))
	for _, level := range policyRiskLevelOrder {
		if assignments[level] == group {
			levels = append(levels, level)
		}
	}
	return levels
}

func formatRiskLevels(levels []contracts.RiskLevel) string {
	if len(levels) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(levels))
	for _, level := range levels {
		parts = append(parts, RiskLevelLabel(level))
	}
	return strings.Join(parts, ", ")
}

func formatRiskLevelsByGroup(assignments map[contracts.RiskLevel]PolicyGroup, group PolicyGroup) string {
	levels := make([]contracts.RiskLevel, 0, len(policyRiskLevelOrder))
	for _, level := range policyRiskLevelOrder {
		if assignments[level] == group {
			levels = append(levels, level)
		}
	}
	return formatRiskLevels(levels)
}
