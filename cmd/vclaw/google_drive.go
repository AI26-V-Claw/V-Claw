package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"vclaw/internal/connectors/google"
	driveconnector "vclaw/internal/connectors/google/drive"
	googleoauth "vclaw/internal/connectors/google/oauth"
	drivetool "vclaw/internal/tools/office/drive"
)

func runGoogleDrive(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printGoogleDriveUsage()
		return nil
	}

	switch args[0] {
	case "search":
		fs := newGoogleFlagSet("drive search")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		query := fs.String("query", "trashed = false", "Drive query string")
		maxResults := fs.Int64("max-results", 10, "number of files to return (1-50)")
		pageToken := fs.String("page-token", "", "optional Drive page token")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleDriveService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.SearchFiles(ctx, drivetool.SearchFilesInput{Query: *query, MaxResults: *maxResults, PageToken: *pageToken})
		return printDriveToolOutput(output, toolErr)

	case "get":
		fs := newGoogleFlagSet("drive get")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		fileID := fs.String("id", "", "Drive file ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleDriveService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.GetFileMetadata(ctx, drivetool.GetFileMetadataInput{FileID: *fileID})
		return printDriveToolOutput(output, toolErr)

	case "export":
		fs := newGoogleFlagSet("drive export")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		fileID := fs.String("id", "", "Drive file ID")
		mimeType := fs.String("mime-type", "text/plain", "export MIME type")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleDriveService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.ExportFile(ctx, drivetool.ExportFileInput{FileID: *fileID, MimeType: *mimeType})
		return printDriveToolOutput(output, toolErr)

	case "download":
		fs := newGoogleFlagSet("drive download")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		fileID := fs.String("id", "", "Drive file ID")
		outputDir := fs.String("output-dir", "", "local output directory")
		filename := fs.String("filename", "", "optional local filename")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleDriveService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.DownloadFile(ctx, drivetool.DownloadFileInput{FileID: *fileID, OutputDir: *outputDir, Filename: *filename})
		return printDriveToolOutput(output, toolErr)

	case "create-text":
		fs := newGoogleFlagSet("drive create-text")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		name := fs.String("name", "", "Drive file name")
		content := fs.String("content", "", "text content")
		mimeType := fs.String("mime-type", "text/plain", "file MIME type")
		parentID := fs.String("parent", "", "optional Drive parent folder ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleDriveService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.CreateTextFile(ctx, drivetool.CreateTextFileInput{Name: *name, Content: *content, MimeType: *mimeType, ParentID: *parentID})
		return printDriveToolOutput(output, toolErr)

	case "create-folder":
		fs := newGoogleFlagSet("drive create-folder")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		name := fs.String("name", "", "Drive folder name")
		parentID := fs.String("parent", "", "optional Drive parent folder ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleDriveService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.CreateFolder(ctx, drivetool.CreateFolderInput{Name: *name, ParentID: *parentID})
		return printDriveToolOutput(output, toolErr)

	case "update-text":
		fs := newGoogleFlagSet("drive update-text")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		fileID := fs.String("id", "", "Drive file ID")
		name := fs.String("name", "", "optional new file name")
		content := fs.String("content", "", "replacement text content")
		mimeType := fs.String("mime-type", "text/plain", "file MIME type")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleDriveService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.UpdateTextFile(ctx, drivetool.UpdateTextFileInput{FileID: *fileID, Name: *name, Content: *content, MimeType: *mimeType})
		return printDriveToolOutput(output, toolErr)

	case "rename":
		fs := newGoogleFlagSet("drive rename")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		fileID := fs.String("id", "", "Drive file ID")
		name := fs.String("name", "", "new file name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleDriveService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.RenameFile(ctx, drivetool.RenameFileInput{FileID: *fileID, Name: *name})
		return printDriveToolOutput(output, toolErr)

	case "move":
		fs := newGoogleFlagSet("drive move")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		fileID := fs.String("id", "", "Drive file ID")
		folderID := fs.String("folder", "", "destination Drive folder ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleDriveService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.MoveFile(ctx, drivetool.MoveFileInput{FileID: *fileID, FolderID: *folderID})
		return printDriveToolOutput(output, toolErr)

	case "move-batch":
		fs := newGoogleFlagSet("drive move-batch")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		fileIDs := fs.String("ids", "", "comma separated Drive file IDs")
		folderID := fs.String("folder", "", "destination Drive folder ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleDriveService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.MoveFiles(ctx, drivetool.MoveFilesInput{FileIDs: splitCommaList(*fileIDs), FolderID: *folderID})
		return printDriveToolOutput(output, toolErr)

	case "share":
		fs := newGoogleFlagSet("drive share")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		fileID := fs.String("id", "", "Drive file ID")
		role := fs.String("role", "reader", "permission role: reader, commenter, writer")
		permType := fs.String("type", "user", "permission type: user, group, domain")
		email := fs.String("email", "", "target email for user/group permission")
		domain := fs.String("domain", "", "target domain for domain permission")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleDriveService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.ShareFile(ctx, drivetool.ShareFileInput{FileID: *fileID, Role: *role, Type: *permType, EmailAddress: *email, Domain: *domain})
		return printDriveToolOutput(output, toolErr)

	case "help", "-h", "--help":
		printGoogleDriveUsage()
		return nil
	default:
		return fmt.Errorf("unknown google drive command %q", args[0])
	}
}

func googleDriveService(ctx context.Context, credentialsPath string, tokenPath string) (*drivetool.Service, error) {
	httpClient, err := googleoauth.Client(ctx, googleoauth.Config{
		CredentialsPath: credentialsPath,
		TokenPath:       tokenPath,
		Scopes:          google.G1Scopes,
	})
	if err != nil {
		return nil, err
	}
	client, err := driveconnector.NewClient(ctx, httpClient)
	if err != nil {
		return nil, err
	}
	return drivetool.NewService(client), nil
}

func printDriveToolOutput(output any, toolErr *drivetool.ErrorShape) error {
	if toolErr != nil {
		return fmt.Errorf("%s: %s", toolErr.Code, toolErr.Message)
	}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func printGoogleDriveUsage() {
	fmt.Println(`Google Drive commands:
  vclaw google drive search [-query "name contains 'report' and trashed = false"] [-max-results 10]
      Search Drive files.

  vclaw google drive get -id FILE_ID
      Read Drive file metadata.

  vclaw google drive export -id FILE_ID [-mime-type text/plain]
      Export a Google-native file such as Docs/Sheets.

  vclaw google drive download -id FILE_ID -output-dir C:\tmp\vclaw-drive [-filename file.bin]
      Download a Drive file to local disk.

  vclaw google drive create-text -name notes.txt -content "Hello" [-mime-type text/plain] [-parent FOLDER_ID]
      Create a text-like Drive file.

  vclaw google drive create-folder -name "New folder" [-parent FOLDER_ID]
      Create a Google Drive folder.

  vclaw google drive update-text -id FILE_ID -content "Updated" [-name notes.txt]
      Update text-like Drive file content.

  vclaw google drive rename -id FILE_ID -name "New name"
      Rename a Drive file or Google Docs/Sheets file.

  vclaw google drive move -id FILE_ID -folder FOLDER_ID
      Move a Drive file into a folder.

  vclaw google drive move-batch -ids FILE_ID1,FILE_ID2 -folder FOLDER_ID
      Move multiple Drive files into a folder.

  vclaw google drive share -id FILE_ID -role reader -type user -email user@example.com
      Share a Drive file by creating a permission.`)
}

func splitCommaList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
