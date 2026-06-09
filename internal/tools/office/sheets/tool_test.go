package sheets

import (
	"context"
	"testing"

	sheetsconnector "vclaw/internal/connectors/google/sheets"
	"vclaw/internal/tools"
)

type fakeConnector struct{}

func (fakeConnector) GetSpreadsheet(context.Context, string) (sheetsconnector.Spreadsheet, error) {
	return sheetsconnector.Spreadsheet{}, nil
}
func (fakeConnector) ReadRange(context.Context, string, string) (sheetsconnector.RangeValues, error) {
	return sheetsconnector.RangeValues{}, nil
}
func (fakeConnector) CreateSpreadsheet(context.Context, string) (sheetsconnector.Spreadsheet, error) {
	return sheetsconnector.Spreadsheet{}, nil
}
func (fakeConnector) UpdateRange(context.Context, string, string, [][]interface{}, string) (sheetsconnector.RangeValues, error) {
	return sheetsconnector.RangeValues{}, nil
}
func (fakeConnector) AppendRows(context.Context, string, string, [][]interface{}, string) (sheetsconnector.RangeValues, error) {
	return sheetsconnector.RangeValues{}, nil
}

func TestRegisterToolsRiskMetadata(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry, NewService(fakeConnector{})); err != nil {
		t.Fatalf("register tools: %v", err)
	}

	assertTool(t, registry, ToolNameGetSpreadsheet, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertTool(t, registry, ToolNameListSheets, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertTool(t, registry, ToolNameReadRange, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertTool(t, registry, ToolNameCreateSpreadsheet, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertTool(t, registry, ToolNameUpdateRange, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertTool(t, registry, ToolNameAppendRows, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
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
