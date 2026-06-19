package docs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vclaw/internal/connectors/google/common"
	gdocs "vclaw/internal/connectors/google/docs"
	"vclaw/internal/tools"
)

const (
	ToolNameGetDocument    = "docs.getDocument"
	ToolNameCreateDocument = "docs.createDocument"
	ToolNameAppendText     = "docs.appendText"
	ToolNameReplaceText    = "docs.replaceText"
	ToolNameInsertText     = "docs.insertText"
	ToolNameDeleteContent  = "docs.deleteContent"
)

type ToolRegistryEntry struct {
	Name             string
	Owner            string
	Description      string
	DefaultRiskLevel string
	RequiresApproval bool
}

var RegistryEntries = []ToolRegistryEntry{
	{Name: ToolNameGetDocument, Owner: "integration", Description: "Read a Google Docs document in full or preview mode.", DefaultRiskLevel: "sensitive_read", RequiresApproval: true},
	{Name: ToolNameCreateDocument, Owner: "integration", Description: "Create a Google Docs document.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameAppendText, Owner: "integration", Description: "Append text to a Google Docs document.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameReplaceText, Owner: "integration", Description: "Replace matching text in a Google Docs document.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameInsertText, Owner: "integration", Description: "Insert text at a Google Docs structural index.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameDeleteContent, Owner: "integration", Description: "Delete content from a Google Docs structural range.", DefaultRiskLevel: "external_write", RequiresApproval: true},
}

type Connector interface {
	GetDocument(ctx context.Context, documentID string) (gdocs.Document, error)
	CreateDocument(ctx context.Context, title string) (gdocs.Document, error)
	AppendText(ctx context.Context, documentID string, text string) (gdocs.AppendTextOutput, error)
	ReplaceText(ctx context.Context, documentID string, oldText string, newText string, matchCase bool) (gdocs.EditTextOutput, error)
	InsertText(ctx context.Context, documentID string, index int64, text string) (gdocs.EditTextOutput, error)
	DeleteContent(ctx context.Context, documentID string, startIndex int64, endIndex int64) (gdocs.EditTextOutput, error)
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

type GetDocumentInput struct {
	DocumentID   string
	PreviewChars int
	Full         bool
}

type DocumentOutput struct {
	Document     gdocs.Document
	Text         string
	Truncated    bool
	PreviewChars int
}

type CreateDocumentInput struct {
	Title string
}

type AppendTextInput struct {
	DocumentID string
	Text       string
}

type ReplaceTextInput struct {
	DocumentID string
	OldText    string
	NewText    string
	MatchCase  bool
}

type InsertTextInput struct {
	DocumentID string
	Index      int64
	Text       string
}

type DeleteContentInput struct {
	DocumentID string
	StartIndex int64
	EndIndex   int64
}

func (s *Service) GetDocument(ctx context.Context, input GetDocumentInput) (DocumentOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return DocumentOutput{}, errShape
	}
	if strings.TrimSpace(input.DocumentID) == "" {
		return DocumentOutput{}, invalidInput("documentId is required")
	}
	document, err := s.connector.GetDocument(ctx, input.DocumentID)
	if err != nil {
		return DocumentOutput{}, mapError(err)
	}
	text, truncated, previewChars := previewText(document.BodyText, input.Full, input.PreviewChars)
	return DocumentOutput{Document: document, Text: text, Truncated: truncated, PreviewChars: previewChars}, nil
}

func (s *Service) CreateDocument(ctx context.Context, input CreateDocumentInput) (gdocs.Document, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdocs.Document{}, errShape
	}
	if strings.TrimSpace(input.Title) == "" {
		return gdocs.Document{}, invalidInput("title is required")
	}
	document, err := s.connector.CreateDocument(ctx, input.Title)
	if err != nil {
		return gdocs.Document{}, mapError(err)
	}
	return document, nil
}

func (s *Service) AppendText(ctx context.Context, input AppendTextInput) (gdocs.AppendTextOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdocs.AppendTextOutput{}, errShape
	}
	if strings.TrimSpace(input.DocumentID) == "" {
		return gdocs.AppendTextOutput{}, invalidInput("documentId is required")
	}
	if strings.TrimSpace(input.Text) == "" {
		return gdocs.AppendTextOutput{}, invalidInput("text is required")
	}
	output, err := s.connector.AppendText(ctx, input.DocumentID, input.Text)
	if err != nil {
		return gdocs.AppendTextOutput{}, mapError(err)
	}
	return output, nil
}

func (s *Service) ReplaceText(ctx context.Context, input ReplaceTextInput) (gdocs.EditTextOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdocs.EditTextOutput{}, errShape
	}
	if strings.TrimSpace(input.DocumentID) == "" {
		return gdocs.EditTextOutput{}, invalidInput("documentId is required")
	}
	if strings.TrimSpace(input.OldText) == "" {
		return gdocs.EditTextOutput{}, invalidInput("oldText is required")
	}
	output, err := s.connector.ReplaceText(ctx, input.DocumentID, input.OldText, input.NewText, input.MatchCase)
	if err != nil {
		return gdocs.EditTextOutput{}, mapError(err)
	}
	return output, nil
}

func (s *Service) InsertText(ctx context.Context, input InsertTextInput) (gdocs.EditTextOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdocs.EditTextOutput{}, errShape
	}
	if strings.TrimSpace(input.DocumentID) == "" {
		return gdocs.EditTextOutput{}, invalidInput("documentId is required")
	}
	if input.Index < 1 {
		return gdocs.EditTextOutput{}, invalidInput("index must be >= 1")
	}
	if strings.TrimSpace(input.Text) == "" {
		return gdocs.EditTextOutput{}, invalidInput("text is required")
	}
	output, err := s.connector.InsertText(ctx, input.DocumentID, input.Index, input.Text)
	if err != nil {
		return gdocs.EditTextOutput{}, mapError(err)
	}
	return output, nil
}

func (s *Service) DeleteContent(ctx context.Context, input DeleteContentInput) (gdocs.EditTextOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return gdocs.EditTextOutput{}, errShape
	}
	if strings.TrimSpace(input.DocumentID) == "" {
		return gdocs.EditTextOutput{}, invalidInput("documentId is required")
	}
	if input.StartIndex < 1 {
		return gdocs.EditTextOutput{}, invalidInput("startIndex must be >= 1")
	}
	if input.EndIndex <= input.StartIndex {
		return gdocs.EditTextOutput{}, invalidInput("endIndex must be greater than startIndex")
	}
	output, err := s.connector.DeleteContent(ctx, input.DocumentID, input.StartIndex, input.EndIndex)
	if err != nil {
		return gdocs.EditTextOutput{}, mapError(err)
	}
	return output, nil
}

func (s *Service) validateConnector() *ErrorShape {
	if s == nil || s.connector == nil {
		return internalError("docs connector is not configured")
	}
	return nil
}

type DocsTool struct {
	name    string
	service *Service
}

func NewTool(name string, service *Service) DocsTool {
	return DocsTool{name: name, service: service}
}

func (t DocsTool) Name() string { return t.name }

func (t DocsTool) Description() string {
	switch t.name {
	case ToolNameGetDocument:
		return "Read the text content and metadata of a Google Docs document by documentId."
	case ToolNameCreateDocument:
		return "Create a Google Docs document. Requires human approval before execution."
	case ToolNameAppendText:
		return "Append text to an existing Google Docs document. Requires human approval before execution."
	case ToolNameReplaceText:
		return "Replace all matching text in an existing Google Docs document. Requires human approval before execution."
	case ToolNameInsertText:
		return "Insert text at a Google Docs structural index. Requires human approval before execution."
	case ToolNameDeleteContent:
		return "Delete content from a Google Docs structural range. Requires human approval before execution."
	default:
		return "Google Docs tool."
	}
}

func (t DocsTool) Parameters() tools.ToolSchema {
	switch t.name {
	case ToolNameGetDocument:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"documentId":   map[string]any{"type": "string"},
			"previewChars": map[string]any{"type": "number", "description": "Omit to preview the first 4000 characters."},
			"full":         map[string]any{"type": "boolean", "description": "Return full extracted text when true."},
		}, "required": []string{"documentId"}, "additionalProperties": false}
	case ToolNameCreateDocument:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}}, "required": []string{"title"}, "additionalProperties": false}
	case ToolNameAppendText:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"documentId": map[string]any{"type": "string"},
			"text":       map[string]any{"type": "string"},
		}, "required": []string{"documentId", "text"}, "additionalProperties": false}
	case ToolNameReplaceText:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"documentId": map[string]any{"type": "string"},
			"oldText":    map[string]any{"type": "string", "description": "Text to find and replace."},
			"newText":    map[string]any{"type": "string", "description": "Replacement text. Can be empty to remove matches."},
			"matchCase":  map[string]any{"type": "boolean"},
		}, "required": []string{"documentId", "oldText", "newText"}, "additionalProperties": false}
	case ToolNameInsertText:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"documentId": map[string]any{"type": "string"},
			"index":      map[string]any{"type": "number", "description": "Google Docs structural index where text should be inserted."},
			"text":       map[string]any{"type": "string"},
		}, "required": []string{"documentId", "index", "text"}, "additionalProperties": false}
	case ToolNameDeleteContent:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{
			"documentId": map[string]any{"type": "string"},
			"startIndex": map[string]any{"type": "number"},
			"endIndex":   map[string]any{"type": "number"},
		}, "required": []string{"documentId", "startIndex", "endIndex"}, "additionalProperties": false}
	default:
		return tools.ToolSchema{"type": "object"}
	}
}

func (t DocsTool) Capability() tools.Capability {
	if t.name == ToolNameGetDocument {
		return tools.CapabilityReadOnly
	}
	return tools.CapabilityMutating
}

func (t DocsTool) RiskLevel() tools.RiskLevel {
	if t.name == ToolNameGetDocument {
		return tools.RiskLevelSensitiveRead
	}
	return tools.RiskLevelExternalWrite
}

func (t DocsTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	switch t.name {
	case ToolNameGetDocument:
		output, errShape := t.service.GetDocument(ctx, GetDocumentInput{DocumentID: stringArg(call.Arguments, "documentId"), PreviewChars: intArg(call.Arguments, "previewChars"), Full: boolArg(call.Arguments, "full")})
		return outputToolResult(call, output, errShape)
	case ToolNameCreateDocument:
		output, errShape := t.service.CreateDocument(ctx, CreateDocumentInput{Title: stringArg(call.Arguments, "title")})
		return outputToolResult(call, output, errShape)
	case ToolNameAppendText:
		output, errShape := t.service.AppendText(ctx, AppendTextInput{DocumentID: stringArg(call.Arguments, "documentId"), Text: stringArg(call.Arguments, "text")})
		return outputToolResult(call, output, errShape)
	case ToolNameReplaceText:
		output, errShape := t.service.ReplaceText(ctx, ReplaceTextInput{DocumentID: stringArg(call.Arguments, "documentId"), OldText: stringArg(call.Arguments, "oldText"), NewText: stringArg(call.Arguments, "newText"), MatchCase: boolArg(call.Arguments, "matchCase")})
		return outputToolResult(call, output, errShape)
	case ToolNameInsertText:
		output, errShape := t.service.InsertText(ctx, InsertTextInput{DocumentID: stringArg(call.Arguments, "documentId"), Index: int64Arg(call.Arguments, "index"), Text: stringArg(call.Arguments, "text")})
		return outputToolResult(call, output, errShape)
	case ToolNameDeleteContent:
		output, errShape := t.service.DeleteContent(ctx, DeleteContentInput{DocumentID: stringArg(call.Arguments, "documentId"), StartIndex: int64Arg(call.Arguments, "startIndex"), EndIndex: int64Arg(call.Arguments, "endIndex")})
		return outputToolResult(call, output, errShape)
	default:
		return tools.ToolNotFoundResult(call)
	}
}

func RegisterTools(registry *tools.ToolRegistry, service *Service) error {
	for _, name := range []string{ToolNameGetDocument, ToolNameCreateDocument, ToolNameAppendText, ToolNameReplaceText, ToolNameInsertText, ToolNameDeleteContent} {
		if err := registry.RegisterWithEntry(NewTool(name, service), tools.ToolRegistryEntry{Owner: "integration", Group: "google_workspace"}); err != nil {
			return err
		}
	}
	return nil
}

func previewText(text string, full bool, previewChars int) (string, bool, int) {
	if full {
		return text, false, 0
	}
	if previewChars <= 0 {
		previewChars = 4000
	}
	runes := []rune(text)
	if len(runes) <= previewChars {
		return text, false, previewChars
	}
	return string(runes[:previewChars]), true, previewChars
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
		ContentForUser: docsUserSummary(call.Name, output),
		ArtifactRef:    docsArtifactRef(output),
		Metadata:       docsResultMetadata(output),
		Truncated:      docsResultTruncated(output),
	}
}

func docsUserSummary(toolName string, output any) string {
	switch toolName {
	case ToolNameGetDocument:
		if out, ok := output.(DocumentOutput); ok {
			return fmt.Sprintf("Đã đọc tài liệu %s", firstNonEmpty(out.Document.Title, out.Document.ID))
		}
	case ToolNameCreateDocument:
		if out, ok := output.(gdocs.Document); ok {
			return fmt.Sprintf("Đã tạo tài liệu %s", firstNonEmpty(out.Title, out.ID))
		}
	case ToolNameAppendText, ToolNameInsertText:
		return "Đã thêm nội dung vào tài liệu"
	case ToolNameReplaceText:
		return "Đã thay thế nội dung trong tài liệu"
	case ToolNameDeleteContent:
		return "Đã xóa nội dung trong tài liệu"
	}
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Sprintf("%#v", output)
	}
	return string(data)
}

func docsArtifactRef(output any) *tools.ToolArtifactRef {
	switch v := output.(type) {
	case DocumentOutput:
		return documentArtifactRef(v.Document.ID, v.Document.Title)
	case gdocs.Document:
		return documentArtifactRef(v.ID, v.Title)
	case gdocs.AppendTextOutput:
		return documentArtifactRef(v.DocumentID, v.Title)
	case gdocs.EditTextOutput:
		return documentArtifactRef(v.DocumentID, v.Title)
	}
	return nil
}

func documentArtifactRef(id string, title string) *tools.ToolArtifactRef {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	return &tools.ToolArtifactRef{
		Kind:  "google.docs.document",
		Label: firstNonEmpty(title, "Google Docs document"),
		URI:   "https://docs.google.com/document/d/" + strings.TrimSpace(id) + "/edit",
		ID:    strings.TrimSpace(id),
	}
}

func docsResultMetadata(output any) map[string]any {
	meta := map[string]any{}
	switch v := output.(type) {
	case DocumentOutput:
		meta["text_chars"] = len([]rune(v.Text))
		meta["preview_chars"] = v.PreviewChars
		if strings.TrimSpace(v.Document.Revision) != "" {
			meta["revision"] = v.Document.Revision
		}
	case gdocs.Document:
		meta["text_chars"] = len([]rune(v.BodyText))
		if strings.TrimSpace(v.Revision) != "" {
			meta["revision"] = v.Revision
		}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func docsResultTruncated(output any) bool {
	document, ok := output.(DocumentOutput)
	return ok && document.Truncated
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

func stringArg(args map[string]any, name string) string {
	if args == nil {
		return ""
	}
	value, _ := args[name].(string)
	return value
}

func boolArg(args map[string]any, name string) bool {
	if args == nil {
		return false
	}
	value, _ := args[name].(bool)
	return value
}

func intArg(args map[string]any, name string) int {
	if args == nil {
		return 0
	}
	switch value := args[name].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func int64Arg(args map[string]any, name string) int64 {
	if args == nil {
		return 0
	}
	switch value := args[name].(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	default:
		return 0
	}
}
