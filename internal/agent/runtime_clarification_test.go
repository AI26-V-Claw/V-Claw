package agent

import (
	"testing"

	"vclaw/internal/providers"
)

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
