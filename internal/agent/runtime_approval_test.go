package agent

import (
	"strings"
	"testing"

	"vclaw/internal/providers"
)

func TestEnrichDriveMoveApprovalInputUsesRecentListFilesResults(t *testing.T) {
	input := map[string]any{
		"fileId":         "file_1",
		"targetParentId": "folder_1",
	}
	transcript := []providers.Message{{
		Role:       providers.MessageRoleTool,
		ToolCallID: "call_list_sources",
		Content:    `{"Files":[{"id":"file_1","name":"Thuật toán segment tree","mimeType":"application/vnd.google-apps.document"},{"id":"folder_1","name":"Nhập môn lập trình","mimeType":"application/vnd.google-apps.folder"}]}`,
	}}

	got := enrichApprovalInput("drive.moveFile", input, transcript)
	sources, ok := got["sourceFiles"].([]string)
	if !ok || len(sources) != 1 {
		t.Fatalf("sourceFiles = %#v, want one source", got["sourceFiles"])
	}
	if !strings.Contains(sources[0], "Thuật toán segment tree") || !strings.Contains(sources[0], "file_1") {
		t.Fatalf("unexpected source display: %q", sources[0])
	}
	target, _ := got["targetFolder"].(string)
	if !strings.Contains(target, "Nhập môn lập trình") || !strings.Contains(target, "folder_1") {
		t.Fatalf("unexpected target display: %q", target)
	}
}

func TestEnrichDriveMoveFilesApprovalInputShowsEverySource(t *testing.T) {
	input := map[string]any{
		"fileIds":        []any{"file_1", "file_2"},
		"targetParentId": "folder_1",
	}
	transcript := []providers.Message{{
		Role:    providers.MessageRoleTool,
		Content: `{"Files":[{"ID":"file_1","Name":"A"},{"ID":"file_2","Name":"B"},{"ID":"folder_1","Name":"Đích"}]}`,
	}}

	got := enrichApprovalInput("drive.moveFiles", input, transcript)
	sources, ok := got["sourceFiles"].([]string)
	if !ok || len(sources) != 2 {
		t.Fatalf("sourceFiles = %#v, want two sources", got["sourceFiles"])
	}
	if !strings.Contains(strings.Join(sources, "\n"), "A") || !strings.Contains(strings.Join(sources, "\n"), "B") {
		t.Fatalf("unexpected sources: %#v", sources)
	}
}
