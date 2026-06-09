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
