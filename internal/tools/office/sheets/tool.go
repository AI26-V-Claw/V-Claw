package sheets

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	googleconnector "vclaw/internal/connectors/google"
	sheetsconnector "vclaw/internal/connectors/google/sheets"
	"vclaw/internal/tools"

	"google.golang.org/api/googleapi"
)

const (
	ToolNameGetSpreadsheet    = "sheets.getSpreadsheet"
	ToolNameListSheets        = "sheets.listSheets"
	ToolNameReadRange         = "sheets.readRange"
	ToolNameCreateSpreadsheet = "sheets.createSpreadsheet"
	ToolNameUpdateRange       = "sheets.updateRange"
	ToolNameAppendRows        = "sheets.appendRows"
)

type ToolRegistryEntry struct {
	Name             string
	Owner            string
	Description      string
	DefaultRiskLevel string
	RequiresApproval bool
}

var RegistryEntries = []ToolRegistryEntry{
	{Name: ToolNameGetSpreadsheet, Owner: "integration", Description: "Read Google Sheets spreadsheet metadata.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameListSheets, Owner: "integration", Description: "List tabs in a Google Sheets spreadsheet.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameReadRange, Owner: "integration", Description: "Read values from a Google Sheets range.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameCreateSpreadsheet, Owner: "integration", Description: "Create a Google Sheets spreadsheet.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameUpdateRange, Owner: "integration", Description: "Update values in a Google Sheets range.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameAppendRows, Owner: "integration", Description: "Append rows to a Google Sheets range.", DefaultRiskLevel: "external_write", RequiresApproval: true},
}

type Connector interface {
	GetSpreadsheet(ctx context.Context, spreadsheetID string) (sheetsconnector.Spreadsheet, error)
	ReadRange(ctx context.Context, spreadsheetID string, readRange string) (sheetsconnector.RangeValues, error)
	CreateSpreadsheet(ctx context.Context, title string) (sheetsconnector.Spreadsheet, error)
	UpdateRange(ctx context.Context, spreadsheetID string, writeRange string, values [][]interface{}, valueInputOption string) (sheetsconnector.RangeValues, error)
	AppendRows(ctx context.Context, spreadsheetID string, writeRange string, values [][]interface{}, valueInputOption string) (sheetsconnector.RangeValues, error)
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

type SpreadsheetInput struct {
	SpreadsheetID string
}

type ReadRangeInput struct {
	SpreadsheetID string
	Range         string
}

type CreateSpreadsheetInput struct {
	Title string
}

type WriteRangeInput struct {
	SpreadsheetID    string
	Range            string
	Values           [][]interface{}
	ValueInputOption string
}

type ListSheetsOutput struct {
	SpreadsheetID string                      `json:"spreadsheetId"`
	Title         string                      `json:"title"`
	Sheets        []sheetsconnector.SheetInfo `json:"sheets"`
}

func (s *Service) GetSpreadsheet(ctx context.Context, input SpreadsheetInput) (sheetsconnector.Spreadsheet, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return sheetsconnector.Spreadsheet{}, errShape
	}
	if strings.TrimSpace(input.SpreadsheetID) == "" {
		return sheetsconnector.Spreadsheet{}, invalidInput("spreadsheetId is required")
	}
	output, err := s.connector.GetSpreadsheet(ctx, strings.TrimSpace(input.SpreadsheetID))
	if err != nil {
		return sheetsconnector.Spreadsheet{}, MapError(err)
	}
	return output, nil
}

func (s *Service) ListSheets(ctx context.Context, input SpreadsheetInput) (ListSheetsOutput, *ErrorShape) {
	output, errShape := s.GetSpreadsheet(ctx, input)
	if errShape != nil {
		return ListSheetsOutput{}, errShape
	}
	return ListSheetsOutput{SpreadsheetID: output.ID, Title: output.Title, Sheets: output.Sheets}, nil
}

func (s *Service) ReadRange(ctx context.Context, input ReadRangeInput) (sheetsconnector.RangeValues, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return sheetsconnector.RangeValues{}, errShape
	}
	if strings.TrimSpace(input.SpreadsheetID) == "" {
		return sheetsconnector.RangeValues{}, invalidInput("spreadsheetId is required")
	}
	if strings.TrimSpace(input.Range) == "" {
		return sheetsconnector.RangeValues{}, invalidInput("range is required")
	}
	output, err := s.connector.ReadRange(ctx, strings.TrimSpace(input.SpreadsheetID), strings.TrimSpace(input.Range))
	if err != nil {
		return sheetsconnector.RangeValues{}, MapError(err)
	}
	return output, nil
}

func (s *Service) CreateSpreadsheet(ctx context.Context, input CreateSpreadsheetInput) (sheetsconnector.Spreadsheet, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return sheetsconnector.Spreadsheet{}, errShape
	}
	if strings.TrimSpace(input.Title) == "" {
		return sheetsconnector.Spreadsheet{}, invalidInput("title is required")
	}
	output, err := s.connector.CreateSpreadsheet(ctx, strings.TrimSpace(input.Title))
	if err != nil {
		return sheetsconnector.Spreadsheet{}, MapError(err)
	}
	return output, nil
}

func (s *Service) UpdateRange(ctx context.Context, input WriteRangeInput) (sheetsconnector.RangeValues, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return sheetsconnector.RangeValues{}, errShape
	}
	if errShape := validateWriteInput(input); errShape != nil {
		return sheetsconnector.RangeValues{}, errShape
	}
	output, err := s.connector.UpdateRange(ctx, strings.TrimSpace(input.SpreadsheetID), strings.TrimSpace(input.Range), input.Values, input.ValueInputOption)
	if err != nil {
		return sheetsconnector.RangeValues{}, MapError(err)
	}
	return output, nil
}

func (s *Service) AppendRows(ctx context.Context, input WriteRangeInput) (sheetsconnector.RangeValues, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return sheetsconnector.RangeValues{}, errShape
	}
	if errShape := validateWriteInput(input); errShape != nil {
		return sheetsconnector.RangeValues{}, errShape
	}
	output, err := s.connector.AppendRows(ctx, strings.TrimSpace(input.SpreadsheetID), strings.TrimSpace(input.Range), input.Values, input.ValueInputOption)
	if err != nil {
		return sheetsconnector.RangeValues{}, MapError(err)
	}
	return output, nil
}

func (s *Service) validateConnector() *ErrorShape {
	if s == nil || s.connector == nil {
		return &ErrorShape{Code: "INTERNAL_ERROR", Message: "sheets connector is not configured"}
	}
	return nil
}

func validateWriteInput(input WriteRangeInput) *ErrorShape {
	if strings.TrimSpace(input.SpreadsheetID) == "" {
		return invalidInput("spreadsheetId is required")
	}
	if strings.TrimSpace(input.Range) == "" {
		return invalidInput("range is required")
	}
	if len(input.Values) == 0 {
		return invalidInput("values is required")
	}
	return nil
}

type sheetsTool struct {
	name    string
	service *Service
}

func newTool(name string, service *Service) sheetsTool {
	return sheetsTool{name: name, service: service}
}

func (t sheetsTool) Name() string { return t.name }

func (t sheetsTool) Description() string {
	switch t.name {
	case ToolNameGetSpreadsheet:
		return "Read metadata for a Google Sheets spreadsheet."
	case ToolNameListSheets:
		return "List sheet tabs in a Google Sheets spreadsheet."
	case ToolNameReadRange:
		return "Read values from a Google Sheets range such as Sheet1!A1:D20."
	case ToolNameCreateSpreadsheet:
		return "Create a Google Sheets spreadsheet. This external write requires approval."
	case ToolNameUpdateRange:
		return "Update a Google Sheets range. This external write requires approval."
	case ToolNameAppendRows:
		return "Append rows to a Google Sheets range. This external write requires approval."
	default:
		return "Google Sheets tool."
	}
}

func (t sheetsTool) Parameters() tools.ToolSchema {
	switch t.name {
	case ToolNameGetSpreadsheet, ToolNameListSheets:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"spreadsheetId": map[string]any{"type": "string"}}, "required": []string{"spreadsheetId"}, "additionalProperties": false}
	case ToolNameReadRange:
		return rangeSchema([]string{"spreadsheetId", "range"})
	case ToolNameCreateSpreadsheet:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}}, "required": []string{"title"}, "additionalProperties": false}
	case ToolNameUpdateRange, ToolNameAppendRows:
		schema := rangeSchema([]string{"spreadsheetId", "range", "values"})
		props := schema["properties"].(map[string]any)
		props["values"] = map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{}}}
		props["valueInputOption"] = map[string]any{"type": "string", "enum": []string{"RAW", "USER_ENTERED"}}
		return schema
	default:
		return tools.ToolSchema{"type": "object"}
	}
}

func (t sheetsTool) Capability() tools.Capability {
	switch t.name {
	case ToolNameGetSpreadsheet, ToolNameListSheets, ToolNameReadRange:
		return tools.CapabilityReadOnly
	default:
		return tools.CapabilityMutating
	}
}

func (t sheetsTool) RiskLevel() tools.RiskLevel {
	switch t.name {
	case ToolNameGetSpreadsheet, ToolNameListSheets, ToolNameReadRange:
		return tools.RiskLevelSafeRead
	default:
		return tools.RiskLevelExternalWrite
	}
}

func (t sheetsTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	switch t.name {
	case ToolNameGetSpreadsheet:
		output, errShape := t.service.GetSpreadsheet(ctx, SpreadsheetInput{SpreadsheetID: stringArg(call.Arguments, "spreadsheetId")})
		return outputToolResult(call, output, errShape)
	case ToolNameListSheets:
		output, errShape := t.service.ListSheets(ctx, SpreadsheetInput{SpreadsheetID: stringArg(call.Arguments, "spreadsheetId")})
		return outputToolResult(call, output, errShape)
	case ToolNameReadRange:
		output, errShape := t.service.ReadRange(ctx, ReadRangeInput{SpreadsheetID: stringArg(call.Arguments, "spreadsheetId"), Range: stringArg(call.Arguments, "range")})
		return outputToolResult(call, output, errShape)
	case ToolNameCreateSpreadsheet:
		output, errShape := t.service.CreateSpreadsheet(ctx, CreateSpreadsheetInput{Title: stringArg(call.Arguments, "title")})
		return outputToolResult(call, output, errShape)
	case ToolNameUpdateRange:
		output, errShape := t.service.UpdateRange(ctx, WriteRangeInput{SpreadsheetID: stringArg(call.Arguments, "spreadsheetId"), Range: stringArg(call.Arguments, "range"), Values: matrixArg(call.Arguments, "values"), ValueInputOption: stringArg(call.Arguments, "valueInputOption")})
		return outputToolResult(call, output, errShape)
	case ToolNameAppendRows:
		output, errShape := t.service.AppendRows(ctx, WriteRangeInput{SpreadsheetID: stringArg(call.Arguments, "spreadsheetId"), Range: stringArg(call.Arguments, "range"), Values: matrixArg(call.Arguments, "values"), ValueInputOption: stringArg(call.Arguments, "valueInputOption")})
		return outputToolResult(call, output, errShape)
	default:
		return tools.ToolNotFoundResult(call)
	}
}

func RegisterTools(registry *tools.ToolRegistry, service *Service) error {
	for _, name := range []string{ToolNameGetSpreadsheet, ToolNameListSheets, ToolNameReadRange, ToolNameCreateSpreadsheet, ToolNameUpdateRange, ToolNameAppendRows} {
		if err := registry.RegisterWithEntry(newTool(name, service), tools.ToolRegistryEntry{Owner: "integration", Group: "google_workspace"}); err != nil {
			return err
		}
	}
	return nil
}

func invalidInput(message string) *ErrorShape {
	return &ErrorShape{Code: "INVALID_INPUT", Message: message}
}

func MapError(err error) *ErrorShape {
	if err == nil {
		return nil
	}
	if googleconnector.IsNetworkError(err) {
		return &ErrorShape{Code: "PROVIDER_TIMEOUT", Message: "network error contacting Sheets API: " + err.Error(), Retryable: true}
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

func rangeSchema(required []string) tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{"spreadsheetId": map[string]any{"type": "string"}, "range": map[string]any{"type": "string"}}, "required": required, "additionalProperties": false}
}

func stringArg(args map[string]any, name string) string {
	if args == nil {
		return ""
	}
	value, _ := args[name].(string)
	return value
}

func matrixArg(args map[string]any, name string) [][]interface{} {
	if args == nil {
		return nil
	}
	value, ok := args[name]
	if !ok {
		return nil
	}
	switch rows := value.(type) {
	case [][]interface{}:
		return rows
	case []interface{}:
		out := make([][]interface{}, 0, len(rows))
		for _, row := range rows {
			switch cells := row.(type) {
			case []interface{}:
				out = append(out, cells)
			case []string:
				line := make([]interface{}, 0, len(cells))
				for _, cell := range cells {
					line = append(line, cell)
				}
				out = append(out, line)
			default:
				out = append(out, []interface{}{cells})
			}
		}
		return out
	default:
		return nil
	}
}
