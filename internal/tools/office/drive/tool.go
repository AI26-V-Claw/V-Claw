package drive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vclaw/internal/connectors/google/common"
	gdrive "vclaw/internal/connectors/google/drive"
	"vclaw/internal/tools"
)

const (
	ToolNameListFiles          = "drive.listFiles"
	ToolNameGetFile            = "drive.getFile"
	ToolNameExportFile         = "drive.exportFile"
	ToolNameDownloadFile       = "drive.downloadFile"
	ToolNameCreateFolder       = "drive.createFolder"
	ToolNameCreateFile         = "drive.createFile"
	ToolNameUploadFile         = "drive.uploadFile"
	ToolNameUpdateFileMetadata = "drive.updateFileMetadata"
	ToolNameShareFile          = "drive.shareFile"
	ToolNameListPermissions    = "drive.listPermissions"
	ToolNameRevokePermission   = "drive.revokePermission"
	ToolNameMoveFile           = "drive.moveFile"
	ToolNameTrashFile          = "drive.trashFile"
	ToolNameUntrashFile        = "drive.untrashFile"

	defaultMaxResults = int64(10)
	maxResults        = int64(50)
)

type ToolRegistryEntry struct {
	Name             string
	Owner            string
	Description      string
	DefaultRiskLevel string
	RequiresApproval bool
}

var RegistryEntries = []ToolRegistryEntry{
	{Name: ToolNameListFiles, Owner: "integration", Description: "List files in Google Drive.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameGetFile, Owner: "integration", Description: "Read Google Drive file metadata.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameExportFile, Owner: "integration", Description: "Export a Google Workspace Drive file as text or another MIME type.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameDownloadFile, Owner: "integration", Description: "Download Drive file content into the tool response with a size cap.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameCreateFolder, Owner: "integration", Description: "Create a Google Drive folder.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameCreateFile, Owner: "integration", Description: "Create a Google Drive file from provided content.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameUploadFile, Owner: "integration", Description: "Upload a local file to Google Drive.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameUpdateFileMetadata, Owner: "integration", Description: "Update Google Drive file metadata.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameShareFile, Owner: "integration", Description: "Share a Google Drive file.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameListPermissions, Owner: "integration", Description: "List sharing permissions for a Google Drive file.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameRevokePermission, Owner: "integration", Description: "Revoke a Google Drive file permission.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameMoveFile, Owner: "integration", Description: "Move a Google Drive file or folder to another folder.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameTrashFile, Owner: "integration", Description: "Move a Google Drive file or folder to trash.", DefaultRiskLevel: "destructive", RequiresApproval: true},
	{Name: ToolNameUntrashFile, Owner: "integration", Description: "Restore a Google Drive file or folder from trash.", DefaultRiskLevel: "external_write", RequiresApproval: true},
}

type Connector interface {
	ListFiles(ctx context.Context, query, mimeType string, maxResults int64, pageToken string) (gdrive.ListFilesOutput, error)
	GetFile(ctx context.Context, fileID string) (gdrive.FileSummary, error)
	ExportFile(ctx context.Context, fileID string, mimeType string, maxBytes int64) (gdrive.FileContentOutput, error)
	DownloadFile(ctx context.Context, fileID string, maxBytes int64) (gdrive.FileContentOutput, error)
	CreateFolder(ctx context.Context, name string, parentIDs []string) (gdrive.FileSummary, error)
	CreateFile(ctx context.Context, name string, mimeType string, content string, parentIDs []string) (gdrive.FileSummary, error)
	UploadFile(ctx context.Context, localPath string, name string, mimeType string, parentIDs []string) (gdrive.FileSummary, error)
	UpdateFileMetadata(ctx context.Context, fileID string, input gdrive.UpdateFileMetadataInput) (gdrive.FileSummary, error)
	ShareFile(ctx context.Context, fileID string, input gdrive.ShareFileInput) (gdrive.PermissionSummary, error)
	ListPermissions(ctx context.Context, fileID string) ([]gdrive.PermissionSummary, error)
	RevokePermission(ctx context.Context, fileID string, permissionID string) (gdrive.PermissionSummary, error)
	MoveFile(ctx context.Context, fileID string, targetParentID string, removeParentIDs []string) (gdrive.FileSummary, error)
	TrashFile(ctx context.Context, fileID string) (gdrive.FileSummary, error)
	UntrashFile(ctx context.Context, fileID string) (gdrive.FileSummary, error)
}

type Service struct {
	connector Connector
}

func NewService(connector Connector) *Service {
	return &Service{connector: connector}
}

type ErrorShape struct {
	Code      string
	Message   string
	Retryable bool
}

type ListFilesInput struct {
	Query      string
	MimeType   string
	MaxResults int64
	PageToken  string
}

type GetFileInput struct {
	FileID string
}

type CreateFolderInput struct {
	Name      string
	ParentIDs []string
}

type FileContentInput struct {
	FileID   string
	MimeType string
	MaxBytes int64
}

type CreateFileInput struct {
	Name      string
	MimeType  string
	Content   string
	ParentIDs []string
}

type UploadFileInput struct {
	LocalPath string
	Name      string
	MimeType  string
	ParentIDs []string
}

type UpdateFileMetadataInput struct {
	FileID      string
	Name        string
	Description string
	Starred     *bool
}

type ShareFileInput struct {
	FileID                string
	Type                  string
	Role                  string
	EmailAddress          string
	AllowFileDiscovery    bool
	SendNotificationEmail bool
}

type RevokePermissionInput struct {
	FileID       string
	PermissionID string
}

type MoveFileInput struct {
	FileID          string
	TargetParentID  string
	RemoveParentIDs []string
}

type FileIDInput struct {
	FileID string
}

func (s *Service) ListFiles(ctx context.Context, input ListFilesInput) (gdrive.ListFilesOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdrive.ListFilesOutput{}, errShape
	}
	output, err := s.connector.ListFiles(ctx, input.Query, input.MimeType, boundMax(input.MaxResults), input.PageToken)
	if err != nil {
		return gdrive.ListFilesOutput{}, mapError(err)
	}
	return output, nil
}

func (s *Service) GetFile(ctx context.Context, input GetFileInput) (gdrive.FileSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdrive.FileSummary{}, errShape
	}
	if strings.TrimSpace(input.FileID) == "" {
		return gdrive.FileSummary{}, invalidInput("fileId is required")
	}
	file, err := s.connector.GetFile(ctx, input.FileID)
	if err != nil {
		return gdrive.FileSummary{}, mapError(err)
	}
	return file, nil
}

func (s *Service) ExportFile(ctx context.Context, input FileContentInput) (gdrive.FileContentOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdrive.FileContentOutput{}, errShape
	}
	if strings.TrimSpace(input.FileID) == "" {
		return gdrive.FileContentOutput{}, invalidInput("fileId is required")
	}
	output, err := s.connector.ExportFile(ctx, input.FileID, input.MimeType, input.MaxBytes)
	if err != nil {
		return gdrive.FileContentOutput{}, mapError(err)
	}
	return output, nil
}

func (s *Service) DownloadFile(ctx context.Context, input FileContentInput) (gdrive.FileContentOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdrive.FileContentOutput{}, errShape
	}
	if strings.TrimSpace(input.FileID) == "" {
		return gdrive.FileContentOutput{}, invalidInput("fileId is required")
	}
	output, err := s.connector.DownloadFile(ctx, input.FileID, input.MaxBytes)
	if err != nil {
		return gdrive.FileContentOutput{}, mapError(err)
	}
	return output, nil
}

func (s *Service) CreateFolder(ctx context.Context, input CreateFolderInput) (gdrive.FileSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdrive.FileSummary{}, errShape
	}
	if strings.TrimSpace(input.Name) == "" {
		return gdrive.FileSummary{}, invalidInput("name is required")
	}
	file, err := s.connector.CreateFolder(ctx, input.Name, input.ParentIDs)
	if err != nil {
		return gdrive.FileSummary{}, mapError(err)
	}
	return file, nil
}

func (s *Service) CreateFile(ctx context.Context, input CreateFileInput) (gdrive.FileSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdrive.FileSummary{}, errShape
	}
	if strings.TrimSpace(input.Name) == "" {
		return gdrive.FileSummary{}, invalidInput("name is required")
	}
	file, err := s.connector.CreateFile(ctx, input.Name, input.MimeType, input.Content, input.ParentIDs)
	if err != nil {
		return gdrive.FileSummary{}, mapError(err)
	}
	return file, nil
}

func (s *Service) UploadFile(ctx context.Context, input UploadFileInput) (gdrive.FileSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdrive.FileSummary{}, errShape
	}
	if strings.TrimSpace(input.LocalPath) == "" {
		return gdrive.FileSummary{}, invalidInput("localPath is required")
	}
	file, err := s.connector.UploadFile(ctx, input.LocalPath, input.Name, input.MimeType, input.ParentIDs)
	if err != nil {
		return gdrive.FileSummary{}, mapError(err)
	}
	return file, nil
}

func (s *Service) UpdateFileMetadata(ctx context.Context, input UpdateFileMetadataInput) (gdrive.FileSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdrive.FileSummary{}, errShape
	}
	if strings.TrimSpace(input.FileID) == "" {
		return gdrive.FileSummary{}, invalidInput("fileId is required")
	}
	if strings.TrimSpace(input.Name) == "" && strings.TrimSpace(input.Description) == "" && input.Starred == nil {
		return gdrive.FileSummary{}, invalidInput("at least one metadata field is required")
	}
	file, err := s.connector.UpdateFileMetadata(ctx, input.FileID, gdrive.UpdateFileMetadataInput{
		Name:        input.Name,
		Description: input.Description,
		Starred:     input.Starred,
	})
	if err != nil {
		return gdrive.FileSummary{}, mapError(err)
	}
	return file, nil
}

func (s *Service) ShareFile(ctx context.Context, input ShareFileInput) (gdrive.PermissionSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdrive.PermissionSummary{}, errShape
	}
	if strings.TrimSpace(input.FileID) == "" {
		return gdrive.PermissionSummary{}, invalidInput("fileId is required")
	}
	if !contains([]string{"user", "group", "domain", "anyone"}, input.Type) {
		return gdrive.PermissionSummary{}, invalidInput("type must be one of: user, group, domain, anyone")
	}
	if !contains([]string{"reader", "commenter", "writer"}, input.Role) {
		return gdrive.PermissionSummary{}, invalidInput("role must be one of: reader, commenter, writer")
	}
	if (input.Type == "user" || input.Type == "group") && strings.TrimSpace(input.EmailAddress) == "" {
		return gdrive.PermissionSummary{}, invalidInput("emailAddress is required for user or group permissions")
	}
	permission, err := s.connector.ShareFile(ctx, input.FileID, gdrive.ShareFileInput{
		Type:                  input.Type,
		Role:                  input.Role,
		EmailAddress:          input.EmailAddress,
		AllowFileDiscovery:    input.AllowFileDiscovery,
		SendNotificationEmail: input.SendNotificationEmail,
	})
	if err != nil {
		return gdrive.PermissionSummary{}, mapError(err)
	}
	return permission, nil
}

func (s *Service) ListPermissions(ctx context.Context, input FileIDInput) ([]gdrive.PermissionSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return nil, errShape
	}
	if strings.TrimSpace(input.FileID) == "" {
		return nil, invalidInput("fileId is required")
	}
	permissions, err := s.connector.ListPermissions(ctx, input.FileID)
	if err != nil {
		return nil, mapError(err)
	}
	return permissions, nil
}

func (s *Service) RevokePermission(ctx context.Context, input RevokePermissionInput) (gdrive.PermissionSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdrive.PermissionSummary{}, errShape
	}
	if strings.TrimSpace(input.FileID) == "" {
		return gdrive.PermissionSummary{}, invalidInput("fileId is required")
	}
	if strings.TrimSpace(input.PermissionID) == "" {
		return gdrive.PermissionSummary{}, invalidInput("permissionId is required")
	}
	permission, err := s.connector.RevokePermission(ctx, input.FileID, input.PermissionID)
	if err != nil {
		return gdrive.PermissionSummary{}, mapError(err)
	}
	return permission, nil
}

func (s *Service) MoveFile(ctx context.Context, input MoveFileInput) (gdrive.FileSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdrive.FileSummary{}, errShape
	}
	if strings.TrimSpace(input.FileID) == "" {
		return gdrive.FileSummary{}, invalidInput("fileId is required")
	}
	if strings.TrimSpace(input.TargetParentID) == "" {
		return gdrive.FileSummary{}, invalidInput("targetParentId is required")
	}
	file, err := s.connector.MoveFile(ctx, input.FileID, input.TargetParentID, input.RemoveParentIDs)
	if err != nil {
		return gdrive.FileSummary{}, mapError(err)
	}
	return file, nil
}

func (s *Service) TrashFile(ctx context.Context, input FileIDInput) (gdrive.FileSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdrive.FileSummary{}, errShape
	}
	if strings.TrimSpace(input.FileID) == "" {
		return gdrive.FileSummary{}, invalidInput("fileId is required")
	}
	file, err := s.connector.TrashFile(ctx, input.FileID)
	if err != nil {
		return gdrive.FileSummary{}, mapError(err)
	}
	return file, nil
}

func (s *Service) UntrashFile(ctx context.Context, input FileIDInput) (gdrive.FileSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdrive.FileSummary{}, errShape
	}
	if strings.TrimSpace(input.FileID) == "" {
		return gdrive.FileSummary{}, invalidInput("fileId is required")
	}
	file, err := s.connector.UntrashFile(ctx, input.FileID)
	if err != nil {
		return gdrive.FileSummary{}, mapError(err)
	}
	return file, nil
}

func (s *Service) validateConnector() *ErrorShape {
	if s == nil || s.connector == nil {
		return internalError("drive connector is not configured")
	}
	return nil
}

type DriveTool struct {
	name    string
	service *Service
}

func NewTool(name string, service *Service) DriveTool {
	return DriveTool{name: name, service: service}
}

func (t DriveTool) Name() string { return t.name }

func (t DriveTool) Description() string {
	switch t.name {
	case ToolNameListFiles:
		return "List Google Drive files by Drive query or MIME type. This is read-only and returns metadata only."
	case ToolNameGetFile:
		return "Read metadata for one Google Drive file by fileId."
	case ToolNameExportFile:
		return "Export a Google Workspace Drive file, such as a Doc or Sheet, into the tool response with a size cap. This is read-only."
	case ToolNameDownloadFile:
		return "Download Drive file content into the tool response with a size cap. This is read-only and does not write local files."
	case ToolNameCreateFolder:
		return "Create a folder in Google Drive. Requires human approval before execution."
	case ToolNameCreateFile:
		return "Create a Google Drive file from provided content. Requires human approval before execution."
	case ToolNameUploadFile:
		return "Upload a local file to Google Drive. Requires human approval before execution."
	case ToolNameUpdateFileMetadata:
		return "Update Google Drive file metadata only: name, description, or starred state. Do not use this tool to move files or change folders/parents; use drive.moveFile for move requests. Requires human approval before execution."
	case ToolNameShareFile:
		return "Share a Google Drive file by creating a permission. Requires human approval before execution."
	case ToolNameListPermissions:
		return "List sharing permissions for a Google Drive file. This is read-only."
	case ToolNameRevokePermission:
		return "Revoke a Google Drive file permission. Requires human approval before execution."
	case ToolNameMoveFile:
		return "Move a Google Drive file or folder into another Drive folder. Use this for requests like move/di chuyển/chuyển file X vào folder Y. Requires human approval before execution."
	case ToolNameTrashFile:
		return "Move a Google Drive file or folder to trash. Requires human approval before execution."
	case ToolNameUntrashFile:
		return "Restore a Google Drive file or folder from trash. Requires human approval before execution."
	default:
		return "Google Drive tool."
	}
}

func (t DriveTool) Parameters() tools.ToolSchema {
	switch t.name {
	case ToolNameListFiles:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"query":      map[string]any{"type": "string", "description": "Optional Drive query, e.g. name contains 'report' and trashed = false."},
			"mimeType":   map[string]any{"type": "string", "description": "Optional exact MIME type filter."},
			"maxResults": maxResultsSchema(),
			"pageToken":  map[string]any{"type": "string"},
		}, "additionalProperties": false}
	case ToolNameGetFile:
		return idSchema("fileId")
	case ToolNameExportFile:
		return contentReadSchema(true)
	case ToolNameDownloadFile:
		return contentReadSchema(false)
	case ToolNameCreateFolder:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"name":      map[string]any{"type": "string"},
			"parentIds": arrayStringSchema(),
		}, "required": []string{"name"}, "additionalProperties": false}
	case ToolNameCreateFile:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"name":      map[string]any{"type": "string"},
			"mimeType":  map[string]any{"type": "string", "description": "Defaults to text/plain."},
			"content":   map[string]any{"type": "string"},
			"parentIds": arrayStringSchema(),
		}, "required": []string{"name", "content"}, "additionalProperties": false}
	case ToolNameUploadFile:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"localPath": map[string]any{"type": "string"},
			"name":      map[string]any{"type": "string", "description": "Optional Drive file name; defaults to local basename."},
			"mimeType":  map[string]any{"type": "string"},
			"parentIds": arrayStringSchema(),
		}, "required": []string{"localPath"}, "additionalProperties": false}
	case ToolNameUpdateFileMetadata:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"fileId":      map[string]any{"type": "string", "description": "File ID whose metadata should be changed. This does not move the file."},
			"name":        map[string]any{"type": "string", "description": "New file name. For moving to another folder, use drive.moveFile instead."},
			"description": map[string]any{"type": "string", "description": "New file description."},
			"starred":     map[string]any{"type": "boolean", "description": "Whether the file is starred."},
		}, "required": []string{"fileId"}, "additionalProperties": false}
	case ToolNameShareFile:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"fileId":                map[string]any{"type": "string"},
			"type":                  map[string]any{"type": "string", "enum": []string{"user", "group", "domain", "anyone"}},
			"role":                  map[string]any{"type": "string", "enum": []string{"reader", "commenter", "writer"}},
			"emailAddress":          map[string]any{"type": "string"},
			"allowFileDiscovery":    map[string]any{"type": "boolean"},
			"sendNotificationEmail": map[string]any{"type": "boolean"},
		}, "required": []string{"fileId", "type", "role"}, "additionalProperties": false}
	case ToolNameListPermissions:
		return idSchema("fileId")
	case ToolNameRevokePermission:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"fileId":       map[string]any{"type": "string"},
			"permissionId": map[string]any{"type": "string"},
		}, "required": []string{"fileId", "permissionId"}, "additionalProperties": false}
	case ToolNameMoveFile:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"fileId":          map[string]any{"type": "string", "description": "ID of the file or folder to move."},
			"targetParentId":  map[string]any{"type": "string", "description": "Destination folder ID. Resolve folder names with drive.listFiles before calling this tool."},
			"removeParentIds": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional current parent folder IDs to remove. Omit to let the connector remove existing parents."},
		}, "required": []string{"fileId", "targetParentId"}, "additionalProperties": false}
	case ToolNameTrashFile, ToolNameUntrashFile:
		return idSchema("fileId")
	default:
		return tools.ToolSchema{"type": "object"}
	}
}

func (t DriveTool) Capability() tools.Capability {
	switch t.name {
	case ToolNameListFiles, ToolNameGetFile:
		return tools.CapabilityReadOnly
	case ToolNameExportFile, ToolNameDownloadFile, ToolNameListPermissions:
		return tools.CapabilityReadOnly
	default:
		return tools.CapabilityMutating
	}
}

func (t DriveTool) RiskLevel() tools.RiskLevel {
	switch t.name {
	case ToolNameListFiles, ToolNameGetFile, ToolNameExportFile, ToolNameDownloadFile, ToolNameListPermissions:
		return tools.RiskLevelSafeRead
	case ToolNameTrashFile:
		return tools.RiskLevelDestructive
	default:
		return tools.RiskLevelExternalWrite
	}
}

func (t DriveTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	switch t.name {
	case ToolNameListFiles:
		output, errShape := t.service.ListFiles(ctx, ListFilesInput{Query: stringArg(call.Arguments, "query"), MimeType: stringArg(call.Arguments, "mimeType"), MaxResults: int64Arg(call.Arguments, "maxResults"), PageToken: stringArg(call.Arguments, "pageToken")})
		return outputToolResult(call, output, errShape)
	case ToolNameGetFile:
		output, errShape := t.service.GetFile(ctx, GetFileInput{FileID: stringArg(call.Arguments, "fileId")})
		return outputToolResult(call, map[string]any{"File": output}, errShape)
	case ToolNameExportFile:
		output, errShape := t.service.ExportFile(ctx, FileContentInput{FileID: stringArg(call.Arguments, "fileId"), MimeType: stringArg(call.Arguments, "mimeType"), MaxBytes: int64Arg(call.Arguments, "maxBytes")})
		return outputToolResult(call, output, errShape)
	case ToolNameDownloadFile:
		output, errShape := t.service.DownloadFile(ctx, FileContentInput{FileID: stringArg(call.Arguments, "fileId"), MaxBytes: int64Arg(call.Arguments, "maxBytes")})
		return outputToolResult(call, output, errShape)
	case ToolNameCreateFolder:
		output, errShape := t.service.CreateFolder(ctx, CreateFolderInput{Name: stringArg(call.Arguments, "name"), ParentIDs: stringSliceArg(call.Arguments, "parentIds")})
		return outputToolResult(call, map[string]any{"File": output}, errShape)
	case ToolNameCreateFile:
		output, errShape := t.service.CreateFile(ctx, CreateFileInput{Name: stringArg(call.Arguments, "name"), MimeType: stringArg(call.Arguments, "mimeType"), Content: stringArg(call.Arguments, "content"), ParentIDs: stringSliceArg(call.Arguments, "parentIds")})
		return outputToolResult(call, map[string]any{"File": output}, errShape)
	case ToolNameUploadFile:
		output, errShape := t.service.UploadFile(ctx, UploadFileInput{LocalPath: stringArg(call.Arguments, "localPath"), Name: stringArg(call.Arguments, "name"), MimeType: stringArg(call.Arguments, "mimeType"), ParentIDs: stringSliceArg(call.Arguments, "parentIds")})
		return outputToolResult(call, map[string]any{"File": output}, errShape)
	case ToolNameUpdateFileMetadata:
		output, errShape := t.service.UpdateFileMetadata(ctx, UpdateFileMetadataInput{FileID: stringArg(call.Arguments, "fileId"), Name: stringArg(call.Arguments, "name"), Description: stringArg(call.Arguments, "description"), Starred: optionalBoolArg(call.Arguments, "starred")})
		return outputToolResult(call, map[string]any{"File": output}, errShape)
	case ToolNameShareFile:
		output, errShape := t.service.ShareFile(ctx, ShareFileInput{FileID: stringArg(call.Arguments, "fileId"), Type: stringArg(call.Arguments, "type"), Role: stringArg(call.Arguments, "role"), EmailAddress: stringArg(call.Arguments, "emailAddress"), AllowFileDiscovery: boolArg(call.Arguments, "allowFileDiscovery"), SendNotificationEmail: boolArg(call.Arguments, "sendNotificationEmail")})
		return outputToolResult(call, map[string]any{"Permission": output}, errShape)
	case ToolNameListPermissions:
		output, errShape := t.service.ListPermissions(ctx, FileIDInput{FileID: stringArg(call.Arguments, "fileId")})
		return outputToolResult(call, map[string]any{"Permissions": output}, errShape)
	case ToolNameRevokePermission:
		output, errShape := t.service.RevokePermission(ctx, RevokePermissionInput{FileID: stringArg(call.Arguments, "fileId"), PermissionID: stringArg(call.Arguments, "permissionId")})
		return outputToolResult(call, map[string]any{"Permission": output}, errShape)
	case ToolNameMoveFile:
		output, errShape := t.service.MoveFile(ctx, MoveFileInput{FileID: stringArg(call.Arguments, "fileId"), TargetParentID: stringArg(call.Arguments, "targetParentId"), RemoveParentIDs: stringSliceArg(call.Arguments, "removeParentIds")})
		return outputToolResult(call, map[string]any{"File": output}, errShape)
	case ToolNameTrashFile:
		output, errShape := t.service.TrashFile(ctx, FileIDInput{FileID: stringArg(call.Arguments, "fileId")})
		return outputToolResult(call, map[string]any{"File": output}, errShape)
	case ToolNameUntrashFile:
		output, errShape := t.service.UntrashFile(ctx, FileIDInput{FileID: stringArg(call.Arguments, "fileId")})
		return outputToolResult(call, map[string]any{"File": output}, errShape)
	default:
		return tools.ToolNotFoundResult(call)
	}
}

func RegisterTools(registry *tools.ToolRegistry, service *Service) error {
	for _, name := range []string{ToolNameListFiles, ToolNameGetFile, ToolNameExportFile, ToolNameDownloadFile, ToolNameCreateFolder, ToolNameCreateFile, ToolNameUploadFile, ToolNameUpdateFileMetadata, ToolNameShareFile, ToolNameListPermissions, ToolNameRevokePermission, ToolNameMoveFile, ToolNameTrashFile, ToolNameUntrashFile} {
		if err := registry.RegisterWithEntry(NewTool(name, service), tools.ToolRegistryEntry{Owner: "integration", Group: "google_workspace"}); err != nil {
			return err
		}
	}
	return nil
}

func outputToolResult(call tools.ToolCall, output any, errShape *ErrorShape) tools.ToolResult {
	if errShape != nil {
		return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: false, ContentForLLM: errShape.Code + ": " + errShape.Message, ContentForUser: errShape.Message, Error: &tools.ToolError{Code: errShape.Code, Message: errShape.Message}}
	}
	data, err := json.Marshal(output)
	if err != nil {
		data = []byte(fmt.Sprintf("%#v", output))
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  string(data),
		ContentForUser: string(data),
		ArtifactRef:    driveArtifactRef(output),
		Metadata:       driveResultMetadata(call, output),
		Truncated:      driveResultTruncated(output),
	}
}

func driveArtifactRef(output any) *tools.ToolArtifactRef {
	switch v := output.(type) {
	case gdrive.FileSummary:
		return driveFileArtifactRef(v)
	case gdrive.FileContentOutput:
		return driveFileArtifactRef(v.File)
	case map[string]any:
		if file, ok := v["File"].(gdrive.FileSummary); ok {
			return driveFileArtifactRef(file)
		}
		if permission, ok := v["Permission"].(gdrive.PermissionSummary); ok {
			return drivePermissionArtifactRef(permission)
		}
	}
	return nil
}

func driveFileArtifactRef(file gdrive.FileSummary) *tools.ToolArtifactRef {
	if strings.TrimSpace(file.ID) == "" {
		return nil
	}
	return &tools.ToolArtifactRef{
		Kind:  "google.drive.file",
		Label: firstNonEmpty(file.Name, "Google Drive file"),
		URI:   file.WebViewLink,
		ID:    file.ID,
	}
}

func drivePermissionArtifactRef(permission gdrive.PermissionSummary) *tools.ToolArtifactRef {
	if strings.TrimSpace(permission.ID) == "" {
		return nil
	}
	return &tools.ToolArtifactRef{
		Kind:  "google.drive.permission",
		Label: firstNonEmpty(permission.EmailAddress, permission.Type),
		ID:    permission.ID,
	}
}

func driveResultMetadata(call tools.ToolCall, output any) map[string]any {
	meta := map[string]any{}
	switch v := output.(type) {
	case gdrive.ListFilesOutput:
		meta["file_count"] = len(v.Files)
		if strings.TrimSpace(v.NextPageToken) != "" {
			meta["next_page_token"] = v.NextPageToken
		}
	case gdrive.FileContentOutput:
		meta["mime_type"] = v.MimeType
		meta["size_bytes"] = v.Size
	case map[string]any:
		if permissions, ok := v["Permissions"].([]gdrive.PermissionSummary); ok {
			meta["permission_count"] = len(permissions)
		}
		if permission, ok := v["Permission"].(gdrive.PermissionSummary); ok {
			meta["permission_type"] = permission.Type
			meta["permission_role"] = permission.Role
		}
	}
	if fileID := stringArg(call.Arguments, "fileId"); strings.TrimSpace(fileID) != "" {
		meta["file_id"] = fileID
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func driveResultTruncated(output any) bool {
	content, ok := output.(gdrive.FileContentOutput)
	return ok && content.Truncated
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mapError(err error) *ErrorShape {
	switch {
	case errors.Is(err, common.ErrAuth):
		return &ErrorShape{Code: "AUTH_EXPIRED", Message: err.Error(), Retryable: true}
	case errors.Is(err, common.ErrNotFound):
		return &ErrorShape{Code: "RESOURCE_NOT_FOUND", Message: err.Error(), Retryable: false}
	case errors.Is(err, common.ErrRateLimit):
		return &ErrorShape{Code: "RATE_LIMITED", Message: err.Error(), Retryable: true}
	case errors.Is(err, common.ErrAPI):
		return &ErrorShape{Code: "PROVIDER_UNAVAILABLE", Message: err.Error(), Retryable: true}
	default:
		return &ErrorShape{Code: "INTERNAL_ERROR", Message: err.Error(), Retryable: false}
	}
}

func invalidInput(message string) *ErrorShape {
	return &ErrorShape{Code: "INVALID_INPUT", Message: message, Retryable: false}
}

func internalError(message string) *ErrorShape {
	return &ErrorShape{Code: "INTERNAL_ERROR", Message: message, Retryable: false}
}

func boundMax(value int64) int64 {
	if value <= 0 {
		return defaultMaxResults
	}
	if value > maxResults {
		return maxResults
	}
	return value
}

func contains(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if target == value {
			return true
		}
	}
	return false
}

func idSchema(name string) tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{name: map[string]any{"type": "string"}}, "required": []string{name}, "additionalProperties": false}
}

func maxResultsSchema() map[string]any {
	return map[string]any{"type": "number", "minimum": 1, "maximum": maxResults, "description": "Omit to use default 10."}
}

func contentReadSchema(includeMimeType bool) tools.ToolSchema {
	properties := map[string]any{
		"fileId":   map[string]any{"type": "string"},
		"maxBytes": map[string]any{"type": "number", "minimum": 1, "maximum": 10 * 1024 * 1024, "description": "Omit for the default 10 MiB cap."},
	}
	if includeMimeType {
		properties["mimeType"] = map[string]any{"type": "string", "description": "Export MIME type. Defaults to text/plain."}
	}
	return tools.ToolSchema{"type": "object", "properties": properties, "required": []string{"fileId"}, "additionalProperties": false}
}

func arrayStringSchema() map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
}

func stringArg(args map[string]any, name string) string {
	if args == nil {
		return ""
	}
	value, _ := args[name].(string)
	return value
}

func boolArg(args map[string]any, name string) bool {
	if args == nil {
		return false
	}
	value, _ := args[name].(bool)
	return value
}

func optionalBoolArg(args map[string]any, name string) *bool {
	if args == nil {
		return nil
	}
	value, ok := args[name].(bool)
	if !ok {
		return nil
	}
	return &value
}

func int64Arg(args map[string]any, name string) int64 {
	if args == nil {
		return 0
	}
	switch value := args[name].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func stringSliceArg(args map[string]any, name string) []string {
	if args == nil {
		return nil
	}
	switch value := args[name].(type) {
	case []string:
		return cleanStrings(value)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return cleanStrings(out)
	default:
		return nil
	}
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
