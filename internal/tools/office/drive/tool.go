package drive

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	googleconnector "vclaw/internal/connectors/google"
	driveconnector "vclaw/internal/connectors/google/drive"
	"vclaw/internal/tools"

	"google.golang.org/api/googleapi"
)

const (
	ToolNameSearchFiles     = "drive.searchFiles"
	ToolNameGetFileMetadata = "drive.getFileMetadata"
	ToolNameExportFile      = "drive.exportFile"
	ToolNameDownloadFile    = "drive.downloadFile"
	ToolNameCreateTextFile  = "drive.createTextFile"
	ToolNameCreateFolder    = "drive.createFolder"
	ToolNameUpdateTextFile  = "drive.updateTextFile"
	ToolNameRenameFile      = "drive.renameFile"
	ToolNameMoveFile        = "drive.moveFile"
	ToolNameMoveFiles       = "drive.moveFiles"
	ToolNameShareFile       = "drive.shareFile"

	defaultMaxResults  = int64(10)
	maxAllowedResults  = int64(50)
	maxExportTextChars = 20000
)

type ToolRegistryEntry struct {
	Name             string
	Owner            string
	Description      string
	DefaultRiskLevel string
	RequiresApproval bool
}

var RegistryEntries = []ToolRegistryEntry{
	{Name: ToolNameSearchFiles, Owner: "integration", Description: "Search Google Drive files.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameGetFileMetadata, Owner: "integration", Description: "Read Google Drive file metadata.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameExportFile, Owner: "integration", Description: "Export a Google-native Drive file to text-like content.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameDownloadFile, Owner: "integration", Description: "Download a Drive file to a local directory.", DefaultRiskLevel: "local_write", RequiresApproval: true},
	{Name: ToolNameCreateTextFile, Owner: "integration", Description: "Create a text-like file in Drive.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameCreateFolder, Owner: "integration", Description: "Create a folder in Drive.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameUpdateTextFile, Owner: "integration", Description: "Update text-like content in a Drive file.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameRenameFile, Owner: "integration", Description: "Rename a Drive file by updating metadata only.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameMoveFile, Owner: "integration", Description: "Move a Drive file into a folder by updating parents.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameMoveFiles, Owner: "integration", Description: "Move multiple Drive files into a folder by updating parents.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameShareFile, Owner: "integration", Description: "Share a Drive file by creating a permission.", DefaultRiskLevel: "external_write", RequiresApproval: true},
}

type Connector interface {
	SearchFiles(ctx context.Context, query string, pageSize int64, pageToken string) (driveconnector.SearchFilesOutput, error)
	GetFileMetadata(ctx context.Context, fileID string) (driveconnector.FileMetadata, error)
	ExportFile(ctx context.Context, fileID string, mimeType string) (driveconnector.FileContent, error)
	DownloadFile(ctx context.Context, fileID string) (driveconnector.FileContent, error)
	CreateTextFile(ctx context.Context, input driveconnector.TextFileInput) (driveconnector.FileMetadata, error)
	CreateFolder(ctx context.Context, input driveconnector.FolderInput) (driveconnector.FileMetadata, error)
	UpdateTextFile(ctx context.Context, fileID string, input driveconnector.TextFileInput) (driveconnector.FileMetadata, error)
	RenameFile(ctx context.Context, fileID string, name string) (driveconnector.FileMetadata, error)
	MoveFile(ctx context.Context, fileID string, folderID string) (driveconnector.FileMetadata, error)
	ShareFile(ctx context.Context, input driveconnector.PermissionInput) (string, error)
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

type SearchFilesInput struct {
	Query      string
	MaxResults int64
	PageToken  string
}

type GetFileMetadataInput struct {
	FileID string
}

type ExportFileInput struct {
	FileID   string
	MimeType string
}

type DownloadFileInput struct {
	FileID    string
	OutputDir string
	Filename  string
}

type DownloadFileOutput struct {
	File driveconnector.FileMetadata `json:"file"`
	Path string                      `json:"path"`
	Size int                         `json:"size"`
}

type CreateTextFileInput struct {
	Name     string
	MimeType string
	ParentID string
	Content  string
}

type CreateFolderInput struct {
	Name     string
	ParentID string
}

type UpdateTextFileInput struct {
	FileID   string
	Name     string
	MimeType string
	Content  string
}

type RenameFileInput struct {
	FileID string
	Name   string
}

type MoveFileInput struct {
	FileID   string
	FolderID string
}

type MoveFilesInput struct {
	FileIDs  []string
	FolderID string
}

type MoveFilesFailure struct {
	FileID  string `json:"fileId"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type MoveFilesOutput struct {
	Files    []driveconnector.FileMetadata `json:"files"`
	Failures []MoveFilesFailure            `json:"failures,omitempty"`
}

type ShareFileInput struct {
	FileID       string
	Role         string
	Type         string
	EmailAddress string
	Domain       string
}

type ShareFileOutput struct {
	FileID       string `json:"fileId"`
	PermissionID string `json:"permissionId"`
	Role         string `json:"role"`
	Type         string `json:"type"`
	EmailAddress string `json:"emailAddress,omitempty"`
	Domain       string `json:"domain,omitempty"`
}

func (s *Service) SearchFiles(ctx context.Context, input SearchFilesInput) (driveconnector.SearchFilesOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return driveconnector.SearchFilesOutput{}, errShape
	}
	maxResults, errShape := normalizeMaxResults(input.MaxResults)
	if errShape != nil {
		return driveconnector.SearchFilesOutput{}, errShape
	}
	output, err := s.connector.SearchFiles(ctx, strings.TrimSpace(input.Query), maxResults, strings.TrimSpace(input.PageToken))
	if err != nil {
		return driveconnector.SearchFilesOutput{}, MapError(err)
	}
	return output, nil
}

func (s *Service) GetFileMetadata(ctx context.Context, input GetFileMetadataInput) (driveconnector.FileMetadata, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return driveconnector.FileMetadata{}, errShape
	}
	if strings.TrimSpace(input.FileID) == "" {
		return driveconnector.FileMetadata{}, invalidInput("fileId is required")
	}
	output, err := s.connector.GetFileMetadata(ctx, strings.TrimSpace(input.FileID))
	if err != nil {
		return driveconnector.FileMetadata{}, MapError(err)
	}
	return output, nil
}

func (s *Service) ExportFile(ctx context.Context, input ExportFileInput) (driveconnector.FileContent, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return driveconnector.FileContent{}, errShape
	}
	if strings.TrimSpace(input.FileID) == "" {
		return driveconnector.FileContent{}, invalidInput("fileId is required")
	}
	mimeType := strings.TrimSpace(input.MimeType)
	if mimeType == "" {
		mimeType = "text/plain"
	}
	output, err := s.connector.ExportFile(ctx, strings.TrimSpace(input.FileID), mimeType)
	if err != nil {
		return driveconnector.FileContent{}, MapError(err)
	}
	output.ContentText = truncate(output.ContentText, maxExportTextChars)
	return output, nil
}

func (s *Service) DownloadFile(ctx context.Context, input DownloadFileInput) (DownloadFileOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return DownloadFileOutput{}, errShape
	}
	if strings.TrimSpace(input.FileID) == "" {
		return DownloadFileOutput{}, invalidInput("fileId is required")
	}
	if strings.TrimSpace(input.OutputDir) == "" {
		return DownloadFileOutput{}, invalidInput("outputDir is required")
	}
	content, err := s.connector.DownloadFile(ctx, strings.TrimSpace(input.FileID))
	if err != nil {
		return DownloadFileOutput{}, MapError(err)
	}
	if err := os.MkdirAll(input.OutputDir, 0750); err != nil {
		return DownloadFileOutput{}, &ErrorShape{Code: "FILE_ACCESS_DENIED", Message: err.Error()}
	}
	filename := safeFilename(firstNonEmpty(input.Filename, content.File.Name, input.FileID))
	path := filepath.Join(input.OutputDir, filename)
	if err := os.WriteFile(path, content.Data, 0640); err != nil {
		return DownloadFileOutput{}, &ErrorShape{Code: "FILE_ACCESS_DENIED", Message: err.Error()}
	}
	return DownloadFileOutput{File: content.File, Path: path, Size: len(content.Data)}, nil
}

func (s *Service) CreateTextFile(ctx context.Context, input CreateTextFileInput) (driveconnector.FileMetadata, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return driveconnector.FileMetadata{}, errShape
	}
	if strings.TrimSpace(input.Name) == "" {
		return driveconnector.FileMetadata{}, invalidInput("name is required")
	}
	output, err := s.connector.CreateTextFile(ctx, driveconnector.TextFileInput{
		Name:     strings.TrimSpace(input.Name),
		MimeType: strings.TrimSpace(input.MimeType),
		ParentID: strings.TrimSpace(input.ParentID),
		Content:  input.Content,
	})
	if err != nil {
		return driveconnector.FileMetadata{}, MapError(err)
	}
	return output, nil
}

func (s *Service) CreateFolder(ctx context.Context, input CreateFolderInput) (driveconnector.FileMetadata, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return driveconnector.FileMetadata{}, errShape
	}
	if strings.TrimSpace(input.Name) == "" {
		return driveconnector.FileMetadata{}, invalidInput("name is required")
	}
	output, err := s.connector.CreateFolder(ctx, driveconnector.FolderInput{
		Name:     strings.TrimSpace(input.Name),
		ParentID: strings.TrimSpace(input.ParentID),
	})
	if err != nil {
		return driveconnector.FileMetadata{}, MapError(err)
	}
	return output, nil
}

func (s *Service) UpdateTextFile(ctx context.Context, input UpdateTextFileInput) (driveconnector.FileMetadata, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return driveconnector.FileMetadata{}, errShape
	}
	if strings.TrimSpace(input.FileID) == "" {
		return driveconnector.FileMetadata{}, invalidInput("fileId is required")
	}
	output, err := s.connector.UpdateTextFile(ctx, strings.TrimSpace(input.FileID), driveconnector.TextFileInput{
		Name:     strings.TrimSpace(input.Name),
		MimeType: strings.TrimSpace(input.MimeType),
		Content:  input.Content,
	})
	if err != nil {
		return driveconnector.FileMetadata{}, MapError(err)
	}
	return output, nil
}

func (s *Service) RenameFile(ctx context.Context, input RenameFileInput) (driveconnector.FileMetadata, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return driveconnector.FileMetadata{}, errShape
	}
	if strings.TrimSpace(input.FileID) == "" {
		return driveconnector.FileMetadata{}, invalidInput("fileId is required")
	}
	if strings.TrimSpace(input.Name) == "" {
		return driveconnector.FileMetadata{}, invalidInput("name is required")
	}
	output, err := s.connector.RenameFile(ctx, strings.TrimSpace(input.FileID), strings.TrimSpace(input.Name))
	if err != nil {
		return driveconnector.FileMetadata{}, MapError(err)
	}
	return output, nil
}

func (s *Service) MoveFile(ctx context.Context, input MoveFileInput) (driveconnector.FileMetadata, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return driveconnector.FileMetadata{}, errShape
	}
	if strings.TrimSpace(input.FileID) == "" {
		return driveconnector.FileMetadata{}, invalidInput("fileId is required")
	}
	if strings.TrimSpace(input.FolderID) == "" {
		return driveconnector.FileMetadata{}, invalidInput("folderId is required")
	}
	output, err := s.connector.MoveFile(ctx, strings.TrimSpace(input.FileID), strings.TrimSpace(input.FolderID))
	if err != nil {
		return driveconnector.FileMetadata{}, MapError(err)
	}
	return output, nil
}

func (s *Service) MoveFiles(ctx context.Context, input MoveFilesInput) (MoveFilesOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return MoveFilesOutput{}, errShape
	}
	fileIDs := cleanStringSlice(input.FileIDs)
	if len(fileIDs) == 0 {
		return MoveFilesOutput{}, invalidInput("fileIds is required")
	}
	if strings.TrimSpace(input.FolderID) == "" {
		return MoveFilesOutput{}, invalidInput("folderId is required")
	}
	output := MoveFilesOutput{Files: make([]driveconnector.FileMetadata, 0, len(fileIDs))}
	for _, fileID := range fileIDs {
		moved, err := s.connector.MoveFile(ctx, fileID, strings.TrimSpace(input.FolderID))
		if err != nil {
			errShape := MapError(err)
			output.Failures = append(output.Failures, MoveFilesFailure{FileID: fileID, Code: errShape.Code, Message: errShape.Message})
			continue
		}
		output.Files = append(output.Files, moved)
	}
	if len(output.Files) == 0 && len(output.Failures) > 0 {
		return output, &ErrorShape{Code: output.Failures[0].Code, Message: output.Failures[0].Message}
	}
	return output, nil
}

func (s *Service) ShareFile(ctx context.Context, input ShareFileInput) (ShareFileOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return ShareFileOutput{}, errShape
	}
	input.FileID = strings.TrimSpace(input.FileID)
	input.Role = normalizePermissionRole(input.Role)
	input.Type = normalizePermissionType(input.Type)
	input.EmailAddress = strings.TrimSpace(input.EmailAddress)
	input.Domain = strings.TrimSpace(input.Domain)
	if input.FileID == "" {
		return ShareFileOutput{}, invalidInput("fileId is required")
	}
	if input.Type == "user" && input.EmailAddress == "" {
		return ShareFileOutput{}, invalidInput("emailAddress is required for user permissions")
	}
	if input.Type == "domain" && input.Domain == "" {
		return ShareFileOutput{}, invalidInput("domain is required for domain permissions")
	}
	permissionID, err := s.connector.ShareFile(ctx, driveconnector.PermissionInput{
		FileID:       input.FileID,
		Role:         input.Role,
		Type:         input.Type,
		EmailAddress: input.EmailAddress,
		Domain:       input.Domain,
	})
	if err != nil {
		return ShareFileOutput{}, MapError(err)
	}
	return ShareFileOutput{
		FileID:       input.FileID,
		PermissionID: permissionID,
		Role:         input.Role,
		Type:         input.Type,
		EmailAddress: input.EmailAddress,
		Domain:       input.Domain,
	}, nil
}

func (s *Service) validateConnector() *ErrorShape {
	if s == nil || s.connector == nil {
		return &ErrorShape{Code: "INTERNAL_ERROR", Message: "drive connector is not configured"}
	}
	return nil
}

type driveTool struct {
	name    string
	service *Service
}

func newTool(name string, service *Service) driveTool {
	return driveTool{name: name, service: service}
}

func (t driveTool) Name() string { return t.name }

func (t driveTool) Description() string {
	switch t.name {
	case ToolNameSearchFiles:
		return "Search Google Drive files using a Drive query string, for example `name contains 'report' and trashed = false`."
	case ToolNameGetFileMetadata:
		return "Read metadata for one Google Drive file."
	case ToolNameExportFile:
		return "Export a Google-native Drive file such as Docs or Sheets to readable text-like content. This is read-only."
	case ToolNameDownloadFile:
		return "Download a Drive file to a local output directory. This local write requires approval."
	case ToolNameCreateTextFile:
		return "Create a text-like file in Google Drive. This external write requires approval."
	case ToolNameCreateFolder:
		return "Create a folder in Google Drive. This external write requires approval."
	case ToolNameUpdateTextFile:
		return "Update text-like content in an existing Google Drive file. This external write requires approval."
	case ToolNameRenameFile:
		return "Rename an existing Google Drive file or Google-native file such as Docs/Sheets. This updates metadata only and requires approval."
	case ToolNameMoveFile:
		return "Move an existing Google Drive file into a folder by updating its parents. This external write requires approval."
	case ToolNameMoveFiles:
		return "Move multiple existing Google Drive files into a folder by updating their parents. This external write requires approval."
	case ToolNameShareFile:
		return "Share a Google Drive file by creating a permission. This external write requires approval."
	default:
		return "Google Drive tool."
	}
}

func (t driveTool) Parameters() tools.ToolSchema {
	switch t.name {
	case ToolNameSearchFiles:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}, "maxResults": map[string]any{"type": "number"}, "pageToken": map[string]any{"type": "string"}}, "additionalProperties": false}
	case ToolNameGetFileMetadata:
		return requiredStringSchema("fileId")
	case ToolNameExportFile:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"fileId": map[string]any{"type": "string"}, "mimeType": map[string]any{"type": "string"}}, "required": []string{"fileId"}, "additionalProperties": false}
	case ToolNameDownloadFile:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"fileId": map[string]any{"type": "string"}, "outputDir": map[string]any{"type": "string"}, "filename": map[string]any{"type": "string"}}, "required": []string{"fileId", "outputDir"}, "additionalProperties": false}
	case ToolNameCreateTextFile:
		return textFileSchema([]string{"name"})
	case ToolNameCreateFolder:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}, "parentId": map[string]any{"type": "string"}}, "required": []string{"name"}, "additionalProperties": false}
	case ToolNameUpdateTextFile:
		schema := textFileSchema([]string{"fileId"})
		schema["properties"].(map[string]any)["fileId"] = map[string]any{"type": "string"}
		return schema
	case ToolNameRenameFile:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"fileId": map[string]any{"type": "string"}, "name": map[string]any{"type": "string"}}, "required": []string{"fileId", "name"}, "additionalProperties": false}
	case ToolNameMoveFile:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"fileId": map[string]any{"type": "string"}, "folderId": map[string]any{"type": "string"}}, "required": []string{"fileId", "folderId"}, "additionalProperties": false}
	case ToolNameMoveFiles:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"fileIds": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "folderId": map[string]any{"type": "string"}}, "required": []string{"fileIds", "folderId"}, "additionalProperties": false}
	case ToolNameShareFile:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"fileId": map[string]any{"type": "string"}, "role": map[string]any{"type": "string", "enum": []string{"reader", "commenter", "writer"}}, "type": map[string]any{"type": "string", "enum": []string{"user", "group", "domain"}}, "emailAddress": map[string]any{"type": "string"}, "domain": map[string]any{"type": "string"}}, "required": []string{"fileId", "role", "type"}, "additionalProperties": false}
	default:
		return tools.ToolSchema{"type": "object"}
	}
}

func (t driveTool) Capability() tools.Capability {
	switch t.name {
	case ToolNameSearchFiles, ToolNameGetFileMetadata, ToolNameExportFile:
		return tools.CapabilityReadOnly
	default:
		return tools.CapabilityMutating
	}
}

func (t driveTool) RiskLevel() tools.RiskLevel {
	switch t.name {
	case ToolNameSearchFiles, ToolNameGetFileMetadata, ToolNameExportFile:
		return tools.RiskLevelSafeRead
	case ToolNameDownloadFile:
		return tools.RiskLevelLocalWrite
	default:
		return tools.RiskLevelExternalWrite
	}
}

func (t driveTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	switch t.name {
	case ToolNameSearchFiles:
		output, errShape := t.service.SearchFiles(ctx, SearchFilesInput{Query: stringArg(call.Arguments, "query"), MaxResults: int64Arg(call.Arguments, "maxResults"), PageToken: stringArg(call.Arguments, "pageToken")})
		return outputToolResult(call, output, errShape)
	case ToolNameGetFileMetadata:
		output, errShape := t.service.GetFileMetadata(ctx, GetFileMetadataInput{FileID: stringArg(call.Arguments, "fileId")})
		return outputToolResult(call, output, errShape)
	case ToolNameExportFile:
		output, errShape := t.service.ExportFile(ctx, ExportFileInput{FileID: stringArg(call.Arguments, "fileId"), MimeType: stringArg(call.Arguments, "mimeType")})
		return outputToolResult(call, output, errShape)
	case ToolNameDownloadFile:
		output, errShape := t.service.DownloadFile(ctx, DownloadFileInput{FileID: stringArg(call.Arguments, "fileId"), OutputDir: stringArg(call.Arguments, "outputDir"), Filename: stringArg(call.Arguments, "filename")})
		return outputToolResult(call, output, errShape)
	case ToolNameCreateTextFile:
		output, errShape := t.service.CreateTextFile(ctx, CreateTextFileInput{Name: stringArg(call.Arguments, "name"), MimeType: stringArg(call.Arguments, "mimeType"), ParentID: stringArg(call.Arguments, "parentId"), Content: stringArg(call.Arguments, "content")})
		return outputToolResult(call, output, errShape)
	case ToolNameCreateFolder:
		output, errShape := t.service.CreateFolder(ctx, CreateFolderInput{Name: stringArg(call.Arguments, "name"), ParentID: stringArg(call.Arguments, "parentId")})
		return outputToolResult(call, output, errShape)
	case ToolNameUpdateTextFile:
		output, errShape := t.service.UpdateTextFile(ctx, UpdateTextFileInput{FileID: stringArg(call.Arguments, "fileId"), Name: stringArg(call.Arguments, "name"), MimeType: stringArg(call.Arguments, "mimeType"), Content: stringArg(call.Arguments, "content")})
		return outputToolResult(call, output, errShape)
	case ToolNameRenameFile:
		output, errShape := t.service.RenameFile(ctx, RenameFileInput{FileID: stringArg(call.Arguments, "fileId"), Name: stringArg(call.Arguments, "name")})
		return outputToolResult(call, output, errShape)
	case ToolNameMoveFile:
		output, errShape := t.service.MoveFile(ctx, MoveFileInput{FileID: stringArg(call.Arguments, "fileId"), FolderID: stringArg(call.Arguments, "folderId")})
		return outputToolResult(call, output, errShape)
	case ToolNameMoveFiles:
		output, errShape := t.service.MoveFiles(ctx, MoveFilesInput{FileIDs: stringSliceArg(call.Arguments, "fileIds"), FolderID: stringArg(call.Arguments, "folderId")})
		return outputToolResult(call, output, errShape)
	case ToolNameShareFile:
		output, errShape := t.service.ShareFile(ctx, ShareFileInput{FileID: stringArg(call.Arguments, "fileId"), Role: stringArg(call.Arguments, "role"), Type: stringArg(call.Arguments, "type"), EmailAddress: stringArg(call.Arguments, "emailAddress"), Domain: stringArg(call.Arguments, "domain")})
		return outputToolResult(call, output, errShape)
	default:
		return tools.ToolNotFoundResult(call)
	}
}

func RegisterTools(registry *tools.ToolRegistry, service *Service) error {
	for _, name := range []string{ToolNameSearchFiles, ToolNameGetFileMetadata, ToolNameExportFile, ToolNameDownloadFile, ToolNameCreateTextFile, ToolNameCreateFolder, ToolNameUpdateTextFile, ToolNameRenameFile, ToolNameMoveFile, ToolNameMoveFiles, ToolNameShareFile} {
		if err := registry.RegisterWithEntry(newTool(name, service), tools.ToolRegistryEntry{Owner: "integration", Group: "google_workspace"}); err != nil {
			return err
		}
	}
	return nil
}

func normalizeMaxResults(value int64) (int64, *ErrorShape) {
	if value == 0 {
		return defaultMaxResults, nil
	}
	if value < 1 || value > maxAllowedResults {
		return 0, invalidInput(fmt.Sprintf("maxResults must be between 1 and %d", maxAllowedResults))
	}
	return value, nil
}

func normalizePermissionRole(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "reader", "commenter", "writer":
		return value
	default:
		return "reader"
	}
}

func normalizePermissionType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "user", "group", "domain":
		return value
	default:
		return "user"
	}
}

func invalidInput(message string) *ErrorShape {
	return &ErrorShape{Code: "INVALID_INPUT", Message: message}
}

func MapError(err error) *ErrorShape {
	if err == nil {
		return nil
	}
	if googleconnector.IsNetworkError(err) {
		return &ErrorShape{Code: "PROVIDER_TIMEOUT", Message: "network error contacting Drive API: " + err.Error(), Retryable: true}
	}
	gerr, ok := err.(*googleapi.Error)
	if !ok {
		return &ErrorShape{Code: "INTERNAL_ERROR", Message: err.Error()}
	}
	message := googleAPIErrorMessage(gerr)
	switch {
	case gerr.Code == http.StatusUnauthorized:
		return &ErrorShape{Code: "AUTH_EXPIRED", Message: message, Retryable: true}
	case gerr.Code == http.StatusForbidden && hasMissingScopeReason(gerr):
		return &ErrorShape{Code: "AUTH_MISSING_SCOPE", Message: message}
	case gerr.Code == http.StatusBadRequest || gerr.Code == http.StatusNotFound:
		return &ErrorShape{Code: "INVALID_INPUT", Message: message}
	case gerr.Code == http.StatusTooManyRequests:
		return &ErrorShape{Code: "RATE_LIMITED", Message: message, Retryable: true}
	case gerr.Code >= 500:
		return &ErrorShape{Code: "PROVIDER_UNAVAILABLE", Message: message, Retryable: true}
	default:
		return &ErrorShape{Code: "INTERNAL_ERROR", Message: message}
	}
}

func outputToolResult(call tools.ToolCall, output any, errShape *ErrorShape) tools.ToolResult {
	if errShape != nil {
		return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: false, ContentForLLM: errShape.Code + ": " + errShape.Message, ContentForUser: errShape.Message, Error: &tools.ToolError{Code: errShape.Code, Message: errShape.Message}}
	}
	content := formatJSON(output)
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true, ContentForLLM: content, ContentForUser: content}
}

func formatJSON(output any) string {
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Sprintf("%#v", output)
	}
	return string(data)
}

func googleAPIErrorMessage(err *googleapi.Error) string {
	if strings.TrimSpace(err.Message) != "" {
		return err.Message
	}
	if strings.TrimSpace(err.Body) != "" {
		return err.Body
	}
	return err.Error()
}

func hasMissingScopeReason(err *googleapi.Error) bool {
	text := strings.ToLower(err.Message)
	return strings.Contains(text, "insufficient authentication scopes") || strings.Contains(text, "insufficient permissions")
}

func requiredStringSchema(name string) tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{name: map[string]any{"type": "string"}}, "required": []string{name}, "additionalProperties": false}
}

func textFileSchema(required []string) tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}, "mimeType": map[string]any{"type": "string"}, "parentId": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, "required": required, "additionalProperties": false}
}

func stringArg(args map[string]any, name string) string {
	if args == nil {
		return ""
	}
	value, _ := args[name].(string)
	return value
}

func stringSliceArg(args map[string]any, name string) []string {
	if args == nil {
		return nil
	}
	switch value := args[name].(type) {
	case []string:
		return cleanStringSlice(value)
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				items = append(items, text)
			}
		}
		return cleanStringSlice(items)
	case string:
		return cleanStringSlice(strings.Split(value, ","))
	default:
		return nil
	}
}

func cleanStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func int64Arg(args map[string]any, name string) int64 {
	if args == nil {
		return 0
	}
	switch value := args[name].(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func safeFilename(value string) string {
	value = filepath.Base(strings.TrimSpace(value))
	value = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, value)
	if strings.Trim(value, "._ ") == "" {
		return "drive-file"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func truncate(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return strings.TrimSpace(value[:max]) + "\n[truncated]"
}
