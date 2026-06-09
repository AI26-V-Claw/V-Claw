package main

import (
	"context"
	"encoding/json"
	"fmt"

	"vclaw/internal/connectors/google"
	googleoauth "vclaw/internal/connectors/google/oauth"
	sheetsconnector "vclaw/internal/connectors/google/sheets"
	sheetstool "vclaw/internal/tools/office/sheets"
)

func runGoogleSheets(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printGoogleSheetsUsage()
		return nil
	}

	switch args[0] {
	case "get":
		fs := newGoogleFlagSet("sheets get")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		spreadsheetID := fs.String("id", "", "spreadsheet ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleSheetsService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.GetSpreadsheet(ctx, sheetstool.SpreadsheetInput{SpreadsheetID: *spreadsheetID})
		return printSheetsToolOutput(output, toolErr)

	case "list":
		fs := newGoogleFlagSet("sheets list")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		spreadsheetID := fs.String("id", "", "spreadsheet ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleSheetsService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.ListSheets(ctx, sheetstool.SpreadsheetInput{SpreadsheetID: *spreadsheetID})
		return printSheetsToolOutput(output, toolErr)

	case "read":
		fs := newGoogleFlagSet("sheets read")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		spreadsheetID := fs.String("id", "", "spreadsheet ID")
		readRange := fs.String("range", "", "A1 range, for example Sheet1!A1:D20")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleSheetsService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.ReadRange(ctx, sheetstool.ReadRangeInput{SpreadsheetID: *spreadsheetID, Range: *readRange})
		return printSheetsToolOutput(output, toolErr)

	case "create":
		fs := newGoogleFlagSet("sheets create")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		title := fs.String("title", "", "spreadsheet title")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		service, err := googleSheetsService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.CreateSpreadsheet(ctx, sheetstool.CreateSpreadsheetInput{Title: *title})
		return printSheetsToolOutput(output, toolErr)

	case "update":
		fs := newGoogleFlagSet("sheets update")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		spreadsheetID := fs.String("id", "", "spreadsheet ID")
		writeRange := fs.String("range", "", "A1 range")
		valuesJSON := fs.String("values-json", "", `JSON matrix, for example [["Name","Score"],["A",1]]`)
		valueInputOption := fs.String("value-input-option", "RAW", "RAW or USER_ENTERED")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		values, err := parseValuesJSON(*valuesJSON)
		if err != nil {
			return err
		}
		service, err := googleSheetsService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.UpdateRange(ctx, sheetstool.WriteRangeInput{SpreadsheetID: *spreadsheetID, Range: *writeRange, Values: values, ValueInputOption: *valueInputOption})
		return printSheetsToolOutput(output, toolErr)

	case "append":
		fs := newGoogleFlagSet("sheets append")
		credentialsPath, tokenPath := addGoogleAuthFlags(fs)
		spreadsheetID := fs.String("id", "", "spreadsheet ID")
		writeRange := fs.String("range", "", "A1 range")
		valuesJSON := fs.String("values-json", "", `JSON matrix, for example [["A",1],["B",2]]`)
		valueInputOption := fs.String("value-input-option", "RAW", "RAW or USER_ENTERED")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		values, err := parseValuesJSON(*valuesJSON)
		if err != nil {
			return err
		}
		service, err := googleSheetsService(ctx, *credentialsPath, *tokenPath)
		if err != nil {
			return err
		}
		output, toolErr := service.AppendRows(ctx, sheetstool.WriteRangeInput{SpreadsheetID: *spreadsheetID, Range: *writeRange, Values: values, ValueInputOption: *valueInputOption})
		return printSheetsToolOutput(output, toolErr)

	case "help", "-h", "--help":
		printGoogleSheetsUsage()
		return nil
	default:
		return fmt.Errorf("unknown google sheets command %q", args[0])
	}
}

func googleSheetsService(ctx context.Context, credentialsPath string, tokenPath string) (*sheetstool.Service, error) {
	httpClient, err := googleoauth.Client(ctx, googleoauth.Config{
		CredentialsPath: credentialsPath,
		TokenPath:       tokenPath,
		Scopes:          google.G1Scopes,
	})
	if err != nil {
		return nil, err
	}
	client, err := sheetsconnector.NewClient(ctx, httpClient)
	if err != nil {
		return nil, err
	}
	return sheetstool.NewService(client), nil
}

func parseValuesJSON(value string) ([][]interface{}, error) {
	var out [][]interface{}
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil, fmt.Errorf("values-json must be a JSON matrix: %w", err)
	}
	return out, nil
}

func printSheetsToolOutput(output any, toolErr *sheetstool.ErrorShape) error {
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

func printGoogleSheetsUsage() {
	fmt.Println(`Google Sheets commands:
  vclaw google sheets get -id SPREADSHEET_ID
      Read spreadsheet metadata.

  vclaw google sheets list -id SPREADSHEET_ID
      List sheets/tabs in a spreadsheet.

  vclaw google sheets read -id SPREADSHEET_ID -range "Sheet1!A1:D20"
      Read values from a range.

  vclaw google sheets create -title "Tracker"
      Create a spreadsheet.

  vclaw google sheets update -id SPREADSHEET_ID -range "Sheet1!A1:B2" -values-json "[[\"Name\",\"Score\"],[\"A\",1]]"
      Update a range.

  vclaw google sheets append -id SPREADSHEET_ID -range "Sheet1!A:B" -values-json "[[\"B\",2]]"
      Append rows to a range.`)
}
