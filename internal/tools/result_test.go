package tools

import (
	"strings"
	"testing"
)

func TestSuccessResultRedactsAndTruncatesContent(t *testing.T) {
	call := ToolCall{ID: "call_1", Name: "test.tool"}
	result := SuccessResult(call, map[string]any{
		"message": "Authorization: Bearer secret-token-value " + strings.Repeat("x", 256),
	}, WithContentLimits(120, 120))

	if !result.Success {
		t.Fatal("expected success result")
	}
	if strings.Contains(result.ContentForLLM, "secret-token-value") || strings.Contains(result.ContentForUser, "secret-token-value") {
		t.Fatalf("expected sensitive token to be redacted: %#v", result)
	}
	if !strings.Contains(result.ContentForLLM, "[REDACTED]") {
		t.Fatalf("expected redaction marker, got %q", result.ContentForLLM)
	}
	if !strings.Contains(result.ContentForLLM, "[truncated]") {
		t.Fatalf("expected LLM content to be truncated, got %q", result.ContentForLLM)
	}
}

func TestSuccessResultCarriesSourceArtifactAndMetadata(t *testing.T) {
	call := ToolCall{ID: "call_1", Name: "drive.getFileMetadata"}
	source := SourceRef{Kind: "drive_file", ID: "file_1", Label: "Report", URI: "https://drive.google.com/file/d/file_1"}
	artifact := ArtifactRef{Kind: "drive_file", ID: "file_1", Label: "Report", URI: source.URI}
	result := SuccessResult(call, map[string]any{"id": "file_1"}, WithSourceRefs(source), WithArtifactRef(artifact), WithMetadata(map[string]any{"provider": "google_drive"}))

	if len(result.SourceRefs) != 1 || result.SourceRefs[0].ID != "file_1" {
		t.Fatalf("expected source ref to be preserved, got %#v", result.SourceRefs)
	}
	if result.ArtifactRef == nil || result.ArtifactRef.ID != "file_1" {
		t.Fatalf("expected artifact ref to be preserved, got %#v", result.ArtifactRef)
	}
	if result.Metadata["provider"] != "google_drive" {
		t.Fatalf("expected metadata to be preserved, got %#v", result.Metadata)
	}
}

func TestErrorResultRedactsSensitiveMessage(t *testing.T) {
	call := ToolCall{ID: "call_1", Name: "test.tool"}
	result := ErrorResult(call, ErrorExecutionFailed, "provider returned access_token=secret-value")

	if result.Success {
		t.Fatal("expected error result")
	}
	if result.Error == nil {
		t.Fatal("expected tool error")
	}
	if strings.Contains(result.ContentForLLM, "secret-value") || strings.Contains(result.Error.Message, "secret-value") {
		t.Fatalf("expected sensitive error to be redacted, got %#v", result)
	}
}
