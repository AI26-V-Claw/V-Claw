package sheets

import (
	"context"
	"testing"

	gsheets "vclaw/internal/connectors/google/sheets"
	"vclaw/internal/tools"
)

type fakeSheetsConnector struct{}

func (fakeSheetsConnector) GetSpreadsheet(context.Context, string) (gsheets.SpreadsheetSummary, error) {
	return gsheets.SpreadsheetSummary{}, nil
}
func (fakeSheetsConnector) ReadValues(context.Context, string, string) (gsheets.ValuesOutput, error) {
	return gsheets.ValuesOutput{}, nil
}
func (fakeSheetsConnector) BatchGetValues(context.Context, string, []string) (gsheets.BatchValuesOutput, error) {
	return gsheets.BatchValuesOutput{}, nil
}
func (fakeSheetsConnector) CreateSpreadsheet(context.Context, string, []string) (gsheets.SpreadsheetSummary, error) {
	return gsheets.SpreadsheetSummary{}, nil
}
func (fakeSheetsConnector) UpdateValues(context.Context, string, string, [][]any, string) (gsheets.WriteValuesOutput, error) {
	return gsheets.WriteValuesOutput{}, nil
}
func (fakeSheetsConnector) BatchUpdateValues(context.Context, string, map[string][][]any, string) (gsheets.WriteValuesOutput, error) {
	return gsheets.WriteValuesOutput{}, nil
}
func (fakeSheetsConnector) AppendValues(context.Context, string, string, [][]any, string) (gsheets.AppendValuesOutput, error) {
	return gsheets.AppendValuesOutput{}, nil
}
func (fakeSheetsConnector) ClearValues(context.Context, string, string) (gsheets.ClearValuesOutput, error) {
	return gsheets.ClearValuesOutput{}, nil
}
func (fakeSheetsConnector) AddSheet(context.Context, string, string) (gsheets.SpreadsheetSummary, error) {
	return gsheets.SpreadsheetSummary{}, nil
}
func (fakeSheetsConnector) RenameSheet(context.Context, string, int64, string) (gsheets.SpreadsheetSummary, error) {
	return gsheets.SpreadsheetSummary{}, nil
}
func (fakeSheetsConnector) DeleteSheet(context.Context, string, int64) (gsheets.SpreadsheetSummary, error) {
	return gsheets.SpreadsheetSummary{}, nil
}
func (fakeSheetsConnector) DuplicateSheet(context.Context, string, int64, string) (gsheets.SpreadsheetSummary, error) {
	return gsheets.SpreadsheetSummary{}, nil
}

type artifactSheetsConnector struct {
	fakeSheetsConnector
}

func (artifactSheetsConnector) GetSpreadsheet(context.Context, string) (gsheets.SpreadsheetSummary, error) {
	return gsheets.SpreadsheetSummary{
		ID:             "sheet_123",
		Title:          "Metrics",
		SpreadsheetURL: "https://docs.google.com/spreadsheets/d/sheet_123/edit",
		Sheets:         []gsheets.SheetSummary{{ID: 1, Title: "Data"}},
	}, nil
}

func (artifactSheetsConnector) ReadValues(context.Context, string, string) (gsheets.ValuesOutput, error) {
	return gsheets.ValuesOutput{
		SpreadsheetID:  "sheet_123",
		Range:          "Data!A1:B2",
		MajorDimension: "ROWS",
		Values:         [][]any{{"Name", "Value"}, {"Smoke", "OK"}},
	}, nil
}

func TestRegisterToolsMetadata(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry, NewService(fakeSheetsConnector{})); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}

	assertToolMetadata(t, registry, ToolNameGetSpreadsheet, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertToolMetadata(t, registry, ToolNameReadValues, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertToolMetadata(t, registry, ToolNameBatchGetValues, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertToolMetadata(t, registry, ToolNameCreateSpreadsheet, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameUpdateValues, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameBatchUpdateValues, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameAppendValues, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameClearValues, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameAddSheet, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameRenameSheet, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameDeleteSheet, tools.CapabilityMutating, tools.RiskLevelDestructive, true)
	assertToolMetadata(t, registry, ToolNameDuplicateSheet, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
}

func TestSheetsToolResultIncludesArtifactAndSourceMetadata(t *testing.T) {
	read := NewTool(ToolNameReadValues, NewService(artifactSheetsConnector{}))
	result := read.Execute(context.Background(), tools.ToolCall{
		ID:   "call_read_values",
		Name: ToolNameReadValues,
		Arguments: map[string]any{
			"spreadsheetId": "sheet_123",
			"range":         "Data!A1:B2",
		},
	})

	if !result.Success {
		t.Fatalf("expected success, got %#v", result.Error)
	}
	if result.ArtifactRef == nil {
		t.Fatal("expected artifact ref")
	}
	if result.ArtifactRef.Kind != "google.sheets.spreadsheet" || result.ArtifactRef.ID != "sheet_123" {
		t.Fatalf("unexpected artifact ref: %#v", result.ArtifactRef)
	}
	if result.Metadata["range"] != "Data!A1:B2" {
		t.Fatalf("expected range metadata, got %#v", result.Metadata)
	}
	if result.Metadata["row_count"] != 2 {
		t.Fatalf("expected row_count metadata, got %#v", result.Metadata)
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
