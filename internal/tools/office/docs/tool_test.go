package docs

import (
	"context"
	"testing"

	docsconnector "vclaw/internal/connectors/google/docs"
	"vclaw/internal/tools"
)

type fakeConnector struct{}

func (fakeConnector) GetDocument(context.Context, string) (docsconnector.Document, error) {
	return docsconnector.Document{}, nil
}
func (fakeConnector) CreateDocument(context.Context, string) (docsconnector.Document, error) {
	return docsconnector.Document{}, nil
}
func (fakeConnector) AppendText(context.Context, string, string) (docsconnector.Document, error) {
	return docsconnector.Document{}, nil
}

func TestRegisterToolsRiskMetadata(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry, NewService(fakeConnector{})); err != nil {
		t.Fatalf("register tools: %v", err)
	}

	assertTool(t, registry, ToolNameGetDocument, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertTool(t, registry, ToolNameCreateDocument, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertTool(t, registry, ToolNameAppendText, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
}

func TestOutputToolResultAddsDocsSourceAndArtifact(t *testing.T) {
	result := outputToolResult(tools.ToolCall{ID: "call_1", Name: ToolNameGetDocument}, docsconnector.Document{
		ID:         "doc_1",
		Title:      "Report",
		RevisionID: "rev_1",
	}, nil)

	if !result.Success {
		t.Fatalf("expected success, got %#v", result)
	}
	if len(result.SourceRefs) != 1 || result.SourceRefs[0].ID != "doc_1" {
		t.Fatalf("expected docs source ref, got %#v", result.SourceRefs)
	}
	if result.ArtifactRef == nil || result.ArtifactRef.ID != "doc_1" {
		t.Fatalf("expected docs artifact ref, got %#v", result.ArtifactRef)
	}
}

func assertTool(t *testing.T, registry *tools.ToolRegistry, name string, capability tools.Capability, risk tools.RiskLevel, approval bool) {
	t.Helper()
	def, ok := registry.GetDefinition(name)
	if !ok {
		t.Fatalf("missing tool %s", name)
	}
	if def.Group != "google_workspace" {
		t.Fatalf("%s group = %s", name, def.Group)
	}
	if def.Capability != capability {
		t.Fatalf("%s capability = %s, want %s", name, def.Capability, capability)
	}
	if def.RiskLevel != risk {
		t.Fatalf("%s risk = %s, want %s", name, def.RiskLevel, risk)
	}
	if def.RequiresApproval != approval {
		t.Fatalf("%s approval = %t, want %t", name, def.RequiresApproval, approval)
	}
}
