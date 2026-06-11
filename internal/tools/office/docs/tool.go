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
)

type ToolRegistryEntry struct {
	Name             string
	Owner            string
	Description      string
	DefaultRiskLevel string
	RequiresApproval bool
}

var RegistryEntries = []ToolRegistryEntry{
	{Name: ToolNameGetDocument, Owner: "integration", Description: "Read a Google Docs document.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameCreateDocument, Owner: "integration", Description: "Create a Google Docs document.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameAppendText, Owner: "integration", Description: "Append text to a Google Docs document.", DefaultRiskLevel: "external_write", RequiresApproval: true},
}

type Connector interface {
	GetDocument(ctx context.Context, documentID string) (gdocs.Document, error)
	CreateDocument(ctx context.Context, title string) (gdocs.Document, error)
	AppendText(ctx context.Context, documentID string, text string) (gdocs.AppendTextOutput, error)
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
		return tools.RiskLevelSafeRead
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
	default:
		return tools.ToolNotFoundResult(call)
	}
}

func RegisterTools(registry *tools.ToolRegistry, service *Service) error {
	for _, name := range []string{ToolNameGetDocument, ToolNameCreateDocument, ToolNameAppendText} {
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
