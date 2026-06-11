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
	ToolNameCreateFolder       = "drive.createFolder"
	ToolNameUpdateFileMetadata = "drive.updateFileMetadata"
	ToolNameShareFile          = "drive.shareFile"
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
	{Name: ToolNameCreateFolder, Owner: "integration", Description: "Create a Google Drive folder.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameUpdateFileMetadata, Owner: "integration", Description: "Update Google Drive file metadata.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameShareFile, Owner: "integration", Description: "Share a Google Drive file.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameMoveFile, Owner: "integration", Description: "Move a Google Drive file or folder to another folder.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameTrashFile, Owner: "integration", Description: "Move a Google Drive file or folder to trash.", DefaultRiskLevel: "destructive", RequiresApproval: true},
	{Name: ToolNameUntrashFile, Owner: "integration", Description: "Restore a Google Drive file or folder from trash.", DefaultRiskLevel: "external_write", RequiresApproval: true},
}

type Connector interface {
	ListFiles(ctx context.Context, query, mimeType string, maxResults int64, pageToken string) (gdrive.ListFilesOutput, error)
	GetFile(ctx context.Context, fileID string) (gdrive.FileSummary, error)
	CreateFolder(ctx context.Context, name string, parentIDs []string) (gdrive.FileSummary, error)
	UpdateFileMetadata(ctx context.Context, fileID string, input gdrive.UpdateFileMetadataInput) (gdrive.FileSummary, error)
	ShareFile(ctx context.Context, fileID string, input gdrive.ShareFileInput) (gdrive.PermissionSummary, error)
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
	case ToolNameCreateFolder:
		return "Create a folder in Google Drive. Requires human approval before execution."
	case ToolNameUpdateFileMetadata:
		return "Update Google Drive file metadata only: name, description, or starred state. Do not use this tool to move files or change folders/parents; use drive.moveFile for move requests. Requires human approval before execution."
	case ToolNameShareFile:
		return "Share a Google Drive file by creating a permission. Requires human approval before execution."
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
	case ToolNameCreateFolder:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"name":      map[string]any{"type": "string"},
			"parentIds": arrayStringSchema(),
		}, "required": []string{"name"}, "additionalProperties": false}
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
	default:
		return tools.CapabilityMutating
	}
}

func (t DriveTool) RiskLevel() tools.RiskLevel {
	switch t.name {
	case ToolNameListFiles, ToolNameGetFile:
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
	case ToolNameCreateFolder:
		output, errShape := t.service.CreateFolder(ctx, CreateFolderInput{Name: stringArg(call.Arguments, "name"), ParentIDs: stringSliceArg(call.Arguments, "parentIds")})
		return outputToolResult(call, map[string]any{"File": output}, errShape)
	case ToolNameUpdateFileMetadata:
		output, errShape := t.service.UpdateFileMetadata(ctx, UpdateFileMetadataInput{FileID: stringArg(call.Arguments, "fileId"), Name: stringArg(call.Arguments, "name"), Description: stringArg(call.Arguments, "description"), Starred: optionalBoolArg(call.Arguments, "starred")})
		return outputToolResult(call, map[string]any{"File": output}, errShape)
	case ToolNameShareFile:
		output, errShape := t.service.ShareFile(ctx, ShareFileInput{FileID: stringArg(call.Arguments, "fileId"), Type: stringArg(call.Arguments, "type"), Role: stringArg(call.Arguments, "role"), EmailAddress: stringArg(call.Arguments, "emailAddress"), AllowFileDiscovery: boolArg(call.Arguments, "allowFileDiscovery"), SendNotificationEmail: boolArg(call.Arguments, "sendNotificationEmail")})
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
	for _, name := range []string{ToolNameListFiles, ToolNameGetFile, ToolNameCreateFolder, ToolNameUpdateFileMetadata, ToolNameShareFile, ToolNameMoveFile, ToolNameTrashFile, ToolNameUntrashFile} {
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
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true, ContentForLLM: string(data), ContentForUser: string(data)}
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
