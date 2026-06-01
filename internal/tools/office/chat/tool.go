package chat

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	chatconnector "vclaw/internal/connectors/google/chat"
	"vclaw/internal/tools"

	"google.golang.org/api/googleapi"
)

const (
	ToolNameListMessages = "chat.listMessages"
	ToolNameSendMessage  = "chat.sendMessage"
)

const (
	defaultMaxResults = int64(10)
	maxAllowedResults = int64(50)
)

type ToolRegistryEntry struct {
	Name             string
	Owner            string
	Description      string
	DefaultRiskLevel string
	RequiresApproval bool
}

var RegistryEntries = []ToolRegistryEntry{
	{
		Name:             ToolNameListMessages,
		Owner:            "integration",
		Description:      "List messages in a Google Chat space.",
		DefaultRiskLevel: "safe_read",
		RequiresApproval: false,
	},
	{
		Name:             ToolNameSendMessage,
		Owner:            "integration",
		Description:      "Send a Google Chat message, including a new message or thread reply.",
		DefaultRiskLevel: "external_write",
		RequiresApproval: true,
	},
}

type ErrorShape struct {
	Code      string
	Message   string
	Retryable bool
}

type Connector interface {
	ListMessages(ctx context.Context, parent string, pageSize int64, pageToken string, showDeleted bool) (chatconnector.ListMessagesOutput, error)
	CreateTextMessage(ctx context.Context, parent string, text string, options chatconnector.MessageCreateOptions) (chatconnector.Message, error)
}

type Service struct {
	connector Connector
}

func NewService(connector Connector) *Service {
	return &Service{connector: connector}
}

type ListMessagesInput struct {
	Space       string
	MaxResults  int64
	PageToken   string
	ShowDeleted bool
}

type ListMessagesOutput struct {
	Messages      []chatconnector.Message
	NextPageToken string
}

type SendMessageInput struct {
	Space              string
	Text               string
	ThreadName         string
	ThreadKey          string
	MessageReplyOption string
	MessageID          string
	RequestID          string
	CardTitle          string
	CardSubtitle       string
	CardText           string
}

type SendMessageOutput struct {
	Message chatconnector.Message
}

func (s *Service) ListMessages(ctx context.Context, input ListMessagesInput) (ListMessagesOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return ListMessagesOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "chat connector is not configured"}
	}
	if strings.TrimSpace(input.Space) == "" {
		return ListMessagesOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "space is required"}
	}

	maxResults := input.MaxResults
	if maxResults == 0 {
		maxResults = defaultMaxResults
	}
	if maxResults < 1 || maxResults > maxAllowedResults {
		return ListMessagesOutput{}, &ErrorShape{
			Code:    "INVALID_INPUT",
			Message: fmt.Sprintf("maxResults must be between 1 and %d", maxAllowedResults),
		}
	}

	output, err := s.connector.ListMessages(ctx, input.Space, maxResults, input.PageToken, input.ShowDeleted)
	if err != nil {
		return ListMessagesOutput{}, MapError(err)
	}
	return ListMessagesOutput{
		Messages:      output.Messages,
		NextPageToken: output.NextPageToken,
	}, nil
}

func (s *Service) SendMessage(ctx context.Context, input SendMessageInput) (SendMessageOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return SendMessageOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "chat connector is not configured"}
	}
	if strings.TrimSpace(input.Space) == "" {
		return SendMessageOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "space is required"}
	}
	if strings.TrimSpace(input.CardTitle) != "" || strings.TrimSpace(input.CardText) != "" || strings.TrimSpace(input.CardSubtitle) != "" {
		return SendMessageOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "Google Chat card messages are not supported by the current user OAuth flow; send a text message instead"}
	}
	if strings.TrimSpace(input.Text) == "" {
		return SendMessageOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "text is required"}
	}

	options := chatconnector.MessageCreateOptions{
		ThreadName:         input.ThreadName,
		ThreadKey:          input.ThreadKey,
		MessageReplyOption: input.MessageReplyOption,
		MessageID:          input.MessageID,
		RequestID:          input.RequestID,
	}

	message, err := s.connector.CreateTextMessage(ctx, input.Space, input.Text, options)
	if err != nil {
		return SendMessageOutput{}, MapError(err)
	}
	return SendMessageOutput{Message: message}, nil
}

type ListMessagesTool struct {
	service *Service
}

func NewListMessagesTool(service *Service) ListMessagesTool {
	return ListMessagesTool{service: service}
}

func (ListMessagesTool) Name() string {
	return ToolNameListMessages
}

func (ListMessagesTool) Description() string {
	return "List Google Chat messages from a space."
}

func (ListMessagesTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"space":       map[string]any{"type": "string"},
			"maxResults":  map[string]any{"type": "number"},
			"pageToken":   map[string]any{"type": "string"},
			"showDeleted": map[string]any{"type": "boolean"},
		},
		"required":             []string{"space"},
		"additionalProperties": false,
	}
}

func (ListMessagesTool) Capability() tools.Capability {
	return tools.CapabilityReadOnly
}

func (ListMessagesTool) RiskLevel() tools.RiskLevel {
	return tools.RiskLevelSafeRead
}

func (t ListMessagesTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.ListMessages(ctx, ListMessagesInput{
		Space:       stringArg(call.Arguments, "space"),
		MaxResults:  int64Arg(call.Arguments, "maxResults"),
		PageToken:   stringArg(call.Arguments, "pageToken"),
		ShowDeleted: boolArg(call.Arguments, "showDeleted"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}

	lines := []string{}
	for _, message := range output.Messages {
		lines = append(lines, fmt.Sprintf("- %s | %s | %s", message.Name, message.Sender, message.Text))
	}
	if len(lines) == 0 {
		lines = append(lines, "No Chat messages found.")
	}
	if strings.TrimSpace(output.NextPageToken) != "" {
		lines = append(lines, "Next page token: "+output.NextPageToken)
	}
	content := strings.Join(lines, "\n")
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
	}
}

type SendMessageTool struct {
	service *Service
}

func NewSendMessageTool(service *Service) SendMessageTool {
	return SendMessageTool{service: service}
}

func (SendMessageTool) Name() string {
	return ToolNameSendMessage
}

func (SendMessageTool) Description() string {
	return "Send a Google Chat text message. This external write requires approval."
}

func (SendMessageTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"space":              map[string]any{"type": "string"},
			"text":               map[string]any{"type": "string"},
			"threadName":         map[string]any{"type": "string"},
			"threadKey":          map[string]any{"type": "string"},
			"messageReplyOption": map[string]any{"type": "string"},
			"messageId":          map[string]any{"type": "string"},
			"requestId":          map[string]any{"type": "string"},
		},
		"required":             []string{"space", "text"},
		"additionalProperties": false,
	}
}

func (SendMessageTool) Capability() tools.Capability {
	return tools.CapabilityMutating
}

func (SendMessageTool) RiskLevel() tools.RiskLevel {
	return tools.RiskLevelExternalWrite
}

func (t SendMessageTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.SendMessage(ctx, SendMessageInput{
		Space:              stringArg(call.Arguments, "space"),
		Text:               stringArg(call.Arguments, "text"),
		ThreadName:         stringArg(call.Arguments, "threadName"),
		ThreadKey:          stringArg(call.Arguments, "threadKey"),
		MessageReplyOption: stringArg(call.Arguments, "messageReplyOption"),
		MessageID:          stringArg(call.Arguments, "messageId"),
		RequestID:          stringArg(call.Arguments, "requestId"),
		CardTitle:          stringArg(call.Arguments, "cardTitle"),
		CardSubtitle:       stringArg(call.Arguments, "cardSubtitle"),
		CardText:           stringArg(call.Arguments, "cardText"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}

	content := "Sent Google Chat message: " + output.Message.Name
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
	}
}

func MapError(err error) *ErrorShape {
	if err == nil {
		return nil
	}
	gerr, ok := err.(*googleapi.Error)
	if !ok {
		return &ErrorShape{Code: "INTERNAL_ERROR", Message: err.Error()}
	}

	switch {
	case gerr.Code == http.StatusUnauthorized:
		return &ErrorShape{Code: "AUTH_EXPIRED", Message: gerr.Message, Retryable: true}
	case gerr.Code == http.StatusForbidden && hasMissingScopeReason(gerr):
		return &ErrorShape{Code: "AUTH_MISSING_SCOPE", Message: gerr.Message}
	case gerr.Code == http.StatusTooManyRequests:
		return &ErrorShape{Code: "RATE_LIMITED", Message: gerr.Message, Retryable: true}
	case gerr.Code >= 500:
		return &ErrorShape{Code: "PROVIDER_UNAVAILABLE", Message: gerr.Message, Retryable: true}
	default:
		return &ErrorShape{Code: "INTERNAL_ERROR", Message: gerr.Message}
	}
}

func hasMissingScopeReason(err *googleapi.Error) bool {
	text := strings.ToLower(err.Message)
	return strings.Contains(text, "insufficient authentication scopes") ||
		strings.Contains(text, "insufficient permissions")
}

func toolErrorResult(call tools.ToolCall, errShape *ErrorShape) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  errShape.Code + ": " + errShape.Message,
		ContentForUser: errShape.Message,
		Error: &tools.ToolError{
			Code:    errShape.Code,
			Message: errShape.Message,
		},
	}
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
