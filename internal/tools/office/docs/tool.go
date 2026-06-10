package docs

import (
	"context"
	"net/http"
	"strings"

	googleconnector "vclaw/internal/connectors/google"
	docsconnector "vclaw/internal/connectors/google/docs"
	"vclaw/internal/tools"

	"google.golang.org/api/googleapi"
)

const (
	ToolNameGetDocument    = "docs.getDocument"
	ToolNameCreateDocument = "docs.createDocument"
	ToolNameAppendText     = "docs.appendText"

	maxDocumentTextChars = 30000
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
	GetDocument(ctx context.Context, documentID string) (docsconnector.Document, error)
	CreateDocument(ctx context.Context, title string) (docsconnector.Document, error)
	AppendText(ctx context.Context, documentID string, text string) (docsconnector.Document, error)
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
	DocumentID string
	Full       bool
}

type CreateDocumentInput struct {
	Title string
	Text  string
}

type AppendTextInput struct {
	DocumentID string
	Text       string
}

func (s *Service) GetDocument(ctx context.Context, input GetDocumentInput) (docsconnector.Document, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return docsconnector.Document{}, errShape
	}
	if strings.TrimSpace(input.DocumentID) == "" {
		return docsconnector.Document{}, invalidInput("documentId is required")
	}
	output, err := s.connector.GetDocument(ctx, strings.TrimSpace(input.DocumentID))
	if err != nil {
		return docsconnector.Document{}, MapError(err)
	}
	if !input.Full {
		output.Text = truncate(output.Text, maxDocumentTextChars)
	}
	return output, nil
}

func (s *Service) CreateDocument(ctx context.Context, input CreateDocumentInput) (docsconnector.Document, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return docsconnector.Document{}, errShape
	}
	if strings.TrimSpace(input.Title) == "" {
		return docsconnector.Document{}, invalidInput("title is required")
	}
	output, err := s.connector.CreateDocument(ctx, strings.TrimSpace(input.Title))
	if err != nil {
		return docsconnector.Document{}, MapError(err)
	}
	if strings.TrimSpace(input.Text) != "" {
		output, err = s.connector.AppendText(ctx, output.ID, input.Text)
		if err != nil {
			return docsconnector.Document{}, MapError(err)
		}
	}
	return output, nil
}

func (s *Service) AppendText(ctx context.Context, input AppendTextInput) (docsconnector.Document, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return docsconnector.Document{}, errShape
	}
	if strings.TrimSpace(input.DocumentID) == "" {
		return docsconnector.Document{}, invalidInput("documentId is required")
	}
	if strings.TrimSpace(input.Text) == "" {
		return docsconnector.Document{}, invalidInput("text is required")
	}
	output, err := s.connector.AppendText(ctx, strings.TrimSpace(input.DocumentID), input.Text)
	if err != nil {
		return docsconnector.Document{}, MapError(err)
	}
	return output, nil
}

func (s *Service) validateConnector() *ErrorShape {
	if s == nil || s.connector == nil {
		return &ErrorShape{Code: "INTERNAL_ERROR", Message: "docs connector is not configured"}
	}
	return nil
}

type docsTool struct {
	name    string
	service *Service
}

func newTool(name string, service *Service) docsTool {
	return docsTool{name: name, service: service}
}

func (t docsTool) Name() string { return t.name }

func (t docsTool) Description() string {
	switch t.name {
	case ToolNameGetDocument:
		return "Read a Google Docs document and return plain text extracted from the body."
	case ToolNameCreateDocument:
		return "Create a Google Docs document, optionally with initial text. This external write requires approval."
	case ToolNameAppendText:
		return "Append text to an existing Google Docs document. This external write requires approval."
	default:
		return "Google Docs tool."
	}
}

func (t docsTool) Parameters() tools.ToolSchema {
	switch t.name {
	case ToolNameGetDocument:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"documentId": map[string]any{"type": "string"}, "full": map[string]any{"type": "boolean"}}, "required": []string{"documentId"}, "additionalProperties": false}
	case ToolNameCreateDocument:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}, "text": map[string]any{"type": "string"}}, "required": []string{"title"}, "additionalProperties": false}
	case ToolNameAppendText:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"documentId": map[string]any{"type": "string"}, "text": map[string]any{"type": "string"}}, "required": []string{"documentId", "text"}, "additionalProperties": false}
	default:
		return tools.ToolSchema{"type": "object"}
	}
}

func (t docsTool) Capability() tools.Capability {
	if t.name == ToolNameGetDocument {
		return tools.CapabilityReadOnly
	}
	return tools.CapabilityMutating
}

func (t docsTool) RiskLevel() tools.RiskLevel {
	if t.name == ToolNameGetDocument {
		return tools.RiskLevelSafeRead
	}
	return tools.RiskLevelExternalWrite
}

func (t docsTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	switch t.name {
	case ToolNameGetDocument:
		output, errShape := t.service.GetDocument(ctx, GetDocumentInput{DocumentID: stringArg(call.Arguments, "documentId"), Full: boolArg(call.Arguments, "full")})
		return outputToolResult(call, output, errShape)
	case ToolNameCreateDocument:
		output, errShape := t.service.CreateDocument(ctx, CreateDocumentInput{Title: stringArg(call.Arguments, "title"), Text: stringArg(call.Arguments, "text")})
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
		return &ErrorShape{Code: "PROVIDER_TIMEOUT", Message: "network error contacting Docs API: " + err.Error(), Retryable: true}
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
		return tools.ErrorResult(call, errShape.Code, errShape.Message)
	}
	return tools.SuccessResult(call, output, docsResultOptions(output)...)
}

func docsResultOptions(output any) []tools.ResultOption {
	options := []tools.ResultOption{tools.WithMetadata(map[string]any{"provider": "google_docs"})}
	doc, ok := output.(docsconnector.Document)
	if !ok {
		return options
	}
	source := tools.SourceRef{
		Kind:  "google_doc",
		ID:    doc.ID,
		Label: doc.Title,
		URI:   "https://docs.google.com/document/d/" + doc.ID + "/edit",
		Meta:  map[string]any{"revisionId": doc.RevisionID},
	}
	options = append(options, tools.WithSourceRefs(source))
	options = append(options, tools.WithArtifactRef(tools.ArtifactRef{
		Kind:  "google_doc",
		ID:    doc.ID,
		Label: doc.Title,
		URI:   source.URI,
		Meta:  map[string]any{"revisionId": doc.RevisionID},
	}))
	return options
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

func truncate(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return strings.TrimSpace(value[:max]) + "\n[truncated]"
}
