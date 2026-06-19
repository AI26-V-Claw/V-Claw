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
	ToolNameBatchGetValues    = "sheets.batchGetValues"
	ToolNameCreateSpreadsheet = "sheets.createSpreadsheet"
	ToolNameUpdateValues      = "sheets.updateValues"
	ToolNameBatchUpdateValues = "sheets.batchUpdateValues"
	ToolNameAppendValues      = "sheets.appendValues"
	ToolNameClearValues       = "sheets.clearValues"
	ToolNameAddSheet          = "sheets.addSheet"
	ToolNameRenameSheet       = "sheets.renameSheet"
	ToolNameDeleteSheet       = "sheets.deleteSheet"
	ToolNameDuplicateSheet    = "sheets.duplicateSheet"
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
	{Name: ToolNameReadValues, Owner: "integration", Description: "Read values from a Google Sheets range.", DefaultRiskLevel: "sensitive_read", RequiresApproval: true},
	{Name: ToolNameBatchGetValues, Owner: "integration", Description: "Read values from multiple Google Sheets ranges.", DefaultRiskLevel: "sensitive_read", RequiresApproval: true},
	{Name: ToolNameCreateSpreadsheet, Owner: "integration", Description: "Create a Google Sheets spreadsheet.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameUpdateValues, Owner: "integration", Description: "Update values in a Google Sheets range.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameBatchUpdateValues, Owner: "integration", Description: "Update values in multiple Google Sheets ranges.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameAppendValues, Owner: "integration", Description: "Append values to a Google Sheets range.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameClearValues, Owner: "integration", Description: "Clear values from a Google Sheets range.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameAddSheet, Owner: "integration", Description: "Add a tab to a Google Sheets spreadsheet.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameRenameSheet, Owner: "integration", Description: "Rename a Google Sheets tab.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameDeleteSheet, Owner: "integration", Description: "Delete a Google Sheets tab.", DefaultRiskLevel: "destructive", RequiresApproval: true},
	{Name: ToolNameDuplicateSheet, Owner: "integration", Description: "Duplicate a Google Sheets tab.", DefaultRiskLevel: "external_write", RequiresApproval: true},
}

type Connector interface {
	GetSpreadsheet(ctx context.Context, spreadsheetID string) (gsheets.SpreadsheetSummary, error)
	ReadValues(ctx context.Context, spreadsheetID string, readRange string) (gsheets.ValuesOutput, error)
	BatchGetValues(ctx context.Context, spreadsheetID string, ranges []string) (gsheets.BatchValuesOutput, error)
	CreateSpreadsheet(ctx context.Context, title string, sheetTitles []string) (gsheets.SpreadsheetSummary, error)
	UpdateValues(ctx context.Context, spreadsheetID string, writeRange string, values [][]any, valueInputOption string) (gsheets.WriteValuesOutput, error)
	BatchUpdateValues(ctx context.Context, spreadsheetID string, ranges map[string][][]any, valueInputOption string) (gsheets.WriteValuesOutput, error)
	AppendValues(ctx context.Context, spreadsheetID string, writeRange string, values [][]any, valueInputOption string) (gsheets.AppendValuesOutput, error)
	ClearValues(ctx context.Context, spreadsheetID string, clearRange string) (gsheets.ClearValuesOutput, error)
	AddSheet(ctx context.Context, spreadsheetID string, title string) (gsheets.SpreadsheetSummary, error)
	RenameSheet(ctx context.Context, spreadsheetID string, sheetID int64, title string) (gsheets.SpreadsheetSummary, error)
	DeleteSheet(ctx context.Context, spreadsheetID string, sheetID int64) (gsheets.SpreadsheetSummary, error)
	DuplicateSheet(ctx context.Context, spreadsheetID string, sourceSheetID int64, newTitle string) (gsheets.SpreadsheetSummary, error)
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

type BatchGetValuesInput struct {
	SpreadsheetID string
	Ranges        []string
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

type BatchValuesInput struct {
	SpreadsheetID    string
	Ranges           map[string][][]any
	ValueInputOption string
}

type SheetTitleInput struct {
	SpreadsheetID string
	Title         string
}

type SheetIDInput struct {
	SpreadsheetID string
	SheetID       int64
}

type RenameSheetInput struct {
	SpreadsheetID string
	SheetID       int64
	Title         string
}

type DuplicateSheetInput struct {
	SpreadsheetID string
	SourceSheetID int64
	NewTitle      string
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

func (s *Service) BatchGetValues(ctx context.Context, input BatchGetValuesInput) (gsheets.BatchValuesOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gsheets.BatchValuesOutput{}, errShape
	}
	if strings.TrimSpace(input.SpreadsheetID) == "" {
		return gsheets.BatchValuesOutput{}, invalidInput("spreadsheetId is required")
	}
	if len(cleanStrings(input.Ranges)) == 0 {
		return gsheets.BatchValuesOutput{}, invalidInput("ranges must contain at least one range")
	}
	output, err := s.connector.BatchGetValues(ctx, input.SpreadsheetID, input.Ranges)
	if err != nil {
		return gsheets.BatchValuesOutput{}, mapError(err)
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

func (s *Service) BatchUpdateValues(ctx context.Context, input BatchValuesInput) (gsheets.WriteValuesOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gsheets.WriteValuesOutput{}, errShape
	}
	if strings.TrimSpace(input.SpreadsheetID) == "" {
		return gsheets.WriteValuesOutput{}, invalidInput("spreadsheetId is required")
	}
	if len(input.Ranges) == 0 {
		return gsheets.WriteValuesOutput{}, invalidInput("ranges must contain at least one range")
	}
	output, err := s.connector.BatchUpdateValues(ctx, input.SpreadsheetID, input.Ranges, input.ValueInputOption)
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

func (s *Service) ClearValues(ctx context.Context, input ReadValuesInput) (gsheets.ClearValuesOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gsheets.ClearValuesOutput{}, errShape
	}
	if strings.TrimSpace(input.SpreadsheetID) == "" {
		return gsheets.ClearValuesOutput{}, invalidInput("spreadsheetId is required")
	}
	if strings.TrimSpace(input.Range) == "" {
		return gsheets.ClearValuesOutput{}, invalidInput("range is required")
	}
	output, err := s.connector.ClearValues(ctx, input.SpreadsheetID, input.Range)
	if err != nil {
		return gsheets.ClearValuesOutput{}, mapError(err)
	}
	return output, nil
}

func (s *Service) AddSheet(ctx context.Context, input SheetTitleInput) (gsheets.SpreadsheetSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gsheets.SpreadsheetSummary{}, errShape
	}
	if strings.TrimSpace(input.SpreadsheetID) == "" {
		return gsheets.SpreadsheetSummary{}, invalidInput("spreadsheetId is required")
	}
	if strings.TrimSpace(input.Title) == "" {
		return gsheets.SpreadsheetSummary{}, invalidInput("title is required")
	}
	output, err := s.connector.AddSheet(ctx, input.SpreadsheetID, input.Title)
	if err != nil {
		return gsheets.SpreadsheetSummary{}, mapError(err)
	}
	return output, nil
}

func (s *Service) RenameSheet(ctx context.Context, input RenameSheetInput) (gsheets.SpreadsheetSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gsheets.SpreadsheetSummary{}, errShape
	}
	if strings.TrimSpace(input.SpreadsheetID) == "" {
		return gsheets.SpreadsheetSummary{}, invalidInput("spreadsheetId is required")
	}
	if input.SheetID == 0 {
		return gsheets.SpreadsheetSummary{}, invalidInput("sheetId is required")
	}
	if strings.TrimSpace(input.Title) == "" {
		return gsheets.SpreadsheetSummary{}, invalidInput("title is required")
	}
	output, err := s.connector.RenameSheet(ctx, input.SpreadsheetID, input.SheetID, input.Title)
	if err != nil {
		return gsheets.SpreadsheetSummary{}, mapError(err)
	}
	return output, nil
}

func (s *Service) DeleteSheet(ctx context.Context, input SheetIDInput) (gsheets.SpreadsheetSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gsheets.SpreadsheetSummary{}, errShape
	}
	if strings.TrimSpace(input.SpreadsheetID) == "" {
		return gsheets.SpreadsheetSummary{}, invalidInput("spreadsheetId is required")
	}
	if input.SheetID == 0 {
		return gsheets.SpreadsheetSummary{}, invalidInput("sheetId is required")
	}
	output, err := s.connector.DeleteSheet(ctx, input.SpreadsheetID, input.SheetID)
	if err != nil {
		return gsheets.SpreadsheetSummary{}, mapError(err)
	}
	return output, nil
}

func (s *Service) DuplicateSheet(ctx context.Context, input DuplicateSheetInput) (gsheets.SpreadsheetSummary, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gsheets.SpreadsheetSummary{}, errShape
	}
	if strings.TrimSpace(input.SpreadsheetID) == "" {
		return gsheets.SpreadsheetSummary{}, invalidInput("spreadsheetId is required")
	}
	if input.SourceSheetID == 0 {
		return gsheets.SpreadsheetSummary{}, invalidInput("sourceSheetId is required")
	}
	if strings.TrimSpace(input.NewTitle) == "" {
		return gsheets.SpreadsheetSummary{}, invalidInput("newTitle is required")
	}
	output, err := s.connector.DuplicateSheet(ctx, input.SpreadsheetID, input.SourceSheetID, input.NewTitle)
	if err != nil {
		return gsheets.SpreadsheetSummary{}, mapError(err)
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
	case ToolNameBatchGetValues:
		return "Read cell values from multiple Google Sheets ranges."
	case ToolNameCreateSpreadsheet:
		return "Create a Google Sheets spreadsheet. Requires human approval before execution."
	case ToolNameUpdateValues:
		return "Update values in a Google Sheets range. Requires human approval before execution."
	case ToolNameBatchUpdateValues:
		return "Update values in multiple Google Sheets ranges. Requires human approval before execution."
	case ToolNameAppendValues:
		return "Append rows to a Google Sheets range. Requires human approval before execution."
	case ToolNameClearValues:
		return "Clear values from a Google Sheets range. Requires human approval before execution."
	case ToolNameAddSheet:
		return "Add a sheet tab to a Google Sheets spreadsheet. Requires human approval before execution."
	case ToolNameRenameSheet:
		return "Rename a Google Sheets sheet tab. Requires human approval before execution."
	case ToolNameDeleteSheet:
		return "Delete a Google Sheets sheet tab. Requires human approval before execution."
	case ToolNameDuplicateSheet:
		return "Duplicate a Google Sheets sheet tab. Requires human approval before execution."
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
	case ToolNameBatchGetValues:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"spreadsheetId": map[string]any{"type": "string"},
			"ranges":        arrayStringSchema(),
		}, "required": []string{"spreadsheetId", "ranges"}, "additionalProperties": false}
	case ToolNameCreateSpreadsheet:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"title":       map[string]any{"type": "string"},
			"sheetTitles": arrayStringSchema(),
		}, "required": []string{"title"}, "additionalProperties": false}
	case ToolNameUpdateValues, ToolNameAppendValues:
		return valuesSchema()
	case ToolNameBatchUpdateValues:
		return batchValuesSchema()
	case ToolNameClearValues:
		return rangeSchema()
	case ToolNameAddSheet:
		return sheetTitleSchema()
	case ToolNameRenameSheet:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"spreadsheetId": map[string]any{"type": "string"},
			"sheetId":       map[string]any{"type": "number"},
			"title":         map[string]any{"type": "string"},
		}, "required": []string{"spreadsheetId", "sheetId", "title"}, "additionalProperties": false}
	case ToolNameDeleteSheet:
		return sheetIDSchema("sheetId")
	case ToolNameDuplicateSheet:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"spreadsheetId": map[string]any{"type": "string"},
			"sourceSheetId": map[string]any{"type": "number"},
			"newTitle":      map[string]any{"type": "string"},
		}, "required": []string{"spreadsheetId", "sourceSheetId", "newTitle"}, "additionalProperties": false}
	default:
		return tools.ToolSchema{"type": "object"}
	}
}

func (t SheetsTool) Capability() tools.Capability {
	switch t.name {
	case ToolNameGetSpreadsheet, ToolNameReadValues, ToolNameBatchGetValues:
		return tools.CapabilityReadOnly
	default:
		return tools.CapabilityMutating
	}
}

func (t SheetsTool) RiskLevel() tools.RiskLevel {
	switch t.name {
	case ToolNameGetSpreadsheet:
		return tools.RiskLevelSafeRead
	case ToolNameReadValues, ToolNameBatchGetValues:
		return tools.RiskLevelSensitiveRead
	case ToolNameDeleteSheet:
		return tools.RiskLevelDestructive
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
	case ToolNameBatchGetValues:
		output, errShape := t.service.BatchGetValues(ctx, BatchGetValuesInput{SpreadsheetID: stringArg(call.Arguments, "spreadsheetId"), Ranges: stringSliceArg(call.Arguments, "ranges")})
		return outputToolResult(call, output, errShape)
	case ToolNameCreateSpreadsheet:
		output, errShape := t.service.CreateSpreadsheet(ctx, CreateSpreadsheetInput{Title: stringArg(call.Arguments, "title"), SheetTitles: stringSliceArg(call.Arguments, "sheetTitles")})
		return outputToolResult(call, output, errShape)
	case ToolNameUpdateValues:
		output, errShape := t.service.UpdateValues(ctx, valuesInputFromArgs(call.Arguments))
		return outputToolResult(call, output, errShape)
	case ToolNameBatchUpdateValues:
		output, errShape := t.service.BatchUpdateValues(ctx, BatchValuesInput{SpreadsheetID: stringArg(call.Arguments, "spreadsheetId"), Ranges: rangeValuesArg(call.Arguments, "ranges"), ValueInputOption: stringArg(call.Arguments, "valueInputOption")})
		return outputToolResult(call, output, errShape)
	case ToolNameAppendValues:
		output, errShape := t.service.AppendValues(ctx, valuesInputFromArgs(call.Arguments))
		return outputToolResult(call, output, errShape)
	case ToolNameClearValues:
		output, errShape := t.service.ClearValues(ctx, ReadValuesInput{SpreadsheetID: stringArg(call.Arguments, "spreadsheetId"), Range: stringArg(call.Arguments, "range")})
		return outputToolResult(call, output, errShape)
	case ToolNameAddSheet:
		output, errShape := t.service.AddSheet(ctx, SheetTitleInput{SpreadsheetID: stringArg(call.Arguments, "spreadsheetId"), Title: stringArg(call.Arguments, "title")})
		return outputToolResult(call, output, errShape)
	case ToolNameRenameSheet:
		output, errShape := t.service.RenameSheet(ctx, RenameSheetInput{SpreadsheetID: stringArg(call.Arguments, "spreadsheetId"), SheetID: int64Arg(call.Arguments, "sheetId"), Title: stringArg(call.Arguments, "title")})
		return outputToolResult(call, output, errShape)
	case ToolNameDeleteSheet:
		output, errShape := t.service.DeleteSheet(ctx, SheetIDInput{SpreadsheetID: stringArg(call.Arguments, "spreadsheetId"), SheetID: int64Arg(call.Arguments, "sheetId")})
		return outputToolResult(call, output, errShape)
	case ToolNameDuplicateSheet:
		output, errShape := t.service.DuplicateSheet(ctx, DuplicateSheetInput{SpreadsheetID: stringArg(call.Arguments, "spreadsheetId"), SourceSheetID: int64Arg(call.Arguments, "sourceSheetId"), NewTitle: stringArg(call.Arguments, "newTitle")})
		return outputToolResult(call, output, errShape)
	default:
		return tools.ToolNotFoundResult(call)
	}
}

func RegisterTools(registry *tools.ToolRegistry, service *Service) error {
	for _, name := range []string{ToolNameGetSpreadsheet, ToolNameReadValues, ToolNameBatchGetValues, ToolNameCreateSpreadsheet, ToolNameUpdateValues, ToolNameBatchUpdateValues, ToolNameAppendValues, ToolNameClearValues, ToolNameAddSheet, ToolNameRenameSheet, ToolNameDeleteSheet, ToolNameDuplicateSheet} {
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
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  string(data),
		ContentForUser: sheetsUserSummary(call.Name, output),
		ArtifactRef:    sheetsArtifactRef(output),
		Metadata:       sheetsResultMetadata(output),
	}
}

func sheetsUserSummary(toolName string, output any) string {
	switch toolName {
	case ToolNameGetSpreadsheet:
		if out, ok := output.(gsheets.SpreadsheetSummary); ok {
			return fmt.Sprintf("Đã đọc bảng tính %s", firstNonEmpty(out.Title, out.ID))
		}
	case ToolNameReadValues:
		return "Đã đọc dữ liệu từ bảng tính"
	case ToolNameBatchGetValues:
		if out, ok := output.(gsheets.BatchValuesOutput); ok {
			return fmt.Sprintf("Đã đọc dữ liệu từ %d vùng trong bảng tính", len(out.Ranges))
		}
		return "Đã đọc dữ liệu từ bảng tính"
	case ToolNameCreateSpreadsheet:
		if out, ok := output.(gsheets.SpreadsheetSummary); ok {
			return fmt.Sprintf("Đã tạo bảng tính %s", firstNonEmpty(out.Title, out.ID))
		}
		return "Đã tạo bảng tính"
	case ToolNameUpdateValues, ToolNameBatchUpdateValues:
		return "Đã cập nhật dữ liệu trong bảng tính"
	case ToolNameAppendValues:
		return "Đã thêm dữ liệu vào bảng tính"
	case ToolNameClearValues:
		return "Đã xóa dữ liệu trong bảng tính"
	case ToolNameAddSheet:
		return "Đã thêm trang mới vào bảng tính"
	case ToolNameRenameSheet:
		return "Đã đổi tên trang trong bảng tính"
	case ToolNameDeleteSheet:
		return "Đã xóa trang trong bảng tính"
	case ToolNameDuplicateSheet:
		return "Đã nhân bản trang trong bảng tính"
	}
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Sprintf("%#v", output)
	}
	return string(data)
}

func sheetsArtifactRef(output any) *tools.ToolArtifactRef {
	switch v := output.(type) {
	case gsheets.SpreadsheetSummary:
		return spreadsheetArtifactRef(v.ID, v.Title, v.SpreadsheetURL)
	case gsheets.ValuesOutput:
		return spreadsheetArtifactRef(v.SpreadsheetID, "", "")
	case gsheets.BatchValuesOutput:
		return spreadsheetArtifactRef(v.SpreadsheetID, "", "")
	case gsheets.WriteValuesOutput:
		return spreadsheetArtifactRef(v.SpreadsheetID, "", "")
	case gsheets.AppendValuesOutput:
		return spreadsheetArtifactRef(v.SpreadsheetID, "", "")
	case gsheets.ClearValuesOutput:
		return spreadsheetArtifactRef(v.SpreadsheetID, "", "")
	}
	return nil
}

func spreadsheetArtifactRef(id string, title string, uri string) *tools.ToolArtifactRef {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	uri = strings.TrimSpace(uri)
	if uri == "" {
		uri = "https://docs.google.com/spreadsheets/d/" + id + "/edit"
	}
	return &tools.ToolArtifactRef{
		Kind:  "google.sheets.spreadsheet",
		Label: firstNonEmpty(title, "Google Sheets spreadsheet"),
		URI:   uri,
		ID:    id,
	}
}

func sheetsResultMetadata(output any) map[string]any {
	meta := map[string]any{}
	switch v := output.(type) {
	case gsheets.SpreadsheetSummary:
		meta["sheet_count"] = len(v.Sheets)
	case gsheets.ValuesOutput:
		meta["range"] = v.Range
		meta["row_count"] = len(v.Values)
		meta["major_dimension"] = v.MajorDimension
	case gsheets.BatchValuesOutput:
		meta["range_count"] = len(v.Ranges)
	case gsheets.WriteValuesOutput:
		meta["updated_range"] = v.UpdatedRange
		meta["updated_rows"] = v.UpdatedRows
		meta["updated_columns"] = v.UpdatedColumns
		meta["updated_cells"] = v.UpdatedCells
	case gsheets.AppendValuesOutput:
		meta["table_range"] = v.TableRange
		meta["updated_range"] = v.UpdatedRange
		meta["updated_rows"] = v.UpdatedRows
		meta["updated_columns"] = v.UpdatedColumns
		meta["updated_cells"] = v.UpdatedCells
	case gsheets.ClearValuesOutput:
		meta["cleared_range"] = v.ClearedRange
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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

func batchValuesSchema() tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{
		"spreadsheetId":    map[string]any{"type": "string"},
		"ranges": map[string]any{
				"type":                 "object",
				"description":          "Map of A1 notation range to a 2D array of cell values. Each key is a range string (e.g. \"Sheet1!A1:B2\"), each value is an array of rows where each row is an array of cell values. Example: {\"Sheet1!A1:B2\": [[\"Name\", \"Score\"], [\"Alice\", 90]]}. Do NOT pass a flat array as the value — it must be a 2D array (array of rows).",
				"additionalProperties": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "array", "items": map[string]any{}},
				},
			},
		"valueInputOption": map[string]any{"type": "string", "enum": []string{"USER_ENTERED", "RAW"}, "description": "Omit to use USER_ENTERED."},
	}, "required": []string{"spreadsheetId", "ranges"}, "additionalProperties": false}
}

func sheetTitleSchema() tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{
		"spreadsheetId": map[string]any{"type": "string"},
		"title":         map[string]any{"type": "string"},
	}, "required": []string{"spreadsheetId", "title"}, "additionalProperties": false}
}

func sheetIDSchema(name string) tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{
		"spreadsheetId": map[string]any{"type": "string"},
		name:            map[string]any{"type": "number"},
	}, "required": []string{"spreadsheetId", name}, "additionalProperties": false}
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

func rangeValuesArg(args map[string]any, name string) map[string][][]any {
	if args == nil {
		return nil
	}
	raw, ok := args[name].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string][][]any, len(raw))
	for key, value := range raw {
		rows, ok := value.([]any)
		if !ok {
			continue
		}
		converted := make([][]any, 0, len(rows))
		for _, row := range rows {
			rowValues, ok := row.([]any)
			if !ok {
				continue
			}
			converted = append(converted, rowValues)
		}
		if len(converted) > 0 {
			out[key] = converted
		}
	}
	return out
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

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
