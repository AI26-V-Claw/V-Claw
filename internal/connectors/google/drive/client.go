package drive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"vclaw/internal/connectors/google/common"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

const (
	FolderMimeType     = "application/vnd.google-apps.folder"
	defaultContentMIME = "text/plain"
	maxContentBytes    = int64(10 * 1024 * 1024)
)

type Client struct {
	httpClient *http.Client
}

func NewClient(httpClient *http.Client) *Client {
	return &Client{httpClient: httpClient}
}

type FileSummary struct {
	ID           string
	Name         string
	MimeType     string
	Description  string
	WebViewLink  string
	IconLink     string
	Owners       []string
	ModifiedTime string
	Size         int64
	Parents      []string
	Starred      bool
	Trashed      bool
}

type ListFilesOutput struct {
	Files         []FileSummary
	NextPageToken string
}

type UpdateFileMetadataInput struct {
	Name        string
	Description string
	Starred     *bool
}

type ShareFileInput struct {
	Type                  string
	Role                  string
	EmailAddress          string
	AllowFileDiscovery    bool
	SendNotificationEmail bool
}

type PermissionSummary struct {
	ID           string
	Type         string
	Role         string
	EmailAddress string
}

type FileContentOutput struct {
	File      FileSummary
	MimeType  string
	Content   string
	Size      int64
	Truncated bool
}

func (c *Client) ListFiles(ctx context.Context, query, mimeType string, maxResults int64, pageToken string) (ListFilesOutput, error) {
	return ListFiles(ctx, c.httpClient, query, mimeType, maxResults, pageToken)
}

func (c *Client) GetFile(ctx context.Context, fileID string) (FileSummary, error) {
	return GetFile(ctx, c.httpClient, fileID)
}

func (c *Client) CreateFolder(ctx context.Context, name string, parentIDs []string) (FileSummary, error) {
	return CreateFolder(ctx, c.httpClient, name, parentIDs)
}

func (c *Client) CreateFile(ctx context.Context, name string, mimeType string, content string, parentIDs []string) (FileSummary, error) {
	return CreateFile(ctx, c.httpClient, name, mimeType, strings.NewReader(content), parentIDs)
}

func (c *Client) UploadFile(ctx context.Context, localPath string, name string, mimeType string, parentIDs []string) (FileSummary, error) {
	return UploadFile(ctx, c.httpClient, localPath, name, mimeType, parentIDs)
}

func (c *Client) ExportFile(ctx context.Context, fileID string, mimeType string, maxBytes int64) (FileContentOutput, error) {
	return ExportFile(ctx, c.httpClient, fileID, mimeType, maxBytes)
}

func (c *Client) DownloadFile(ctx context.Context, fileID string, maxBytes int64) (FileContentOutput, error) {
	return DownloadFile(ctx, c.httpClient, fileID, maxBytes)
}

func (c *Client) UpdateFileMetadata(ctx context.Context, fileID string, input UpdateFileMetadataInput) (FileSummary, error) {
	return UpdateFileMetadata(ctx, c.httpClient, fileID, input)
}

func (c *Client) ShareFile(ctx context.Context, fileID string, input ShareFileInput) (PermissionSummary, error) {
	return ShareFile(ctx, c.httpClient, fileID, input)
}

func (c *Client) ListPermissions(ctx context.Context, fileID string) ([]PermissionSummary, error) {
	return ListPermissions(ctx, c.httpClient, fileID)
}

func (c *Client) RevokePermission(ctx context.Context, fileID string, permissionID string) (PermissionSummary, error) {
	return RevokePermission(ctx, c.httpClient, fileID, permissionID)
}

func (c *Client) MoveFile(ctx context.Context, fileID string, targetParentID string, removeParentIDs []string) (FileSummary, error) {
	return MoveFile(ctx, c.httpClient, fileID, targetParentID, removeParentIDs)
}

func (c *Client) TrashFile(ctx context.Context, fileID string) (FileSummary, error) {
	return SetFileTrashed(ctx, c.httpClient, fileID, true)
}

func (c *Client) UntrashFile(ctx context.Context, fileID string) (FileSummary, error) {
	return SetFileTrashed(ctx, c.httpClient, fileID, false)
}

func ListFiles(ctx context.Context, client *http.Client, query, mimeType string, maxResults int64, pageToken string) (ListFilesOutput, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return ListFilesOutput{}, err
	}
	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > 50 {
		maxResults = 50
	}

	q := normalizeDriveListQuery(query)
	if mt := strings.TrimSpace(mimeType); mt != "" {
		mimeQuery := fmt.Sprintf("mimeType = '%s'", escapeDriveQueryValue(mt))
		if q == "" {
			q = mimeQuery
		} else {
			q = "(" + q + ") and " + mimeQuery
		}
	}

	call := service.Files.List().
		PageSize(maxResults).
		SupportsAllDrives(true).
		IncludeItemsFromAllDrives(true).
		Fields("nextPageToken, files(id, name, mimeType, description, webViewLink, iconLink, owners(emailAddress, displayName), modifiedTime, size, parents, starred, trashed)").
		OrderBy("modifiedTime desc")
	if q != "" {
		call = call.Q(q)
	}
	if strings.TrimSpace(pageToken) != "" {
		call = call.PageToken(pageToken)
	}

	response, err := call.Do()
	if err != nil {
		return ListFilesOutput{}, common.MapError(err)
	}
	output := ListFilesOutput{NextPageToken: response.NextPageToken}
	for _, file := range response.Files {
		output.Files = append(output.Files, fileSummaryFromAPI(file))
	}
	return output, nil
}

func GetFile(ctx context.Context, client *http.Client, fileID string) (FileSummary, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return FileSummary{}, err
	}
	file, err := service.Files.Get(fileID).
		Fields("id, name, mimeType, description, webViewLink, iconLink, owners(emailAddress, displayName), modifiedTime, size, parents, starred, trashed").
		Do()
	if err != nil {
		return FileSummary{}, common.MapError(err)
	}
	return fileSummaryFromAPI(file), nil
}

func CreateFolder(ctx context.Context, client *http.Client, name string, parentIDs []string) (FileSummary, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return FileSummary{}, err
	}
	file := &drive.File{
		Name:     strings.TrimSpace(name),
		MimeType: FolderMimeType,
		Parents:  cleanStrings(parentIDs),
	}
	created, err := service.Files.Create(file).
		SupportsAllDrives(true).
		Fields("id, name, mimeType, webViewLink, parents").
		Do()
	if err != nil {
		return FileSummary{}, common.MapError(err)
	}
	return fileSummaryFromAPI(created), nil
}

func CreateFile(ctx context.Context, client *http.Client, name string, mimeType string, content io.Reader, parentIDs []string) (FileSummary, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return FileSummary{}, err
	}
	if strings.TrimSpace(mimeType) == "" {
		mimeType = defaultContentMIME
	}
	file := &drive.File{
		Name:     strings.TrimSpace(name),
		MimeType: strings.TrimSpace(mimeType),
		Parents:  cleanStrings(parentIDs),
	}
	created, err := service.Files.Create(file).
		SupportsAllDrives(true).
		Media(content).
		Fields("id, name, mimeType, description, webViewLink, iconLink, owners(emailAddress, displayName), modifiedTime, size, parents, starred, trashed").
		Do()
	if err != nil {
		return FileSummary{}, common.MapError(err)
	}
	return fileSummaryFromAPI(created), nil
}

func UploadFile(ctx context.Context, client *http.Client, localPath string, name string, mimeType string, parentIDs []string) (FileSummary, error) {
	path := strings.TrimSpace(localPath)
	file, err := os.Open(path)
	if err != nil {
		return FileSummary{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return FileSummary{}, err
	}
	if info.Size() > maxContentBytes {
		return FileSummary{}, fmt.Errorf("file exceeds %d byte upload limit", maxContentBytes)
	}
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(path)
	}
	return CreateFile(ctx, client, name, mimeType, file, parentIDs)
}

func ExportFile(ctx context.Context, client *http.Client, fileID string, mimeType string, maxBytes int64) (FileContentOutput, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return FileContentOutput{}, err
	}
	if strings.TrimSpace(mimeType) == "" {
		mimeType = defaultContentMIME
	}
	file, err := service.Files.Get(fileID).
		Fields("id, name, mimeType, description, webViewLink, iconLink, owners(emailAddress, displayName), modifiedTime, size, parents, starred, trashed").
		Do()
	if err != nil {
		return FileContentOutput{}, common.MapError(err)
	}
	response, err := service.Files.Export(fileID, mimeType).Download()
	if err != nil {
		return FileContentOutput{}, common.MapError(err)
	}
	defer response.Body.Close()
	content, size, truncated, err := readLimited(response.Body, maxBytes)
	if err != nil {
		return FileContentOutput{}, err
	}
	return FileContentOutput{File: fileSummaryFromAPI(file), MimeType: mimeType, Content: string(content), Size: size, Truncated: truncated}, nil
}

func DownloadFile(ctx context.Context, client *http.Client, fileID string, maxBytes int64) (FileContentOutput, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return FileContentOutput{}, err
	}
	file, err := service.Files.Get(fileID).
		Fields("id, name, mimeType, description, webViewLink, iconLink, owners(emailAddress, displayName), modifiedTime, size, parents, starred, trashed").
		Do()
	if err != nil {
		return FileContentOutput{}, common.MapError(err)
	}
	response, err := service.Files.Get(fileID).Download()
	if err != nil {
		return FileContentOutput{}, common.MapError(err)
	}
	defer response.Body.Close()
	content, size, truncated, err := readLimited(response.Body, maxBytes)
	if err != nil {
		return FileContentOutput{}, err
	}
	return FileContentOutput{File: fileSummaryFromAPI(file), MimeType: file.MimeType, Content: string(content), Size: size, Truncated: truncated}, nil
}

func UpdateFileMetadata(ctx context.Context, client *http.Client, fileID string, input UpdateFileMetadataInput) (FileSummary, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return FileSummary{}, err
	}
	file := &drive.File{
		Name:        strings.TrimSpace(input.Name),
		Description: input.Description,
	}
	if input.Starred != nil {
		file.Starred = *input.Starred
		file.ForceSendFields = append(file.ForceSendFields, "Starred")
	}
	updated, err := service.Files.Update(fileID, file).
		Fields("id, name, mimeType, description, webViewLink, iconLink, modifiedTime, parents, starred, trashed").
		Do()
	if err != nil {
		return FileSummary{}, common.MapError(err)
	}
	return fileSummaryFromAPI(updated), nil
}

func ShareFile(ctx context.Context, client *http.Client, fileID string, input ShareFileInput) (PermissionSummary, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return PermissionSummary{}, err
	}
	permission := &drive.Permission{
		Type:               strings.TrimSpace(input.Type),
		Role:               strings.TrimSpace(input.Role),
		EmailAddress:       strings.TrimSpace(input.EmailAddress),
		AllowFileDiscovery: input.AllowFileDiscovery,
	}
	created, err := service.Permissions.Create(fileID, permission).
		SendNotificationEmail(input.SendNotificationEmail).
		Fields("id, type, role, emailAddress").
		Do()
	if err != nil {
		return PermissionSummary{}, common.MapError(err)
	}
	return PermissionSummary{
		ID:           created.Id,
		Type:         created.Type,
		Role:         created.Role,
		EmailAddress: created.EmailAddress,
	}, nil
}

func ListPermissions(ctx context.Context, client *http.Client, fileID string) ([]PermissionSummary, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return nil, err
	}
	response, err := service.Permissions.List(fileID).
		Fields("permissions(id, type, role, emailAddress)").
		Do()
	if err != nil {
		return nil, common.MapError(err)
	}
	out := make([]PermissionSummary, 0, len(response.Permissions))
	for _, permission := range response.Permissions {
		out = append(out, permissionSummaryFromAPI(permission))
	}
	return out, nil
}

func RevokePermission(ctx context.Context, client *http.Client, fileID string, permissionID string) (PermissionSummary, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return PermissionSummary{}, err
	}
	permission, err := service.Permissions.Get(fileID, permissionID).
		Fields("id, type, role, emailAddress").
		Do()
	if err != nil {
		return PermissionSummary{}, common.MapError(err)
	}
	if err := service.Permissions.Delete(fileID, permissionID).Do(); err != nil {
		return PermissionSummary{}, common.MapError(err)
	}
	return permissionSummaryFromAPI(permission), nil
}

func MoveFile(ctx context.Context, client *http.Client, fileID string, targetParentID string, removeParentIDs []string) (FileSummary, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return FileSummary{}, err
	}
	removeParentIDs = cleanStrings(removeParentIDs)
	if len(removeParentIDs) == 0 {
		file, err := service.Files.Get(fileID).Fields("parents").Do()
		if err != nil {
			return FileSummary{}, common.MapError(err)
		}
		removeParentIDs = cleanStrings(file.Parents)
	}
	updated, err := service.Files.Update(fileID, &drive.File{}).
		AddParents(strings.TrimSpace(targetParentID)).
		RemoveParents(strings.Join(removeParentIDs, ",")).
		Fields("id, name, mimeType, description, webViewLink, iconLink, owners(emailAddress, displayName), modifiedTime, size, parents, starred, trashed").
		Do()
	if err != nil {
		return FileSummary{}, common.MapError(err)
	}
	return fileSummaryFromAPI(updated), nil
}

func SetFileTrashed(ctx context.Context, client *http.Client, fileID string, trashed bool) (FileSummary, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return FileSummary{}, err
	}
	updated, err := service.Files.Update(fileID, &drive.File{
		Trashed:         trashed,
		ForceSendFields: []string{"Trashed"},
	}).
		Fields("id, name, mimeType, description, webViewLink, iconLink, owners(emailAddress, displayName), modifiedTime, size, parents, starred, trashed").
		Do()
	if err != nil {
		return FileSummary{}, common.MapError(err)
	}
	return fileSummaryFromAPI(updated), nil
}

func serviceFromClient(ctx context.Context, client *http.Client) (*drive.Service, error) {
	if client == nil {
		return nil, errors.New("http client is required")
	}
	service, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("create drive service: %w", err)
	}
	return service, nil
}

func fileSummaryFromAPI(file *drive.File) FileSummary {
	if file == nil {
		return FileSummary{}
	}
	owners := make([]string, 0, len(file.Owners))
	for _, owner := range file.Owners {
		if strings.TrimSpace(owner.EmailAddress) != "" {
			owners = append(owners, owner.EmailAddress)
		} else if strings.TrimSpace(owner.DisplayName) != "" {
			owners = append(owners, owner.DisplayName)
		}
	}
	return FileSummary{
		ID:           file.Id,
		Name:         file.Name,
		MimeType:     file.MimeType,
		Description:  file.Description,
		WebViewLink:  file.WebViewLink,
		IconLink:     file.IconLink,
		Owners:       owners,
		ModifiedTime: file.ModifiedTime,
		Size:         file.Size,
		Parents:      append([]string(nil), file.Parents...),
		Starred:      file.Starred,
		Trashed:      file.Trashed,
	}
}

func permissionSummaryFromAPI(permission *drive.Permission) PermissionSummary {
	if permission == nil {
		return PermissionSummary{}
	}
	return PermissionSummary{
		ID:           permission.Id,
		Type:         permission.Type,
		Role:         permission.Role,
		EmailAddress: permission.EmailAddress,
	}
}

func readLimited(reader io.Reader, limit int64) ([]byte, int64, bool, error) {
	if limit <= 0 || limit > maxContentBytes {
		limit = maxContentBytes
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, 0, false, err
	}
	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:limit]
	}
	return data, int64(len(data)), truncated, nil
}

func escapeDriveQueryValue(value string) string {
	return strings.ReplaceAll(value, "'", "\\'")
}

func normalizeDriveListQuery(query string) string {
	q := strings.TrimSpace(query)
	if q == "" || looksLikeDriveQuery(q) {
		return q
	}
	return fmt.Sprintf("name contains '%s' and trashed = false", escapeDriveQueryValue(q))
}

func looksLikeDriveQuery(query string) bool {
	lower := strings.ToLower(strings.TrimSpace(query))
	if lower == "" {
		return false
	}
	for _, marker := range []string{
		"=", "!=", "<", ">", " contains ", " in ", " and ", " or ", " not ",
		"name ", "mimetype", "trashed", "fulltext", "modifiedtime",
		"viewedbyme", "starred", "parents", "owners", "writers", "readers",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return strings.Contains(lower, "'")
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
