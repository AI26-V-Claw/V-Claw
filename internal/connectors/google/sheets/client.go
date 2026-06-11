package sheets

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"vclaw/internal/connectors/google/common"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type Client struct {
	httpClient *http.Client
}

func NewClient(httpClient *http.Client) *Client {
	return &Client{httpClient: httpClient}
}

type SpreadsheetSummary struct {
	ID             string
	Title          string
	SpreadsheetURL string
	Sheets         []SheetSummary
}

type SheetSummary struct {
	ID    int64
	Title string
	Index int64
}

type ValuesOutput struct {
	SpreadsheetID  string
	Range          string
	MajorDimension string
	Values         [][]any
}

type WriteValuesOutput struct {
	SpreadsheetID  string
	UpdatedRange   string
	UpdatedRows    int64
	UpdatedColumns int64
	UpdatedCells   int64
}

type AppendValuesOutput struct {
	SpreadsheetID  string
	TableRange     string
	UpdatedRange   string
	UpdatedRows    int64
	UpdatedColumns int64
	UpdatedCells   int64
}

func (c *Client) GetSpreadsheet(ctx context.Context, spreadsheetID string) (SpreadsheetSummary, error) {
	return GetSpreadsheet(ctx, c.httpClient, spreadsheetID)
}

func (c *Client) ReadValues(ctx context.Context, spreadsheetID string, readRange string) (ValuesOutput, error) {
	return ReadValues(ctx, c.httpClient, spreadsheetID, readRange)
}

func (c *Client) CreateSpreadsheet(ctx context.Context, title string, sheetTitles []string) (SpreadsheetSummary, error) {
	return CreateSpreadsheet(ctx, c.httpClient, title, sheetTitles)
}

func (c *Client) UpdateValues(ctx context.Context, spreadsheetID string, writeRange string, values [][]any, valueInputOption string) (WriteValuesOutput, error) {
	return UpdateValues(ctx, c.httpClient, spreadsheetID, writeRange, values, valueInputOption)
}

func (c *Client) AppendValues(ctx context.Context, spreadsheetID string, writeRange string, values [][]any, valueInputOption string) (AppendValuesOutput, error) {
	return AppendValues(ctx, c.httpClient, spreadsheetID, writeRange, values, valueInputOption)
}

func GetSpreadsheet(ctx context.Context, client *http.Client, spreadsheetID string) (SpreadsheetSummary, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return SpreadsheetSummary{}, err
	}
	spreadsheet, err := service.Spreadsheets.Get(spreadsheetID).IncludeGridData(false).Do()
	if err != nil {
		return SpreadsheetSummary{}, common.MapError(err)
	}
	return spreadsheetFromAPI(spreadsheet), nil
}

func ReadValues(ctx context.Context, client *http.Client, spreadsheetID string, readRange string) (ValuesOutput, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return ValuesOutput{}, err
	}
	response, err := service.Spreadsheets.Values.Get(spreadsheetID, readRange).Do()
	if err != nil {
		return ValuesOutput{}, common.MapError(err)
	}
	return ValuesOutput{
		SpreadsheetID:  spreadsheetID,
		Range:          response.Range,
		MajorDimension: response.MajorDimension,
		Values:         response.Values,
	}, nil
}

func CreateSpreadsheet(ctx context.Context, client *http.Client, title string, sheetTitles []string) (SpreadsheetSummary, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return SpreadsheetSummary{}, err
	}
	spreadsheet := &sheets.Spreadsheet{
		Properties: &sheets.SpreadsheetProperties{Title: strings.TrimSpace(title)},
	}
	for _, sheetTitle := range cleanStrings(sheetTitles) {
		spreadsheet.Sheets = append(spreadsheet.Sheets, &sheets.Sheet{
			Properties: &sheets.SheetProperties{Title: sheetTitle},
		})
	}
	created, err := service.Spreadsheets.Create(spreadsheet).Do()
	if err != nil {
		return SpreadsheetSummary{}, common.MapError(err)
	}
	return spreadsheetFromAPI(created), nil
}

func UpdateValues(ctx context.Context, client *http.Client, spreadsheetID string, writeRange string, values [][]any, valueInputOption string) (WriteValuesOutput, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return WriteValuesOutput{}, err
	}
	response, err := service.Spreadsheets.Values.Update(spreadsheetID, writeRange, &sheets.ValueRange{
		Values: values,
	}).ValueInputOption(normalizeValueInputOption(valueInputOption)).Do()
	if err != nil {
		return WriteValuesOutput{}, common.MapError(err)
	}
	return WriteValuesOutput{
		SpreadsheetID:  spreadsheetID,
		UpdatedRange:   response.UpdatedRange,
		UpdatedRows:    response.UpdatedRows,
		UpdatedColumns: response.UpdatedColumns,
		UpdatedCells:   response.UpdatedCells,
	}, nil
}

func AppendValues(ctx context.Context, client *http.Client, spreadsheetID string, writeRange string, values [][]any, valueInputOption string) (AppendValuesOutput, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return AppendValuesOutput{}, err
	}
	response, err := service.Spreadsheets.Values.Append(spreadsheetID, writeRange, &sheets.ValueRange{
		Values: values,
	}).ValueInputOption(normalizeValueInputOption(valueInputOption)).InsertDataOption("INSERT_ROWS").Do()
	if err != nil {
		return AppendValuesOutput{}, common.MapError(err)
	}
	output := AppendValuesOutput{
		SpreadsheetID: spreadsheetID,
		TableRange:    response.TableRange,
	}
	if response.Updates != nil {
		output.UpdatedRange = response.Updates.UpdatedRange
		output.UpdatedRows = response.Updates.UpdatedRows
		output.UpdatedColumns = response.Updates.UpdatedColumns
		output.UpdatedCells = response.Updates.UpdatedCells
	}
	return output, nil
}

func serviceFromClient(ctx context.Context, client *http.Client) (*sheets.Service, error) {
	if client == nil {
		return nil, errors.New("http client is required")
	}
	service, err := sheets.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("create sheets service: %w", err)
	}
	return service, nil
}

func spreadsheetFromAPI(spreadsheet *sheets.Spreadsheet) SpreadsheetSummary {
	if spreadsheet == nil {
		return SpreadsheetSummary{}
	}
	summary := SpreadsheetSummary{
		ID:             spreadsheet.SpreadsheetId,
		SpreadsheetURL: spreadsheet.SpreadsheetUrl,
	}
	if spreadsheet.Properties != nil {
		summary.Title = spreadsheet.Properties.Title
	}
	for _, sheet := range spreadsheet.Sheets {
		if sheet == nil || sheet.Properties == nil {
			continue
		}
		summary.Sheets = append(summary.Sheets, SheetSummary{
			ID:    sheet.Properties.SheetId,
			Title: sheet.Properties.Title,
			Index: sheet.Properties.Index,
		})
	}
	return summary
}

func normalizeValueInputOption(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "RAW":
		return "RAW"
	default:
		return "USER_ENTERED"
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
