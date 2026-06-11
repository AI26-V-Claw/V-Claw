package drive

import (
	"context"
	"strings"
	"testing"

	gdrive "vclaw/internal/connectors/google/drive"
	"vclaw/internal/tools"
)

type fakeDriveConnector struct{}

func (fakeDriveConnector) ListFiles(context.Context, string, string, int64, string) (gdrive.ListFilesOutput, error) {
	return gdrive.ListFilesOutput{}, nil
}
func (fakeDriveConnector) GetFile(context.Context, string) (gdrive.FileSummary, error) {
	return gdrive.FileSummary{}, nil
}
func (fakeDriveConnector) ExportFile(context.Context, string, string, int64) (gdrive.FileContentOutput, error) {
	return gdrive.FileContentOutput{}, nil
}
func (fakeDriveConnector) DownloadFile(context.Context, string, int64) (gdrive.FileContentOutput, error) {
	return gdrive.FileContentOutput{}, nil
}
func (fakeDriveConnector) CreateFolder(context.Context, string, []string) (gdrive.FileSummary, error) {
	return gdrive.FileSummary{}, nil
}
func (fakeDriveConnector) CreateFile(context.Context, string, string, string, []string) (gdrive.FileSummary, error) {
	return gdrive.FileSummary{}, nil
}
func (fakeDriveConnector) UploadFile(context.Context, string, string, string, []string) (gdrive.FileSummary, error) {
	return gdrive.FileSummary{}, nil
}
func (fakeDriveConnector) UpdateFileMetadata(context.Context, string, gdrive.UpdateFileMetadataInput) (gdrive.FileSummary, error) {
	return gdrive.FileSummary{}, nil
}
func (fakeDriveConnector) ShareFile(context.Context, string, gdrive.ShareFileInput) (gdrive.PermissionSummary, error) {
	return gdrive.PermissionSummary{}, nil
}
func (fakeDriveConnector) ListPermissions(context.Context, string) ([]gdrive.PermissionSummary, error) {
	return nil, nil
}
func (fakeDriveConnector) RevokePermission(context.Context, string, string) (gdrive.PermissionSummary, error) {
	return gdrive.PermissionSummary{}, nil
}
func (fakeDriveConnector) MoveFile(context.Context, string, string, []string) (gdrive.FileSummary, error) {
	return gdrive.FileSummary{}, nil
}
func (fakeDriveConnector) TrashFile(context.Context, string) (gdrive.FileSummary, error) {
	return gdrive.FileSummary{}, nil
}
func (fakeDriveConnector) UntrashFile(context.Context, string) (gdrive.FileSummary, error) {
	return gdrive.FileSummary{}, nil
}

func TestRegisterToolsMetadata(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry, NewService(fakeDriveConnector{})); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}

	assertToolMetadata(t, registry, ToolNameListFiles, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertToolMetadata(t, registry, ToolNameGetFile, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertToolMetadata(t, registry, ToolNameExportFile, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertToolMetadata(t, registry, ToolNameDownloadFile, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertToolMetadata(t, registry, ToolNameCreateFolder, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameCreateFile, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameUploadFile, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameUpdateFileMetadata, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameShareFile, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameListPermissions, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertToolMetadata(t, registry, ToolNameRevokePermission, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameMoveFile, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameTrashFile, tools.CapabilityMutating, tools.RiskLevelDestructive, true)
	assertToolMetadata(t, registry, ToolNameUntrashFile, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
}

func TestUpdateMetadataDescriptionRejectsMoveUse(t *testing.T) {
	description := NewTool(ToolNameUpdateFileMetadata, NewService(fakeDriveConnector{})).Description()
	for _, want := range []string{"metadata only", "Do not use this tool to move", "drive.moveFile"} {
		if !strings.Contains(description, want) {
			t.Fatalf("expected update metadata description to contain %q, got %q", want, description)
		}
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
