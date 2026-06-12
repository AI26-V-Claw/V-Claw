package docs

import (
	"context"
	"testing"

	gdocs "vclaw/internal/connectors/google/docs"
	"vclaw/internal/tools"
)

type fakeDocsConnector struct{}

func (fakeDocsConnector) GetDocument(context.Context, string) (gdocs.Document, error) {
	return gdocs.Document{}, nil
}
func (fakeDocsConnector) CreateDocument(context.Context, string) (gdocs.Document, error) {
	return gdocs.Document{}, nil
}
func (fakeDocsConnector) AppendText(context.Context, string, string) (gdocs.AppendTextOutput, error) {
	return gdocs.AppendTextOutput{}, nil
}
func (fakeDocsConnector) ReplaceText(context.Context, string, string, string, bool) (gdocs.EditTextOutput, error) {
	return gdocs.EditTextOutput{}, nil
}
func (fakeDocsConnector) InsertText(context.Context, string, int64, string) (gdocs.EditTextOutput, error) {
	return gdocs.EditTextOutput{}, nil
}
func (fakeDocsConnector) DeleteContent(context.Context, string, int64, int64) (gdocs.EditTextOutput, error) {
	return gdocs.EditTextOutput{}, nil
}

type artifactDocsConnector struct {
	fakeDocsConnector
}

func (artifactDocsConnector) GetDocument(context.Context, string) (gdocs.Document, error) {
	return gdocs.Document{
		ID:       "doc_123",
		Title:    "Plan",
		Revision: "rev_1",
		BodyText: "abcdef",
	}, nil
}

func (artifactDocsConnector) AppendText(context.Context, string, string) (gdocs.AppendTextOutput, error) {
	return gdocs.AppendTextOutput{DocumentID: "doc_123", Title: "Plan"}, nil
}

func TestRegisterToolsMetadata(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry, NewService(fakeDocsConnector{})); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}

	assertToolMetadata(t, registry, ToolNameGetDocument, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertToolMetadata(t, registry, ToolNameCreateDocument, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameAppendText, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameReplaceText, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameInsertText, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameDeleteContent, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
}

func TestDocsToolResultIncludesArtifactMetadataAndTruncation(t *testing.T) {
	get := NewTool(ToolNameGetDocument, NewService(artifactDocsConnector{}))
	result := get.Execute(context.Background(), tools.ToolCall{
		ID:   "call_get_doc",
		Name: ToolNameGetDocument,
		Arguments: map[string]any{
			"documentId":   "doc_123",
			"previewChars": float64(3),
		},
	})

	if !result.Success {
		t.Fatalf("expected success, got %#v", result.Error)
	}
	if result.ArtifactRef == nil {
		t.Fatal("expected artifact ref")
	}
	if result.ArtifactRef.Kind != "google.docs.document" || result.ArtifactRef.ID != "doc_123" {
		t.Fatalf("unexpected artifact ref: %#v", result.ArtifactRef)
	}
	if result.Metadata["revision"] != "rev_1" {
		t.Fatalf("expected revision metadata, got %#v", result.Metadata)
	}
	if result.Metadata["preview_chars"] != 3 {
		t.Fatalf("expected preview_chars metadata, got %#v", result.Metadata)
	}
	if !result.Truncated {
		t.Fatal("expected truncated document preview")
	}
}

func assertToolMetadata(t *testing.T, registry *tools.ToolRegistry, name string, capability tools.Capability, risk tools.RiskLevel, approval bool) {
	t.Helper()
	definition, ok := registry.GetDefinition(name)
	if !ok {
		t.Fatalf("expected %s definition", name)
	}
	if definition.Capability != capability {
		t.Fatalf("%s capability = %s, want %s", name, definition.Capability, capability)
	}
	if definition.RiskLevel != risk {
		t.Fatalf("%s risk = %s, want %s", name, definition.RiskLevel, risk)
	}
	if definition.RequiresApproval != approval {
		t.Fatalf("%s approval = %t, want %t", name, definition.RequiresApproval, approval)
	}
}
