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
