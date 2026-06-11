package sheets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vclaw/internal/connectors/google/common"
	gsheets "vclaw/internal/connectors/google/sheets"
	"vclaw/internal/tools"
)

const (
	ToolNameGetSpreadsheet    = "sheets.getSpreadsheet"
	ToolNameReadValues        = "sheets.readValues"
	ToolNameCreateSpreadsheet = "sheets.createSpreadsheet"
	ToolNameUpdateValues      = "sheets.updateValues"
	ToolNameAppendValues      = "sheets.appendValues"
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
	{Name: ToolNameReadValues, Owner: "integration", Description: "Read values from a Google Sheets range.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameCreateSpreadsheet, Owner: "integration", Description: "Create a Google Sheets spreadsheet.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameUpdateValues, Owner: "integration", Description: "Update values in a Google Sheets range.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameAppendValues, Owner: "integration", Description: "Append values to a Google Sheets range.", DefaultRiskLevel: "external_write", RequiresApproval: true},
}

type Connector interface {
	GetSpreadsheet(ctx context.Context, spreadsheetID string) (gsheets.SpreadsheetSummary, error)
	ReadValues(ctx context.Context, spreadsheetID string, readRange string) (gsheets.ValuesOutput, error)
	CreateSpreadsheet(ctx context.Context, title string, sheetTitles []string) (gsheets.SpreadsheetSummary, error)
	UpdateValues(ctx context.Context, spreadsheetID string, writeRange string, values [][]any, valueInputOption string) (gsheets.WriteValuesOutput, error)
	AppendValues(ctx context.Context, spreadsheetID string, writeRange string, values [][]any, valueInputOption string) (gsheets.AppendValuesOutput, error)
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

type GetSpreadsheetInput struct {
	SpreadsheetID string
}

type ReadValuesInput struct {
	SpreadsheetID string
	Range         string
}

type CreateSpreadsheetInput struct {
	Title       string
	SheetTitles []string
}

type ValuesInput struct {
	SpreadsheetID    string
	Range            string
	Values           [][]any
	ValueInputOption string
}

func (s *Service) GetSpreadsheet(ctx context.Context, input GetSpreadsheetInput) (gsheets.SpreadsheetSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gsheets.SpreadsheetSummary{}, errShape
	}
	if strings.TrimSpace(input.SpreadsheetID) == "" {
		return gsheets.SpreadsheetSummary{}, invalidInput("spreadsheetId is required")
	}
	output, err := s.connector.GetSpreadsheet(ctx, input.SpreadsheetID)
	if err != nil {
		return gsheets.SpreadsheetSummary{}, mapError(err)
	}
	return output, nil
}

func (s *Service) ReadValues(ctx context.Context, input ReadValuesInput) (gsheets.ValuesOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gsheets.ValuesOutput{}, errShape
	}
	if strings.TrimSpace(input.SpreadsheetID) == "" {
		return gsheets.ValuesOutput{}, invalidInput("spreadsheetId is required")
	}
	if strings.TrimSpace(input.Range) == "" {
		return gsheets.ValuesOutput{}, invalidInput("range is required")
	}
	output, err := s.connector.ReadValues(ctx, input.SpreadsheetID, input.Range)
	if err != nil {
		return gsheets.ValuesOutput{}, mapError(err)
	}
	return output, nil
}

func (s *Service) CreateSpreadsheet(ctx context.Context, input CreateSpreadsheetInput) (gsheets.SpreadsheetSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gsheets.SpreadsheetSummary{}, errShape
	}
	if strings.TrimSpace(input.Title) == "" {
		return gsheets.SpreadsheetSummary{}, invalidInput("title is required")
	}
	output, err := s.connector.CreateSpreadsheet(ctx, input.Title, input.SheetTitles)
	if err != nil {
		return gsheets.SpreadsheetSummary{}, mapError(err)
	}
	return output, nil
}

func (s *Service) UpdateValues(ctx context.Context, input ValuesInput) (gsheets.WriteValuesOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gsheets.WriteValuesOutput{}, errShape
	}
	if errShape := validateValuesInput(input); errShape != nil {
		return gsheets.WriteValuesOutput{}, errShape
	}
	output, err := s.connector.UpdateValues(ctx, input.SpreadsheetID, input.Range, input.Values, input.ValueInputOption)
	if err != nil {
		return gsheets.WriteValuesOutput{}, mapError(err)
	}
	return output, nil
}

func (s *Service) AppendValues(ctx context.Context, input ValuesInput) (gsheets.AppendValuesOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gsheets.AppendValuesOutput{}, errShape
	}
	if errShape := validateValuesInput(input); errShape != nil {
		return gsheets.AppendValuesOutput{}, errShape
	}
	output, err := s.connector.AppendValues(ctx, input.SpreadsheetID, input.Range, input.Values, input.ValueInputOption)
	if err != nil {
		return gsheets.AppendValuesOutput{}, mapError(err)
	}
	return output, nil
}

func (s *Service) validateConnector() *ErrorShape {
	if s == nil || s.connector == nil {
		return internalError("sheets connector is not configured")
	}
	return nil
}

func validateValuesInput(input ValuesInput) *ErrorShape {
	if strings.TrimSpace(input.SpreadsheetID) == "" {
		return invalidInput("spreadsheetId is required")
	}
	if strings.TrimSpace(input.Range) == "" {
		return invalidInput("range is required")
	}
	if len(input.Values) == 0 {
		return invalidInput("values must contain at least one row")
	}
	return nil
}

type SheetsTool struct {
	name    string
	service *Service
}

func NewTool(name string, service *Service) SheetsTool {
	return SheetsTool{name: name, service: service}
}

func (t SheetsTool) Name() string { return t.name }

func (t SheetsTool) Description() string {
	switch t.name {
	case ToolNameGetSpreadsheet:
		return "Read Google Sheets spreadsheet metadata and sheet tabs by spreadsheetId."
	case ToolNameReadValues:
		return "Read cell values from a Google Sheets range."
	case ToolNameCreateSpreadsheet:
		return "Create a Google Sheets spreadsheet. Requires human approval before execution."
	case ToolNameUpdateValues:
		return "Update values in a Google Sheets range. Requires human approval before execution."
	case ToolNameAppendValues:
		return "Append rows to a Google Sheets range. Requires human approval before execution."
	default:
		return "Google Sheets tool."
	}
}

func (t SheetsTool) Parameters() tools.ToolSchema {
	switch t.name {
	case ToolNameGetSpreadsheet:
		return idSchema("spreadsheetId")
	case ToolNameReadValues:
		return rangeSchema()
	case ToolNameCreateSpreadsheet:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"title":       map[string]any{"type": "string"},
			"sheetTitles": arrayStringSchema(),
		}, "required": []string{"title"}, "additionalProperties": false}
	case ToolNameUpdateValues, ToolNameAppendValues:
		return valuesSchema()
	default:
		return tools.ToolSchema{"type": "object"}
	}
}

func (t SheetsTool) Capability() tools.Capability {
	switch t.name {
	case ToolNameGetSpreadsheet, ToolNameReadValues:
		return tools.CapabilityReadOnly
	default:
		return tools.CapabilityMutating
	}
}

func (t SheetsTool) RiskLevel() tools.RiskLevel {
	switch t.name {
	case ToolNameGetSpreadsheet, ToolNameReadValues:
		return tools.RiskLevelSafeRead
	default:
		return tools.RiskLevelExternalWrite
	}
}

func (t SheetsTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	switch t.name {
	case ToolNameGetSpreadsheet:
		output, errShape := t.service.GetSpreadsheet(ctx, GetSpreadsheetInput{SpreadsheetID: stringArg(call.Arguments, "spreadsheetId")})
		return outputToolResult(call, output, errShape)
	case ToolNameReadValues:
		output, errShape := t.service.ReadValues(ctx, ReadValuesInput{SpreadsheetID: stringArg(call.Arguments, "spreadsheetId"), Range: stringArg(call.Arguments, "range")})
		return outputToolResult(call, output, errShape)
	case ToolNameCreateSpreadsheet:
		output, errShape := t.service.CreateSpreadsheet(ctx, CreateSpreadsheetInput{Title: stringArg(call.Arguments, "title"), SheetTitles: stringSliceArg(call.Arguments, "sheetTitles")})
		return outputToolResult(call, output, errShape)
	case ToolNameUpdateValues:
		output, errShape := t.service.UpdateValues(ctx, valuesInputFromArgs(call.Arguments))
		return outputToolResult(call, output, errShape)
	case ToolNameAppendValues:
		output, errShape := t.service.AppendValues(ctx, valuesInputFromArgs(call.Arguments))
		return outputToolResult(call, output, errShape)
	default:
		return tools.ToolNotFoundResult(call)
	}
}

func RegisterTools(registry *tools.ToolRegistry, service *Service) error {
	for _, name := range []string{ToolNameGetSpreadsheet, ToolNameReadValues, ToolNameCreateSpreadsheet, ToolNameUpdateValues, ToolNameAppendValues} {
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

func idSchema(name string) tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{name: map[string]any{"type": "string"}}, "required": []string{name}, "additionalProperties": false}
}

func rangeSchema() tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{
		"spreadsheetId": map[string]any{"type": "string"},
		"range":         map[string]any{"type": "string", "description": "A1 notation, e.g. Sheet1!A1:D10."},
	}, "required": []string{"spreadsheetId", "range"}, "additionalProperties": false}
}

func valuesSchema() tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{
		"spreadsheetId":    map[string]any{"type": "string"},
		"range":            map[string]any{"type": "string", "description": "A1 notation."},
		"values":           map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{}}},
		"valueInputOption": map[string]any{"type": "string", "enum": []string{"USER_ENTERED", "RAW"}, "description": "Omit to use USER_ENTERED."},
	}, "required": []string{"spreadsheetId", "range", "values"}, "additionalProperties": false}
}

func arrayStringSchema() map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
}

func valuesInputFromArgs(args map[string]any) ValuesInput {
	return ValuesInput{
		SpreadsheetID:    stringArg(args, "spreadsheetId"),
		Range:            stringArg(args, "range"),
		Values:           valuesArg(args, "values"),
		ValueInputOption: stringArg(args, "valueInputOption"),
	}
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

func valuesArg(args map[string]any, name string) [][]any {
	if args == nil {
		return nil
	}
	rawRows, ok := args[name].([]any)
	if !ok {
		return nil
	}
	rows := make([][]any, 0, len(rawRows))
	for _, rawRow := range rawRows {
		rowValues, ok := rawRow.([]any)
		if !ok {
			continue
		}
		rows = append(rows, rowValues)
	}
	return rows
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
