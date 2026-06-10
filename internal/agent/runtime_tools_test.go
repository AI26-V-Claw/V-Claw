package agent

import (
	"testing"

	"vclaw/internal/tools"
)

func TestContractToolResultCarriesConsistentShape(t *testing.T) {
	result := tools.SuccessResult(
		tools.ToolCall{ID: "call_1", Name: "drive.getFileMetadata"},
		map[string]any{"id": "file_1"},
		tools.WithSourceRefs(tools.SourceRef{Kind: "drive_file", ID: "file_1"}),
		tools.WithArtifactRef(tools.ArtifactRef{Kind: "drive_file", ID: "file_1"}),
		tools.WithMetadata(map[string]any{"provider": "google_drive"}),
	)

	contractResult := contractToolResult(result)
	if contractResult.ContentForLLM == "" || contractResult.ContentForUser == "" {
		t.Fatalf("expected top-level content fields, got %#v", contractResult)
	}
	data, ok := contractResult.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data map, got %#v", contractResult.Data)
	}
	for _, key := range []string{"contentForLLM", "contentForUser", "payload", "sourceRefs", "artifactRef", "metadata"} {
		if _, ok := data[key]; !ok {
			t.Fatalf("expected data.%s to be present, got %#v", key, data)
		}
	}
	if len(contractResult.SourceRefs) != 1 || contractResult.SourceRefs[0].ID != "file_1" {
		t.Fatalf("expected source refs to be mapped, got %#v", contractResult.SourceRefs)
	}
	if contractResult.ArtifactRef == nil || contractResult.ArtifactRef.ID != "file_1" {
		t.Fatalf("expected artifact ref to be mapped, got %#v", contractResult.ArtifactRef)
	}
}
