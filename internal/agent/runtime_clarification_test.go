package agent

import (
	"testing"
	"time"

	"vclaw/internal/providers"
	"vclaw/internal/sessions"
)

func TestShouldRedirectClarifyToDriveMove(t *testing.T) {
	cases := []struct {
		request  string
		evidence string
		want     bool
	}{
		{`di chuyển file docs "Thuật toán binary search" vào folder "Nhập môn lập trình"`, ``, true},
		{`chuyển tài liệu này sang thư mục Projects`, ``, true},
		{`move file "report.xlsx" into folder "Archive"`, ``, true},
		{`vào thư mục mới`, ``, true},
		// Once NEEDS_DRIVE_MOVE_RESOLUTION is already in the transcript, do NOT redirect again.
		{`di chuyển file docs "Thuật toán binary search" vào folder "Nhập môn lập trình"`, `NEEDS_DRIVE_MOVE_RESOLUTION: The current request...`, false},
		{`đổi tên file thành "Binary search"`, ``, false},
		{`tạo file mới trong Drive`, ``, false},
		{``, ``, false},
	}
	for _, tc := range cases {
		got := shouldRedirectClarifyToDriveMove(tc.request, tc.evidence)
		if got != tc.want {
			t.Errorf("shouldRedirectClarifyToDriveMove(%q, evidence) = %v, want %v", tc.request, got, tc.want)
		}
	}
}

func TestShouldRerouteDriveMetadataMove(t *testing.T) {
	call := providers.ToolCall{Name: "drive.updateFileMetadata"}
	if !shouldRerouteDriveMetadataMove(call, `di chuyển file docs "Thuật toán binary search" vào folder "Nhập môn lập trình"`) {
		t.Fatal("expected Drive move request through update metadata to be rerouted")
	}
	if shouldRerouteDriveMetadataMove(call, `đổi tên file thành "Thuật toán binary search"`) {
		t.Fatal("rename metadata request should not be rerouted")
	}
	if shouldRerouteDriveMetadataMove(providers.ToolCall{Name: "drive.moveFile"}, `di chuyển file`) {
		t.Fatal("correct move tool should not be rerouted")
	}
}

func TestShouldResolveDriveMoveBeforeClarification(t *testing.T) {
	call := providers.ToolCall{
		Name: "drive.moveFile",
		Arguments: map[string]any{
			"fileId": "doc_123",
		},
	}
	request := `di chuyển file docs "Thuật toán binary search" vào folder "Nhập môn lập trình"`
	if !shouldResolveDriveMoveBeforeClarification(call, request, []string{"targetParentId"}) {
		t.Fatal("expected Drive move with named destination folder to resolve targetParentId before asking the user")
	}
	if shouldResolveDriveMoveBeforeClarification(call, `đổi tên file docs`, []string{"targetParentId"}) {
		t.Fatal("non-move request should not trigger Drive move resolution")
	}
	if shouldResolveDriveMoveBeforeClarification(providers.ToolCall{Name: "drive.updateFileMetadata"}, request, []string{"targetParentId"}) {
		t.Fatal("other tools should not trigger Drive move resolution")
	}
	if shouldResolveDriveMoveBeforeClarification(call, request, []string{"name"}) {
		t.Fatal("unrelated missing fields should not trigger Drive move resolution")
	}
}

func TestPendingClarificationExpiresTTL(t *testing.T) {
	now := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)

	// Fresh clarification (10 min ago) — usable.
	fresh := &sessions.PendingClarification{
		OriginalRequest: "create a meeting",
		Question:        "what time?",
		CreatedAt:       now.Add(-10 * time.Minute),
	}
	if !isUsablePendingClarification(fresh, now) {
		t.Error("fresh clarification (10 min old) should be usable")
	}

	// Expired clarification (31 min ago, beyond 30-min TTL) — not usable.
	expired := &sessions.PendingClarification{
		OriginalRequest: "create a meeting",
		Question:        "what time?",
		CreatedAt:       now.Add(-31 * time.Minute),
	}
	if isUsablePendingClarification(expired, now) {
		t.Error("expired clarification (31 min old) should not be usable")
	}

	// Exactly at TTL boundary (30 min ago) — not usable (> not >=).
	atBoundary := &sessions.PendingClarification{
		OriginalRequest: "create a meeting",
		Question:        "what time?",
		CreatedAt:       now.Add(-30 * time.Minute),
	}
	if isUsablePendingClarification(atBoundary, now) {
		t.Error("clarification exactly at TTL boundary should not be usable")
	}

	// Zero CreatedAt (legacy memory.json without this field) — skip TTL, still usable.
	legacy := &sessions.PendingClarification{
		OriginalRequest: "create a meeting",
		Question:        "what time?",
	}
	if !isUsablePendingClarification(legacy, now) {
		t.Error("legacy clarification with zero CreatedAt should be usable (backward compat)")
	}

	// nil — not usable.
	if isUsablePendingClarification(nil, now) {
		t.Error("nil clarification should not be usable")
	}

	// Empty content — not usable.
	empty := &sessions.PendingClarification{CreatedAt: now.Add(-1 * time.Minute)}
	if isUsablePendingClarification(empty, now) {
		t.Error("clarification with no content should not be usable")
	}
}
