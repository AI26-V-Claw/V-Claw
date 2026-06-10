package drive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	gdrive "google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

const defaultTextMimeType = "text/plain"

type Client struct {
	srv *gdrive.Service
}

func NewClient(ctx context.Context, client *http.Client) (*Client, error) {
	if client == nil {
		return nil, errors.New("http client is required")
	}
	srv, err := gdrive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("create drive service: %w", err)
	}
	return &Client{srv: srv}, nil
}

type FileMetadata struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	MimeType    string   `json:"mimeType"`
	WebViewLink string   `json:"webViewLink,omitempty"`
	IconLink    string   `json:"iconLink,omitempty"`
	ModifiedAt  string   `json:"modifiedAt,omitempty"`
	Size        int64    `json:"size,omitempty"`
	Owners      []string `json:"owners,omitempty"`
	Parents     []string `json:"parents,omitempty"`
	Shared      bool     `json:"shared,omitempty"`
	Trashed     bool     `json:"trashed,omitempty"`
}

type SearchFilesOutput struct {
	Files         []FileMetadata `json:"files"`
	NextPageToken string         `json:"nextPageToken,omitempty"`
}

type FileContent struct {
	File        FileMetadata `json:"file"`
	MimeType    string       `json:"mimeType"`
	Data        []byte       `json:"-"`
	ContentText string       `json:"contentText,omitempty"`
}

type TextFileInput struct {
	Name     string
	MimeType string
	ParentID string
	Content  string
}

type PermissionInput struct {
	FileID       string
	Role         string
	Type         string
	EmailAddress string
	Domain       string
}

func (c *Client) SearchFiles(ctx context.Context, query string, pageSize int64, pageToken string) (SearchFilesOutput, error) {
	if c == nil || c.srv == nil {
		return SearchFilesOutput{}, errors.New("drive service is not configured")
	}
	call := c.srv.Files.List().
		Context(ctx).
		PageSize(pageSize).
		Fields("nextPageToken, files(id,name,mimeType,webViewLink,iconLink,modifiedTime,size,owners(emailAddress,displayName),parents,shared,trashed)")
	if strings.TrimSpace(query) != "" {
		call = call.Q(query)
	}
	if strings.TrimSpace(pageToken) != "" {
		call = call.PageToken(pageToken)
	}
	response, err := call.Do()
	if err != nil {
		return SearchFilesOutput{}, err
	}
	output := SearchFilesOutput{NextPageToken: response.NextPageToken}
	for _, file := range response.Files {
		output.Files = append(output.Files, metadataFromAPI(file))
	}
	return output, nil
}

func (c *Client) GetFileMetadata(ctx context.Context, fileID string) (FileMetadata, error) {
	if c == nil || c.srv == nil {
		return FileMetadata{}, errors.New("drive service is not configured")
	}
	file, err := c.srv.Files.Get(fileID).
		Context(ctx).
		Fields("id,name,mimeType,webViewLink,iconLink,modifiedTime,size,owners(emailAddress,displayName),parents,shared,trashed").
		Do()
	if err != nil {
		return FileMetadata{}, err
	}
	return metadataFromAPI(file), nil
}

func (c *Client) ExportFile(ctx context.Context, fileID string, mimeType string) (FileContent, error) {
	if c == nil || c.srv == nil {
		return FileContent{}, errors.New("drive service is not configured")
	}
	resp, err := c.srv.Files.Export(fileID, mimeType).Context(ctx).Download()
	if err != nil {
		return FileContent{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return FileContent{}, err
	}
	meta, _ := c.GetFileMetadata(ctx, fileID)
	return FileContent{File: meta, MimeType: mimeType, Data: data, ContentText: string(data)}, nil
}

func (c *Client) DownloadFile(ctx context.Context, fileID string) (FileContent, error) {
	if c == nil || c.srv == nil {
		return FileContent{}, errors.New("drive service is not configured")
	}
	resp, err := c.srv.Files.Get(fileID).Context(ctx).Download()
	if err != nil {
		return FileContent{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return FileContent{}, err
	}
	meta, _ := c.GetFileMetadata(ctx, fileID)
	return FileContent{File: meta, MimeType: resp.Header.Get("Content-Type"), Data: data}, nil
}

func (c *Client) CreateTextFile(ctx context.Context, input TextFileInput) (FileMetadata, error) {
	if c == nil || c.srv == nil {
		return FileMetadata{}, errors.New("drive service is not configured")
	}
	mimeType := strings.TrimSpace(input.MimeType)
	if mimeType == "" {
		mimeType = defaultTextMimeType
	}
	file := &gdrive.File{Name: input.Name, MimeType: mimeType}
	if strings.TrimSpace(input.ParentID) != "" {
		file.Parents = []string{strings.TrimSpace(input.ParentID)}
	}
	created, err := c.srv.Files.Create(file).
		Context(ctx).
		Media(strings.NewReader(input.Content), googleapi.ContentType(mimeType)).
		Fields("id,name,mimeType,webViewLink,iconLink,modifiedTime,size,owners(emailAddress,displayName),parents,shared,trashed").
		Do()
	if err != nil {
		return FileMetadata{}, err
	}
	return metadataFromAPI(created), nil
}

func (c *Client) UpdateTextFile(ctx context.Context, fileID string, input TextFileInput) (FileMetadata, error) {
	if c == nil || c.srv == nil {
		return FileMetadata{}, errors.New("drive service is not configured")
	}
	mimeType := strings.TrimSpace(input.MimeType)
	if mimeType == "" {
		mimeType = defaultTextMimeType
	}
	file := &gdrive.File{}
	if strings.TrimSpace(input.Name) != "" {
		file.Name = strings.TrimSpace(input.Name)
	}
	updated, err := c.srv.Files.Update(fileID, file).
		Context(ctx).
		Media(strings.NewReader(input.Content), googleapi.ContentType(mimeType)).
		Fields("id,name,mimeType,webViewLink,iconLink,modifiedTime,size,owners(emailAddress,displayName),parents,shared,trashed").
		Do()
	if err != nil {
		return FileMetadata{}, err
	}
	return metadataFromAPI(updated), nil
}

func (c *Client) RenameFile(ctx context.Context, fileID string, name string) (FileMetadata, error) {
	if c == nil || c.srv == nil {
		return FileMetadata{}, errors.New("drive service is not configured")
	}
	updated, err := c.srv.Files.Update(fileID, &gdrive.File{Name: strings.TrimSpace(name)}).
		Context(ctx).
		Fields("id,name,mimeType,webViewLink,iconLink,modifiedTime,size,owners(emailAddress,displayName),parents,shared,trashed").
		Do()
	if err != nil {
		return FileMetadata{}, err
	}
	return metadataFromAPI(updated), nil
}

func (c *Client) ShareFile(ctx context.Context, input PermissionInput) (string, error) {
	if c == nil || c.srv == nil {
		return "", errors.New("drive service is not configured")
	}
	permission := &gdrive.Permission{
		Type:         input.Type,
		Role:         input.Role,
		EmailAddress: input.EmailAddress,
		Domain:       input.Domain,
	}
	created, err := c.srv.Permissions.Create(input.FileID, permission).
		Context(ctx).
		SendNotificationEmail(false).
		Fields("id").
		Do()
	if err != nil {
		return "", err
	}
	return created.Id, nil
}

func metadataFromAPI(file *gdrive.File) FileMetadata {
	if file == nil {
		return FileMetadata{}
	}
	owners := make([]string, 0, len(file.Owners))
	for _, owner := range file.Owners {
		if strings.TrimSpace(owner.EmailAddress) != "" {
			owners = append(owners, owner.EmailAddress)
		} else if strings.TrimSpace(owner.DisplayName) != "" {
			owners = append(owners, owner.DisplayName)
		}
	}
	return FileMetadata{
		ID:          file.Id,
		Name:        file.Name,
		MimeType:    file.MimeType,
		WebViewLink: file.WebViewLink,
		IconLink:    file.IconLink,
		ModifiedAt:  file.ModifiedTime,
		Size:        file.Size,
		Owners:      owners,
		Parents:     append([]string(nil), file.Parents...),
		Shared:      file.Shared,
		Trashed:     file.Trashed,
	}
}
