package longmem

import (
	"strings"
	"testing"
)

func TestExtractiveEmail(t *testing.T) {
	summary := "Người dùng làm việc với Bao: bao@vclaw.com hôm nay."
	facts := extractiveFallback(summary)
	found := containsAny(facts, "bao@vclaw.com")
	if !found {
		t.Errorf("email fact not extracted, got: %v", facts)
	}
}

func TestExtractiveTimezone(t *testing.T) {
	summary := "timezone: Asia/Ho_Chi_Minh được cấu hình."
	facts := extractiveFallback(summary)
	found := containsAny(facts, "Ho_Chi_Minh")
	if !found {
		t.Errorf("timezone fact not extracted, got: %v", facts)
	}
}

func TestExtractiveNameFact(t *testing.T) {
	summary := "tên: Quang Ho đã đăng nhập."
	facts := extractiveFallback(summary)
	found := containsAny(facts, "Quang Ho")
	if !found {
		t.Errorf("name fact not extracted, got: %v", facts)
	}
}

func TestExtractivePreference(t *testing.T) {
	summary := "Người dùng thích họp ngắn dưới 30 phút."
	facts := extractiveFallback(summary)
	found := containsAny(facts, "thích")
	if !found {
		t.Errorf("preference fact not extracted, got: %v", facts)
	}
}

func TestExtractiveNoDuplicates(t *testing.T) {
	// Same name+email appears twice — should produce exactly one fact.
	summary := "Bao: bao@vclaw.com là contact chính. Bao: bao@vclaw.com xuất hiện lại."
	facts := extractiveFallback(summary)
	count := 0
	for _, f := range facts {
		if strings.Contains(f, "bao@vclaw.com") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 email fact, got %d: %v", count, facts)
	}
}

func TestExtractiveEmptySummary(t *testing.T) {
	facts := extractiveFallback("")
	if len(facts) != 0 {
		t.Errorf("expected no facts for empty summary, got: %v", facts)
	}
}

// containsAny reports whether any fact in facts contains substr.
func containsAny(facts []string, substr string) bool {
	for _, f := range facts {
		if strings.Contains(f, substr) {
			return true
		}
	}
	return false
}
