package agent

import (
	"strings"
	"testing"

	"vclaw/internal/contracts"
)

func TestRenderDriveSearchFilesUsesLowercaseFields(t *testing.T) {
	response := contracts.AgentResponse{
		Status:  contracts.AgentStatusCompleted,
		Message: `{"files":[{"id":"1TI1GF9dm88y75JZnYEb4Uubl05uYrkVhD9BxUuJL_pc","name":"Cấu trúc đề thi ielts listening","mimeType":"application/vnd.google-apps.document","webViewLink":"https://docs.google.com/document/d/1TI1/edit"}]}`,
		ToolResults: []contracts.ToolResult{
			{
				ToolName: "drive.searchFiles",
				Success:  true,
				Data: map[string]any{
					"contentForUser": `{"files":[{"id":"1TI1GF9dm88y75JZnYEb4Uubl05uYrkVhD9BxUuJL_pc","name":"Cấu trúc đề thi ielts listening","mimeType":"application/vnd.google-apps.document","webViewLink":"https://docs.google.com/document/d/1TI1/edit"}]}`,
				},
			},
		},
	}

	got := renderAgentResponse(response)
	if !strings.Contains(got, "Cấu trúc đề thi ielts listening") {
		t.Fatalf("expected rendered file name, got:\n%s", got)
	}
	if strings.Contains(got, "một mục") {
		t.Fatalf("should not render Drive file as generic item, got:\n%s", got)
	}
}
