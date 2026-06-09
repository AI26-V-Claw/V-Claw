package sheets

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/api/option"
	gsheets "google.golang.org/api/sheets/v4"
)

type Client struct {
	srv *gsheets.Service
}

func NewClient(ctx context.Context, client *http.Client) (*Client, error) {
	if client == nil {
		return nil, errors.New("http client is required")
	}
	srv, err := gsheets.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("create sheets service: %w", err)
	}
	return &Client{srv: srv}, nil
}

type Spreadsheet struct {
	ID       string      `json:"id"`
	Title    string      `json:"title"`
	Sheets   []SheetInfo `json:"sheets,omitempty"`
	Locale   string      `json:"locale,omitempty"`
	TimeZone string      `json:"timeZone,omitempty"`
	URL      string      `json:"url,omitempty"`
}

type SheetInfo struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Index  int64  `json:"index"`
	Type   string `json:"type,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
}

type RangeValues struct {
	SpreadsheetID  string          `json:"spreadsheetId"`
	Range          string          `json:"range"`
	MajorDimension string          `json:"majorDimension,omitempty"`
	Values         [][]interface{} `json:"values"`
}

func (c *Client) GetSpreadsheet(ctx context.Context, spreadsheetID string) (Spreadsheet, error) {
	if c == nil || c.srv == nil {
		return Spreadsheet{}, errors.New("sheets service is not configured")
	}
	spreadsheet, err := c.srv.Spreadsheets.Get(spreadsheetID).Context(ctx).Do()
	if err != nil {
		return Spreadsheet{}, err
	}
	return spreadsheetFromAPI(spreadsheet), nil
}

func (c *Client) ReadRange(ctx context.Context, spreadsheetID string, readRange string) (RangeValues, error) {
	if c == nil || c.srv == nil {
		return RangeValues{}, errors.New("sheets service is not configured")
	}
	values, err := c.srv.Spreadsheets.Values.Get(spreadsheetID, readRange).Context(ctx).Do()
	if err != nil {
		return RangeValues{}, err
	}
	return rangeValuesFromAPI(spreadsheetID, values), nil
}

func (c *Client) CreateSpreadsheet(ctx context.Context, title string) (Spreadsheet, error) {
	if c == nil || c.srv == nil {
		return Spreadsheet{}, errors.New("sheets service is not configured")
	}
	spreadsheet, err := c.srv.Spreadsheets.Create(&gsheets.Spreadsheet{
		Properties: &gsheets.SpreadsheetProperties{Title: title},
	}).Context(ctx).Do()
	if err != nil {
		return Spreadsheet{}, err
	}
	return spreadsheetFromAPI(spreadsheet), nil
}

func (c *Client) UpdateRange(ctx context.Context, spreadsheetID string, writeRange string, values [][]interface{}, valueInputOption string) (RangeValues, error) {
	if c == nil || c.srv == nil {
		return RangeValues{}, errors.New("sheets service is not configured")
	}
	valueInputOption = normalizeValueInputOption(valueInputOption)
	response, err := c.srv.Spreadsheets.Values.Update(spreadsheetID, writeRange, &gsheets.ValueRange{
		Range:  writeRange,
		Values: values,
	}).Context(ctx).ValueInputOption(valueInputOption).Do()
	if err != nil {
		return RangeValues{}, err
	}
	return RangeValues{
		SpreadsheetID: spreadsheetID,
		Range:         response.UpdatedRange,
		Values:        values,
	}, nil
}

func (c *Client) AppendRows(ctx context.Context, spreadsheetID string, writeRange string, values [][]interface{}, valueInputOption string) (RangeValues, error) {
	if c == nil || c.srv == nil {
		return RangeValues{}, errors.New("sheets service is not configured")
	}
	valueInputOption = normalizeValueInputOption(valueInputOption)
	response, err := c.srv.Spreadsheets.Values.Append(spreadsheetID, writeRange, &gsheets.ValueRange{
		Range:  writeRange,
		Values: values,
	}).Context(ctx).ValueInputOption(valueInputOption).InsertDataOption("INSERT_ROWS").Do()
	if err != nil {
		return RangeValues{}, err
	}
	updatedRange := ""
	if response.Updates != nil {
		updatedRange = response.Updates.UpdatedRange
	}
	return RangeValues{
		SpreadsheetID: spreadsheetID,
		Range:         updatedRange,
		Values:        values,
	}, nil
}

func spreadsheetFromAPI(spreadsheet *gsheets.Spreadsheet) Spreadsheet {
	if spreadsheet == nil {
		return Spreadsheet{}
	}
	out := Spreadsheet{
		ID:  spreadsheet.SpreadsheetId,
		URL: spreadsheet.SpreadsheetUrl,
	}
	if spreadsheet.Properties != nil {
		out.Title = spreadsheet.Properties.Title
		out.Locale = spreadsheet.Properties.Locale
		out.TimeZone = spreadsheet.Properties.TimeZone
	}
	for _, sheet := range spreadsheet.Sheets {
		if sheet == nil || sheet.Properties == nil {
			continue
		}
		out.Sheets = append(out.Sheets, SheetInfo{
			ID:     sheet.Properties.SheetId,
			Title:  sheet.Properties.Title,
			Index:  sheet.Properties.Index,
			Type:   sheet.Properties.SheetType,
			Hidden: sheet.Properties.Hidden,
		})
	}
	return out
}

func rangeValuesFromAPI(spreadsheetID string, values *gsheets.ValueRange) RangeValues {
	if values == nil {
		return RangeValues{SpreadsheetID: spreadsheetID}
	}
	return RangeValues{
		SpreadsheetID:  spreadsheetID,
		Range:          values.Range,
		MajorDimension: values.MajorDimension,
		Values:         values.Values,
	}
}

func normalizeValueInputOption(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	switch value {
	case "RAW", "USER_ENTERED":
		return value
	default:
		return "RAW"
	}
}
