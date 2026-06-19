package policies

import (
	"strings"
	"testing"

	"vclaw/internal/contracts"
)

func TestPolicyConfigFromAssignmentsRejectsDestructiveAutoAllow(t *testing.T) {
	assignments := EffectivePolicyAssignments(UserPolicyConfig{})
	assignments[contracts.RiskLevelDestructive] = PolicyGroupAutoAllow

	if _, err := PolicyConfigFromAssignments(assignments); err == nil || !strings.Contains(err.Error(), "destructive cannot be auto_allowed") {
		t.Fatalf("expected destructive auto_allow validation error, got %v", err)
	}
}

func TestPolicyAssignmentsSummaryUsesCurrentGroups(t *testing.T) {
	assignments := map[contracts.RiskLevel]PolicyGroup{
		contracts.RiskLevelSafeRead:      PolicyGroupAutoAllow,
		contracts.RiskLevelSafeCompute:   PolicyGroupAutoAllow,
		contracts.RiskLevelSensitiveRead: PolicyGroupRequireApprove,
		contracts.RiskLevelExternalWrite: PolicyGroupRequireApprove,
		contracts.RiskLevelLocalWrite:    PolicyGroupRequireApprove,
		contracts.RiskLevelCodeExecution: PolicyGroupAlwaysBlock,
		contracts.RiskLevelDestructive:   PolicyGroupAlwaysBlock,
	}
	summary := PolicyAssignmentsSummary(assignments)
	for _, want := range []string{
		"Tự động cho phép",
		"Cần phê duyệt",
		"Luôn chặn",
		"Xem danh sách & thông tin tổng quan",
		"Tóm tắt, phân tích nội dung",
		"Chạy lệnh hệ thống",
		"Xóa dữ liệu",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("expected summary to contain %q, got %q", want, summary)
		}
	}
	for _, want := range []string{"safe_read", "safe_compute", "code_execution", "destructive", "auto_allow", "require_approval", "always_block"} {
		if strings.Contains(summary, want) {
			t.Fatalf("expected summary to hide technical key %q, got %q", want, summary)
		}
	}
}
