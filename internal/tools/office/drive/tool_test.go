package drive

import (
	"context"
	"errors"
	"strings"
	"testing"

	gdrive "vclaw/internal/connectors/google/drive"
	"vclaw/internal/tools"
)

// fakeUploadGuard approves only paths under allowedPrefix, modelling the sandbox
// PathGuard used by drive.uploadFile.
type fakeUploadGuard struct{ allowedPrefix string }

func (g fakeUploadGuard) Resolve(path string) (string, error) {
	if g.allowedPrefix != "" && strings.HasPrefix(path, g.allowedPrefix) {
		return path, nil
	}
	return "", errors.New("path is outside allowed directories")
}

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

type artifactDriveConnector struct {
	fakeDriveConnector
}

func (artifactDriveConnector) GetFile(context.Context, string) (gdrive.FileSummary, error) {
	return gdrive.FileSummary{ID: "file_123", Name: "Report", WebViewLink: "https://drive.google.com/file/d/file_123/view"}, nil
}

func (artifactDriveConnector) DownloadFile(context.Context, string, int64) (gdrive.FileContentOutput, error) {
	return gdrive.FileContentOutput{
		File:      gdrive.FileSummary{ID: "file_123", Name: "Report", WebViewLink: "https://drive.google.com/file/d/file_123/view"},
		MimeType:  "text/plain",
		Size:      2048,
		Content:   "preview",
		Truncated: true,
	}, nil
}

type listFilesConnector struct {
	fakeDriveConnector
}

func (listFilesConnector) ListFiles(context.Context, string, string, int64, string) (gdrive.ListFilesOutput, error) {
	return gdrive.ListFilesOutput{Files: []gdrive.FileSummary{{
		ID:           "file_1",
		Name:         "Long metadata doc",
		MimeType:     "application/vnd.google-apps.document",
		Description:  strings.Repeat("verbose description ", 80),
		WebViewLink:  "https://drive.google.com/file/d/file_1/view",
		Owners:       []string{"owner@example.com"},
		ModifiedTime: "2026-06-12T02:58:37.000Z",
		Parents:      []string{"folder_1"},
	}}}, nil
}

type moveFilesConnector struct {
	fakeDriveConnector
	moved []string
}

func (c *moveFilesConnector) MoveFile(_ context.Context, fileID string, targetParentID string, _ []string) (gdrive.FileSummary, error) {
	c.moved = append(c.moved, fileID)
	return gdrive.FileSummary{ID: fileID, Name: fileID + " name", Parents: []string{targetParentID}}, nil
}

func TestRegisterToolsMetadata(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry, NewService(fakeDriveConnector{}), nil); err != nil {
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
	assertToolMetadata(t, registry, ToolNameMoveFiles, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameTrashFile, tools.CapabilityMutating, tools.RiskLevelDestructive, true)
	assertToolMetadata(t, registry, ToolNameUntrashFile, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
}

func TestMoveFilesMovesEveryFileID(t *testing.T) {
	connector := &moveFilesConnector{}
	tool := NewTool(ToolNameMoveFiles, NewService(connector), nil)
	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "call_move_files",
		Name: ToolNameMoveFiles,
		Arguments: map[string]any{
			"fileIds":        []any{"file_1", "file_2"},
			"targetParentId": "folder_1",
		},
	})

	if !result.Success {
		t.Fatalf("expected success, got %#v", result.Error)
	}
	if strings.Join(connector.moved, ",") != "file_1,file_2" {
		t.Fatalf("moved = %#v, want [file_1 file_2]", connector.moved)
	}
	if result.Metadata["file_count"] != 2 {
		t.Fatalf("expected file_count metadata, got %#v", result.Metadata)
	}
	if !strings.Contains(result.ContentForUser, "file_1") || !strings.Contains(result.ContentForUser, "file_2") {
		t.Fatalf("result should include moved files, got %s", result.ContentForUser)
	}
}

func TestListFilesUsesCompactContentForLLM(t *testing.T) {
	tool := NewTool(ToolNameListFiles, NewService(listFilesConnector{}), nil)
	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:        "call_list",
		Name:      ToolNameListFiles,
		Arguments: map[string]any{"query": "name contains 'doc'"},
	})

	if !result.Success {
		t.Fatalf("expected success, got %#v", result.Error)
	}
	for _, want := range []string{"file_1", "Long metadata doc", "folder_1"} {
		if !strings.Contains(result.ContentForLLM, want) {
			t.Fatalf("ContentForLLM missing %q: %s", want, result.ContentForLLM)
		}
	}
	for _, verbose := range []string{"verbose description", "owner@example.com", `"Description"`} {
		if strings.Contains(result.ContentForLLM, verbose) {
			t.Fatalf("ContentForLLM should omit verbose field %q: %s", verbose, result.ContentForLLM)
		}
		if !strings.Contains(result.ContentForUser, verbose) {
			t.Fatalf("ContentForUser should keep verbose field %q: %s", verbose, result.ContentForUser)
		}
	}
}

func TestUpdateMetadataDescriptionRejectsMoveUse(t *testing.T) {
	description := NewTool(ToolNameUpdateFileMetadata, NewService(fakeDriveConnector{}), nil).Description()
	for _, want := range []string{"metadata only", "Do not use this tool to move", "drive.moveFile"} {
		if !strings.Contains(description, want) {
			t.Fatalf("expected update metadata description to contain %q, got %q", want, description)
		}
	}
}

func TestDriveToolResultIncludesArtifactMetadataAndTruncation(t *testing.T) {
	download := NewTool(ToolNameDownloadFile, NewService(artifactDriveConnector{}), nil)
	result := download.Execute(context.Background(), tools.ToolCall{
		ID:   "call_download",
		Name: ToolNameDownloadFile,
		Arguments: map[string]any{
			"fileId":   "file_123",
			"maxBytes": float64(1024),
		},
	})

	if !result.Success {
		t.Fatalf("expected success, got %#v", result.Error)
	}
	if result.ArtifactRef == nil {
		t.Fatal("expected artifact ref")
	}
	if result.ArtifactRef.Kind != "google.drive.file" || result.ArtifactRef.ID != "file_123" {
		t.Fatalf("unexpected artifact ref: %#v", result.ArtifactRef)
	}
	if result.Metadata["mime_type"] != "text/plain" {
		t.Fatalf("expected mime_type metadata, got %#v", result.Metadata)
	}
	if result.Metadata["size_bytes"] != int64(2048) {
		t.Fatalf("expected size_bytes metadata, got %#v", result.Metadata)
	}
	if !result.Truncated {
		t.Fatal("expected truncated result")
	}
}

func TestUploadFileRejectsPathOutsideSandbox(t *testing.T) {
	tool := NewTool(ToolNameUploadFile, NewService(fakeDriveConnector{}), fakeUploadGuard{allowedPrefix: "/workspace/"})
	result := tool.Execute(context.Background(), tools.ToolCall{ID: "c1", Name: ToolNameUploadFile, Arguments: map[string]any{"localPath": "/etc/passwd"}})
	if result.Success {
		t.Fatal("expected upload of out-of-sandbox path to fail")
	}
	if result.Error == nil || result.Error.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %#v", result.Error)
	}
}

func TestUploadFileAllowsPathInsideSandbox(t *testing.T) {
	tool := NewTool(ToolNameUploadFile, NewService(fakeDriveConnector{}), fakeUploadGuard{allowedPrefix: "/workspace/"})
	result := tool.Execute(context.Background(), tools.ToolCall{ID: "c2", Name: ToolNameUploadFile, Arguments: map[string]any{"localPath": "/workspace/report.txt"}})
	if !result.Success {
		t.Fatalf("expected in-sandbox upload to succeed, got %#v", result.Error)
	}
}

func TestUploadFileWithoutGuardRejected(t *testing.T) {
	tool := NewTool(ToolNameUploadFile, NewService(fakeDriveConnector{}), nil)
	result := tool.Execute(context.Background(), tools.ToolCall{ID: "c3", Name: ToolNameUploadFile, Arguments: map[string]any{"localPath": "/workspace/report.txt"}})
	if result.Success {
		t.Fatal("expected upload without a configured guard to fail")
	}
}

func TestShareFileRejectsPublicWrite(t *testing.T) {
	service := NewService(fakeDriveConnector{})
	for _, role := range []string{"writer", "commenter"} {
		_, errShape := service.ShareFile(context.Background(), ShareFileInput{FileID: "f1", Type: "anyone", Role: role})
		if errShape == nil || errShape.Code != "INVALID_INPUT" {
			t.Fatalf("anyone+%s should be rejected with INVALID_INPUT, got %#v", role, errShape)
		}
	}
	if _, errShape := service.ShareFile(context.Background(), ShareFileInput{FileID: "f1", Type: "anyone", Role: "reader"}); errShape != nil {
		t.Fatalf("anyone+reader should be allowed, got %#v", errShape)
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
