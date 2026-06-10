package drive

import (
	"context"
	"testing"

	driveconnector "vclaw/internal/connectors/google/drive"
	"vclaw/internal/tools"
)

type fakeConnector struct{}

func (fakeConnector) SearchFiles(context.Context, string, int64, string) (driveconnector.SearchFilesOutput, error) {
	return driveconnector.SearchFilesOutput{}, nil
}
func (fakeConnector) GetFileMetadata(context.Context, string) (driveconnector.FileMetadata, error) {
	return driveconnector.FileMetadata{}, nil
}
func (fakeConnector) ExportFile(context.Context, string, string) (driveconnector.FileContent, error) {
	return driveconnector.FileContent{}, nil
}
func (fakeConnector) DownloadFile(context.Context, string) (driveconnector.FileContent, error) {
	return driveconnector.FileContent{}, nil
}
func (fakeConnector) CreateTextFile(context.Context, driveconnector.TextFileInput) (driveconnector.FileMetadata, error) {
	return driveconnector.FileMetadata{}, nil
}
func (fakeConnector) CreateFolder(context.Context, driveconnector.FolderInput) (driveconnector.FileMetadata, error) {
	return driveconnector.FileMetadata{}, nil
}
func (fakeConnector) UpdateTextFile(context.Context, string, driveconnector.TextFileInput) (driveconnector.FileMetadata, error) {
	return driveconnector.FileMetadata{}, nil
}
func (fakeConnector) RenameFile(context.Context, string, string) (driveconnector.FileMetadata, error) {
	return driveconnector.FileMetadata{}, nil
}
func (fakeConnector) MoveFile(context.Context, string, string) (driveconnector.FileMetadata, error) {
	return driveconnector.FileMetadata{}, nil
}
func (fakeConnector) ShareFile(context.Context, driveconnector.PermissionInput) (string, error) {
	return "perm_1", nil
}

func TestRegisterToolsRiskMetadata(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry, NewService(fakeConnector{})); err != nil {
		t.Fatalf("register tools: %v", err)
	}

	assertTool(t, registry, ToolNameSearchFiles, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertTool(t, registry, ToolNameGetFileMetadata, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertTool(t, registry, ToolNameExportFile, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertTool(t, registry, ToolNameDownloadFile, tools.CapabilityMutating, tools.RiskLevelLocalWrite, true)
	assertTool(t, registry, ToolNameCreateTextFile, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertTool(t, registry, ToolNameCreateFolder, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertTool(t, registry, ToolNameUpdateTextFile, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertTool(t, registry, ToolNameRenameFile, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertTool(t, registry, ToolNameMoveFile, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertTool(t, registry, ToolNameMoveFiles, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertTool(t, registry, ToolNameShareFile, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
}

func TestOutputToolResultAddsDriveSourceAndArtifact(t *testing.T) {
	result := outputToolResult(tools.ToolCall{ID: "call_1", Name: ToolNameGetFileMetadata}, driveconnector.FileMetadata{
		ID:          "file_1",
		Name:        "Report",
		MimeType:    "application/pdf",
		WebViewLink: "https://drive.google.com/file/d/file_1",
	}, nil)

	if !result.Success {
		t.Fatalf("expected success, got %#v", result)
	}
	if len(result.SourceRefs) != 1 || result.SourceRefs[0].ID != "file_1" {
		t.Fatalf("expected drive source ref, got %#v", result.SourceRefs)
	}
	if result.ArtifactRef == nil || result.ArtifactRef.ID != "file_1" {
		t.Fatalf("expected drive artifact ref, got %#v", result.ArtifactRef)
	}
	if result.Metadata["provider"] != "google_drive" {
		t.Fatalf("expected google_drive metadata, got %#v", result.Metadata)
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
