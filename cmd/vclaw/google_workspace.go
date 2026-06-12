package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"vclaw/internal/connectors/google"
	gdocs "vclaw/internal/connectors/google/docs"
	gdrive "vclaw/internal/connectors/google/drive"
	googleoauth "vclaw/internal/connectors/google/oauth"
	gsheets "vclaw/internal/connectors/google/sheets"
	docstool "vclaw/internal/tools/office/docs"
	drivetool "vclaw/internal/tools/office/drive"
	sheetstool "vclaw/internal/tools/office/sheets"
)

func runGoogleDrive(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printGoogleDriveUsage()
		return nil
	}
	service, flags, err := googleDriveServiceFromArgs(ctx, "drive "+args[0], args[1:])
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		query := flags.String("query", "", "Drive query")
		mimeType := flags.String("mime-type", "", "optional MIME type filter")
		maxResults := flags.Int64("max-results", 10, "max results")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.ListFiles(ctx, drivetool.ListFilesInput{Query: *query, MimeType: *mimeType, MaxResults: *maxResults})
		return printWorkspaceOutput(output, toolErr)
	case "get":
		fileID := flags.String("id", "", "Drive file ID")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.GetFile(ctx, drivetool.GetFileInput{FileID: *fileID})
		return printWorkspaceOutput(output, toolErr)
	case "export":
		fileID := flags.String("id", "", "Drive file ID")
		mimeType := flags.String("mime-type", "text/plain", "export MIME type")
		maxBytes := flags.Int64("max-bytes", 1024*1024, "max bytes to return")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.ExportFile(ctx, drivetool.FileContentInput{FileID: *fileID, MimeType: *mimeType, MaxBytes: *maxBytes})
		return printWorkspaceOutput(output, toolErr)
	case "download":
		fileID := flags.String("id", "", "Drive file ID")
		maxBytes := flags.Int64("max-bytes", 1024*1024, "max bytes to return")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.DownloadFile(ctx, drivetool.FileContentInput{FileID: *fileID, MaxBytes: *maxBytes})
		return printWorkspaceOutput(output, toolErr)
	case "create-folder":
		name := flags.String("name", "", "folder name")
		parentIDs := flags.String("parents", "", "comma-separated parent IDs")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.CreateFolder(ctx, drivetool.CreateFolderInput{Name: *name, ParentIDs: splitCSV(*parentIDs)})
		return printWorkspaceOutput(output, toolErr)
	case "create-file":
		name := flags.String("name", "", "file name")
		mimeType := flags.String("mime-type", "text/plain", "MIME type")
		content := flags.String("content", "", "file content")
		parentIDs := flags.String("parents", "", "comma-separated parent IDs")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.CreateFile(ctx, drivetool.CreateFileInput{Name: *name, MimeType: *mimeType, Content: *content, ParentIDs: splitCSV(*parentIDs)})
		return printWorkspaceOutput(output, toolErr)
	case "upload":
		localPath := flags.String("path", "", "local file path")
		name := flags.String("name", "", "optional Drive file name")
		mimeType := flags.String("mime-type", "", "MIME type")
		parentIDs := flags.String("parents", "", "comma-separated parent IDs")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.UploadFile(ctx, drivetool.UploadFileInput{LocalPath: *localPath, Name: *name, MimeType: *mimeType, ParentIDs: splitCSV(*parentIDs)})
		return printWorkspaceOutput(output, toolErr)
	case "share":
		fileID := flags.String("id", "", "Drive file ID")
		shareType := flags.String("type", "user", "user, group, domain, or anyone")
		role := flags.String("role", "reader", "reader, commenter, or writer")
		email := flags.String("email", "", "email for user/group permissions")
		notify := flags.Bool("notify", false, "send notification email")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.ShareFile(ctx, drivetool.ShareFileInput{FileID: *fileID, Type: *shareType, Role: *role, EmailAddress: *email, SendNotificationEmail: *notify})
		return printWorkspaceOutput(output, toolErr)
	case "move":
		fileID := flags.String("id", "", "Drive file ID")
		targetParentID := flags.String("target-parent", "", "destination parent folder ID")
		removeParentIDs := flags.String("remove-parents", "", "optional comma-separated parent IDs to remove")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.MoveFile(ctx, drivetool.MoveFileInput{FileID: *fileID, TargetParentID: *targetParentID, RemoveParentIDs: splitCSV(*removeParentIDs)})
		return printWorkspaceOutput(output, toolErr)
	case "permissions":
		fileID := flags.String("id", "", "Drive file ID")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.ListPermissions(ctx, drivetool.FileIDInput{FileID: *fileID})
		return printWorkspaceOutput(output, toolErr)
	case "revoke":
		fileID := flags.String("id", "", "Drive file ID")
		permissionID := flags.String("permission-id", "", "permission ID")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.RevokePermission(ctx, drivetool.RevokePermissionInput{FileID: *fileID, PermissionID: *permissionID})
		return printWorkspaceOutput(output, toolErr)
	case "trash", "untrash":
		fileID := flags.String("id", "", "Drive file ID")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if args[0] == "trash" {
			output, toolErr := service.TrashFile(ctx, drivetool.FileIDInput{FileID: *fileID})
			return printWorkspaceOutput(output, toolErr)
		}
		output, toolErr := service.UntrashFile(ctx, drivetool.FileIDInput{FileID: *fileID})
		return printWorkspaceOutput(output, toolErr)
	default:
		return fmt.Errorf("unknown google drive command %q", args[0])
	}
}

func runGoogleDocs(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printGoogleDocsUsage()
		return nil
	}
	service, flags, err := googleDocsServiceFromArgs(ctx, "docs "+args[0], args[1:])
	if err != nil {
		return err
	}
	switch args[0] {
	case "get":
		documentID := flags.String("id", "", "document ID")
		full := flags.Bool("full", false, "return full text")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.GetDocument(ctx, docstool.GetDocumentInput{DocumentID: *documentID, Full: *full})
		return printWorkspaceOutput(output, toolErr)
	case "create":
		title := flags.String("title", "", "document title")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.CreateDocument(ctx, docstool.CreateDocumentInput{Title: *title})
		return printWorkspaceOutput(output, toolErr)
	case "append":
		documentID := flags.String("id", "", "document ID")
		text := flags.String("text", "", "text to append")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.AppendText(ctx, docstool.AppendTextInput{DocumentID: *documentID, Text: *text})
		return printWorkspaceOutput(output, toolErr)
	case "replace":
		documentID := flags.String("id", "", "document ID")
		oldText := flags.String("old", "", "text to find")
		newText := flags.String("new", "", "replacement text")
		matchCase := flags.Bool("match-case", false, "match case")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.ReplaceText(ctx, docstool.ReplaceTextInput{DocumentID: *documentID, OldText: *oldText, NewText: *newText, MatchCase: *matchCase})
		return printWorkspaceOutput(output, toolErr)
	case "insert":
		documentID := flags.String("id", "", "document ID")
		index := flags.Int64("index", 1, "Google Docs structural index")
		text := flags.String("text", "", "text to insert")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.InsertText(ctx, docstool.InsertTextInput{DocumentID: *documentID, Index: *index, Text: *text})
		return printWorkspaceOutput(output, toolErr)
	case "delete":
		documentID := flags.String("id", "", "document ID")
		startIndex := flags.Int64("start-index", 1, "start index")
		endIndex := flags.Int64("end-index", 1, "end index")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.DeleteContent(ctx, docstool.DeleteContentInput{DocumentID: *documentID, StartIndex: *startIndex, EndIndex: *endIndex})
		return printWorkspaceOutput(output, toolErr)
	default:
		return fmt.Errorf("unknown google docs command %q", args[0])
	}
}

func runGoogleSheets(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printGoogleSheetsUsage()
		return nil
	}
	service, flags, err := googleSheetsServiceFromArgs(ctx, "sheets "+args[0], args[1:])
	if err != nil {
		return err
	}
	switch args[0] {
	case "get":
		spreadsheetID := flags.String("id", "", "spreadsheet ID")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.GetSpreadsheet(ctx, sheetstool.GetSpreadsheetInput{SpreadsheetID: *spreadsheetID})
		return printWorkspaceOutput(output, toolErr)
	case "read":
		spreadsheetID := flags.String("id", "", "spreadsheet ID")
		readRange := flags.String("range", "", "A1 range")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.ReadValues(ctx, sheetstool.ReadValuesInput{SpreadsheetID: *spreadsheetID, Range: *readRange})
		return printWorkspaceOutput(output, toolErr)
	case "batch-get":
		spreadsheetID := flags.String("id", "", "spreadsheet ID")
		ranges := flags.String("ranges", "", "comma-separated A1 ranges")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.BatchGetValues(ctx, sheetstool.BatchGetValuesInput{SpreadsheetID: *spreadsheetID, Ranges: splitCSV(*ranges)})
		return printWorkspaceOutput(output, toolErr)
	case "create":
		title := flags.String("title", "", "spreadsheet title")
		sheetTitles := flags.String("sheets", "", "comma-separated sheet titles")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.CreateSpreadsheet(ctx, sheetstool.CreateSpreadsheetInput{Title: *title, SheetTitles: splitCSV(*sheetTitles)})
		return printWorkspaceOutput(output, toolErr)
	case "update", "append":
		spreadsheetID := flags.String("id", "", "spreadsheet ID")
		writeRange := flags.String("range", "", "A1 range")
		valuesJSON := flags.String("values", "[]", "JSON array of rows")
		valueInputOption := flags.String("value-input-option", "USER_ENTERED", "USER_ENTERED or RAW")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		values, err := parseRowsJSON(*valuesJSON)
		if err != nil {
			return err
		}
		input := sheetstool.ValuesInput{SpreadsheetID: *spreadsheetID, Range: *writeRange, Values: values, ValueInputOption: *valueInputOption}
		if args[0] == "append" {
			output, toolErr := service.AppendValues(ctx, input)
			return printWorkspaceOutput(output, toolErr)
		}
		output, toolErr := service.UpdateValues(ctx, input)
		return printWorkspaceOutput(output, toolErr)
	case "batch-update":
		spreadsheetID := flags.String("id", "", "spreadsheet ID")
		rangesJSON := flags.String("ranges", "{}", "JSON object mapping A1 ranges to row arrays")
		valueInputOption := flags.String("value-input-option", "USER_ENTERED", "USER_ENTERED or RAW")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		ranges, err := parseRangesJSON(*rangesJSON)
		if err != nil {
			return err
		}
		output, toolErr := service.BatchUpdateValues(ctx, sheetstool.BatchValuesInput{SpreadsheetID: *spreadsheetID, Ranges: ranges, ValueInputOption: *valueInputOption})
		return printWorkspaceOutput(output, toolErr)
	case "clear":
		spreadsheetID := flags.String("id", "", "spreadsheet ID")
		clearRange := flags.String("range", "", "A1 range")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.ClearValues(ctx, sheetstool.ReadValuesInput{SpreadsheetID: *spreadsheetID, Range: *clearRange})
		return printWorkspaceOutput(output, toolErr)
	case "add-sheet":
		spreadsheetID := flags.String("id", "", "spreadsheet ID")
		title := flags.String("title", "", "sheet title")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.AddSheet(ctx, sheetstool.SheetTitleInput{SpreadsheetID: *spreadsheetID, Title: *title})
		return printWorkspaceOutput(output, toolErr)
	case "rename-sheet":
		spreadsheetID := flags.String("id", "", "spreadsheet ID")
		sheetID := flags.Int64("sheet-id", 0, "sheet tab ID")
		title := flags.String("title", "", "new sheet title")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.RenameSheet(ctx, sheetstool.RenameSheetInput{SpreadsheetID: *spreadsheetID, SheetID: *sheetID, Title: *title})
		return printWorkspaceOutput(output, toolErr)
	case "delete-sheet":
		spreadsheetID := flags.String("id", "", "spreadsheet ID")
		sheetID := flags.Int64("sheet-id", 0, "sheet tab ID")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.DeleteSheet(ctx, sheetstool.SheetIDInput{SpreadsheetID: *spreadsheetID, SheetID: *sheetID})
		return printWorkspaceOutput(output, toolErr)
	case "duplicate-sheet":
		spreadsheetID := flags.String("id", "", "spreadsheet ID")
		sourceSheetID := flags.Int64("source-sheet-id", 0, "source sheet tab ID")
		newTitle := flags.String("new-title", "", "new sheet title")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		output, toolErr := service.DuplicateSheet(ctx, sheetstool.DuplicateSheetInput{SpreadsheetID: *spreadsheetID, SourceSheetID: *sourceSheetID, NewTitle: *newTitle})
		return printWorkspaceOutput(output, toolErr)
	default:
		return fmt.Errorf("unknown google sheets command %q", args[0])
	}
}

func googleDriveServiceFromArgs(ctx context.Context, name string, args []string) (*drivetool.Service, *flag.FlagSet, error) {
	_ = args
	fs := newGoogleFlagSet(name)
	credentialsPath, tokenPath := addGoogleAuthFlags(fs)
	httpClient, err := googleoauth.Client(ctx, googleoauth.Config{CredentialsPath: *credentialsPath, TokenPath: *tokenPath, Scopes: google.G1Scopes})
	if err != nil {
		return nil, nil, err
	}
	return drivetool.NewService(gdrive.NewClient(httpClient)), fs, nil
}

func googleDocsServiceFromArgs(ctx context.Context, name string, args []string) (*docstool.Service, *flag.FlagSet, error) {
	_ = args
	fs := newGoogleFlagSet(name)
	credentialsPath, tokenPath := addGoogleAuthFlags(fs)
	httpClient, err := googleoauth.Client(ctx, googleoauth.Config{CredentialsPath: *credentialsPath, TokenPath: *tokenPath, Scopes: google.G1Scopes})
	if err != nil {
		return nil, nil, err
	}
	return docstool.NewService(gdocs.NewClient(httpClient)), fs, nil
}

func googleSheetsServiceFromArgs(ctx context.Context, name string, args []string) (*sheetstool.Service, *flag.FlagSet, error) {
	_ = args
	fs := newGoogleFlagSet(name)
	credentialsPath, tokenPath := addGoogleAuthFlags(fs)
	httpClient, err := googleoauth.Client(ctx, googleoauth.Config{CredentialsPath: *credentialsPath, TokenPath: *tokenPath, Scopes: google.G1Scopes})
	if err != nil {
		return nil, nil, err
	}
	return sheetstool.NewService(gsheets.NewClient(httpClient)), fs, nil
}

func printWorkspaceOutput(output any, toolErr any) error {
	if !isNilToolErr(toolErr) {
		data, err := json.Marshal(toolErr)
		if err != nil {
			return fmt.Errorf("%v", toolErr)
		}
		return fmt.Errorf("%s", string(data))
	}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func isNilToolErr(toolErr any) bool {
	if toolErr == nil {
		return true
	}
	switch v := toolErr.(type) {
	case *drivetool.ErrorShape:
		return v == nil
	case *docstool.ErrorShape:
		return v == nil
	case *sheetstool.ErrorShape:
		return v == nil
	default:
		return false
	}
}

func parseRowsJSON(value string) ([][]any, error) {
	var rows [][]any
	if err := json.Unmarshal([]byte(value), &rows); err != nil {
		return nil, fmt.Errorf("parse -values JSON: %w", err)
	}
	return rows, nil
}

func parseRangesJSON(value string) (map[string][][]any, error) {
	var ranges map[string][][]any
	if err := json.Unmarshal([]byte(value), &ranges); err != nil {
		return nil, fmt.Errorf("parse -ranges JSON: %w", err)
	}
	return ranges, nil
}

func printGoogleDriveUsage() {
	fmt.Println("Usage: vclaw google drive <list|get|export|download|create-folder|create-file|upload|share|move|permissions|revoke|trash|untrash> [flags]")
}

func printGoogleDocsUsage() {
	fmt.Println("Usage: vclaw google docs <get|create|append|replace|insert|delete> [flags]")
}

func printGoogleSheetsUsage() {
	fmt.Println("Usage: vclaw google sheets <get|read|batch-get|create|update|batch-update|append|clear|add-sheet|rename-sheet|delete-sheet|duplicate-sheet> [flags]")
}
