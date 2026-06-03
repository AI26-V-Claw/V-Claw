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
	ToolNameListSpaces          = "chat.listSpaces"
	ToolNameListMembers         = "chat.listMembers"
	ToolNameFindSpacesByMembers = "chat.findSpacesByMembers"
	ToolNameListMessages        = "chat.listMessages"
	ToolNameSendMessage         = "chat.sendMessage"
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
		Name:             ToolNameListSpaces,
		Owner:            "integration",
		Description:      "List Google Chat spaces available to the authenticated user.",
		DefaultRiskLevel: "safe_read",
		RequiresApproval: false,
	},
	{
		Name:             ToolNameListMembers,
		Owner:            "integration",
		Description:      "List members in a Google Chat space.",
		DefaultRiskLevel: "safe_read",
		RequiresApproval: false,
	},
	{
		Name:             ToolNameFindSpacesByMembers,
		Owner:            "integration",
		Description:      "Find Google Chat spaces that contain the requested member user resource names.",
		DefaultRiskLevel: "safe_read",
		RequiresApproval: false,
	},
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
	ListSpacesPage(ctx context.Context, pageSize int64, pageToken string) (chatconnector.ListSpacesOutput, error)
	ListMembers(ctx context.Context, parent string, pageSize int64, pageToken string) (chatconnector.ListMembersOutput, error)
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

type ListSpacesInput struct {
	MaxResults int64
	PageToken  string
}

type ListSpacesOutput struct {
	Spaces        []chatconnector.Space
	NextPageToken string
}

type ListMembersInput struct {
	Space      string
	MaxResults int64
	PageToken  string
}

type ListMembersOutput struct {
	Members       []chatconnector.Membership
	NextPageToken string
}

type FindSpacesByMembersInput struct {
	MemberUserNames []string
	MaxResults      int64
	PageToken       string
	SpaceType       string
}

type MatchedSpace struct {
	Space   chatconnector.Space
	Members []chatconnector.Membership
}

type FindSpacesByMembersOutput struct {
	Spaces        []MatchedSpace
	NextPageToken string
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

func (s *Service) ListSpaces(ctx context.Context, input ListSpacesInput) (ListSpacesOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return ListSpacesOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "chat connector is not configured"}
	}
	maxResults, errShape := normalizeMaxResults(input.MaxResults)
	if errShape != nil {
		return ListSpacesOutput{}, errShape
	}
	output, err := s.connector.ListSpacesPage(ctx, maxResults, input.PageToken)
	if err != nil {
		return ListSpacesOutput{}, MapError(err)
	}
	return ListSpacesOutput{Spaces: output.Spaces, NextPageToken: output.NextPageToken}, nil
}

func (s *Service) ListMembers(ctx context.Context, input ListMembersInput) (ListMembersOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return ListMembersOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "chat connector is not configured"}
	}
	if strings.TrimSpace(input.Space) == "" {
		return ListMembersOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "space is required"}
	}
	space := normalizeSpaceName(input.Space)
	if space == "" {
		return ListMembersOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "space must contain a resource name like spaces/AAAA"}
	}
	maxResults, errShape := normalizeMaxResults(input.MaxResults)
	if errShape != nil {
		return ListMembersOutput{}, errShape
	}
	output, err := s.connector.ListMembers(ctx, space, maxResults, input.PageToken)
	if err != nil {
		return ListMembersOutput{}, MapError(err)
	}
	return ListMembersOutput{Members: output.Members, NextPageToken: output.NextPageToken}, nil
}

func (s *Service) FindSpacesByMembers(ctx context.Context, input FindSpacesByMembersInput) (FindSpacesByMembersOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return FindSpacesByMembersOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "chat connector is not configured"}
	}

	memberNames := normalizeMemberUserNames(input.MemberUserNames)
	if len(memberNames) == 0 {
		return FindSpacesByMembersOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "memberUserNames is required"}
	}

	maxResults, errShape := normalizeMaxResults(input.MaxResults)
	if errShape != nil {
		return FindSpacesByMembersOutput{}, errShape
	}

	spacesOutput, err := s.connector.ListSpacesPage(ctx, maxResults, input.PageToken)
	if err != nil {
		return FindSpacesByMembersOutput{}, MapError(err)
	}

	required := map[string]struct{}{}
	for _, memberName := range memberNames {
		required[strings.ToLower(memberName)] = struct{}{}
	}

	var matches []MatchedSpace
	for _, space := range spacesOutput.Spaces {
		if strings.TrimSpace(input.SpaceType) != "" && !strings.EqualFold(space.SpaceType, input.SpaceType) {
			continue
		}

		membersOutput, err := s.connector.ListMembers(ctx, space.Name, maxAllowedResults, "")
		if err != nil {
			return FindSpacesByMembersOutput{}, MapError(err)
		}
		if membershipsContainAll(membersOutput.Members, required) {
			matches = append(matches, MatchedSpace{
				Space:   space,
				Members: membersOutput.Members,
			})
		}
	}

	return FindSpacesByMembersOutput{Spaces: matches, NextPageToken: spacesOutput.NextPageToken}, nil
}

func (s *Service) ListMessages(ctx context.Context, input ListMessagesInput) (ListMessagesOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return ListMessagesOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "chat connector is not configured"}
	}
	if strings.TrimSpace(input.Space) == "" {
		return ListMessagesOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "space is required"}
	}
	space := normalizeSpaceName(input.Space)
	if space == "" {
		return ListMessagesOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "space must contain a resource name like spaces/AAAA"}
	}

	maxResults, errShape := normalizeMaxResults(input.MaxResults)
	if errShape != nil {
		return ListMessagesOutput{}, errShape
	}

	output, err := s.connector.ListMessages(ctx, space, maxResults, input.PageToken, input.ShowDeleted)
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
	space := normalizeSpaceName(input.Space)
	if space == "" {
		return SendMessageOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "space must contain a resource name like spaces/AAAA"}
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

	message, err := s.connector.CreateTextMessage(ctx, space, input.Text, options)
	if err != nil {
		return SendMessageOutput{}, MapError(err)
	}
	return SendMessageOutput{Message: message}, nil
}

func normalizeMaxResults(value int64) (int64, *ErrorShape) {
	if value == 0 {
		return defaultMaxResults, nil
	}
	if value < 1 || value > maxAllowedResults {
		return 0, &ErrorShape{
			Code:    "INVALID_INPUT",
			Message: fmt.Sprintf("maxResults must be between 1 and %d", maxAllowedResults),
		}
	}
	return value, nil
}

type ListSpacesTool struct {
	service *Service
}

func NewListSpacesTool(service *Service) ListSpacesTool {
	return ListSpacesTool{service: service}
}

func (ListSpacesTool) Name() string {
	return ToolNameListSpaces
}

func (ListSpacesTool) Description() string {
	return "List Google Chat spaces available to the authenticated user. Use this before chat.listMembers when the user names people instead of a space id."
}

func (ListSpacesTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"maxResults": map[string]any{"type": "number", "minimum": 1, "maximum": maxAllowedResults},
			"pageToken":  map[string]any{"type": "string"},
		},
		"additionalProperties": false,
	}
}

func (ListSpacesTool) Capability() tools.Capability {
	return tools.CapabilityReadOnly
}

func (ListSpacesTool) RiskLevel() tools.RiskLevel {
	return tools.RiskLevelSafeRead
}

func (t ListSpacesTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.ListSpaces(ctx, ListSpacesInput{
		MaxResults: boundedInt64Arg(call.Arguments, "maxResults", defaultMaxResults, maxAllowedResults),
		PageToken:  stringArg(call.Arguments, "pageToken"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}

	lines := []string{}
	for _, space := range output.Spaces {
		lines = append(lines, fmt.Sprintf("- %s | %s | %s | %s", space.Name, emptyValue(space.DisplayName, "(no display name)"), emptyValue(space.SpaceType, emptyValue(space.Type, "(no type)")), space.SpaceURI))
	}
	if len(lines) == 0 {
		lines = append(lines, "No Google Chat spaces found.")
	}
	if strings.TrimSpace(output.NextPageToken) != "" {
		lines = append(lines, "Next page token: "+output.NextPageToken)
	}
	content := strings.Join(lines, "\n")
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true, ContentForLLM: content, ContentForUser: content}
}

type ListMembersTool struct {
	service *Service
}

func NewListMembersTool(service *Service) ListMembersTool {
	return ListMembersTool{service: service}
}

func (ListMembersTool) Name() string {
	return ToolNameListMembers
}

func (ListMembersTool) Description() string {
	return "List members in a Google Chat space. Use this to identify which unnamed space contains people mentioned by the user."
}

func (ListMembersTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"space":      map[string]any{"type": "string"},
			"maxResults": map[string]any{"type": "number", "minimum": 1, "maximum": maxAllowedResults},
			"pageToken":  map[string]any{"type": "string"},
		},
		"required":             []string{"space"},
		"additionalProperties": false,
	}
}

func (ListMembersTool) Capability() tools.Capability {
	return tools.CapabilityReadOnly
}

func (ListMembersTool) RiskLevel() tools.RiskLevel {
	return tools.RiskLevelSafeRead
}

func (t ListMembersTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.ListMembers(ctx, ListMembersInput{
		Space:      stringArg(call.Arguments, "space"),
		MaxResults: boundedInt64Arg(call.Arguments, "maxResults", defaultMaxResults, maxAllowedResults),
		PageToken:  stringArg(call.Arguments, "pageToken"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}

	lines := []string{}
	for _, member := range output.Members {
		lines = append(lines, fmt.Sprintf("- %s | %s | %s | %s | %s", member.Name, member.MemberName, emptyValue(member.DisplayName, "(no display name)"), emptyValue(member.Email, "(no email)"), member.MemberType))
	}
	if len(lines) == 0 {
		lines = append(lines, "No Chat members found.")
	}
	if strings.TrimSpace(output.NextPageToken) != "" {
		lines = append(lines, "Next page token: "+output.NextPageToken)
	}
	content := strings.Join(lines, "\n")
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true, ContentForLLM: content, ContentForUser: content}
}

type FindSpacesByMembersTool struct {
	service *Service
}

func NewFindSpacesByMembersTool(service *Service) FindSpacesByMembersTool {
	return FindSpacesByMembersTool{service: service}
}

func (FindSpacesByMembersTool) Name() string {
	return ToolNameFindSpacesByMembers
}

func (FindSpacesByMembersTool) Description() string {
	return "Find Google Chat spaces by member user resource names. Use this after people.searchDirectory when the user says messages with a person, for example Bao, then pass Candidate Chat users like users/123 before calling chat.listMessages with the returned spaces/... name."
}

func (FindSpacesByMembersTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"memberUserNames": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"maxResults": map[string]any{"type": "number", "minimum": 1, "maximum": maxAllowedResults},
			"pageToken":  map[string]any{"type": "string"},
			"spaceType":  map[string]any{"type": "string", "enum": []string{"SPACE", "GROUP_CHAT", "DIRECT_MESSAGE"}},
		},
		"required":             []string{"memberUserNames"},
		"additionalProperties": false,
	}
}

func (FindSpacesByMembersTool) Capability() tools.Capability {
	return tools.CapabilityReadOnly
}

func (FindSpacesByMembersTool) RiskLevel() tools.RiskLevel {
	return tools.RiskLevelSafeRead
}

func (t FindSpacesByMembersTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.FindSpacesByMembers(ctx, FindSpacesByMembersInput{
		MemberUserNames: stringSliceArg(call.Arguments, "memberUserNames"),
		MaxResults:      boundedInt64Arg(call.Arguments, "maxResults", maxAllowedResults, maxAllowedResults),
		PageToken:       stringArg(call.Arguments, "pageToken"),
		SpaceType:       stringArg(call.Arguments, "spaceType"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}

	lines := []string{}
	for _, match := range output.Spaces {
		lines = append(lines, fmt.Sprintf("- %s | %s | %s | members: %s | %s",
			match.Space.Name,
			emptyValue(match.Space.DisplayName, "(no display name)"),
			emptyValue(match.Space.SpaceType, emptyValue(match.Space.Type, "(no type)")),
			memberNamesForOutput(match.Members),
			match.Space.SpaceURI,
		))
	}
	if len(lines) == 0 {
		lines = append(lines, "No Google Chat spaces matched the requested members.")
	}
	if strings.TrimSpace(output.NextPageToken) != "" {
		lines = append(lines, "Next page token: "+output.NextPageToken)
	}
	content := strings.Join(lines, "\n")
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true, ContentForLLM: content, ContentForUser: content}
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
	return "List Google Chat messages from a space. The space input must be a spaces/... resource name; if the user names a person or group instead, call people.searchDirectory and chat.findSpacesByMembers first."
}

func (ListMessagesTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"space":       map[string]any{"type": "string"},
			"maxResults":  map[string]any{"type": "number", "minimum": 1, "maximum": maxAllowedResults},
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
		MaxResults:  boundedInt64Arg(call.Arguments, "maxResults", defaultMaxResults, maxAllowedResults),
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

func RegisterTools(registry *tools.ToolRegistry, service *Service) error {
	for _, tool := range []tools.Tool{
		NewListSpacesTool(service),
		NewListMembersTool(service),
		NewFindSpacesByMembersTool(service),
		NewListMessagesTool(service),
		NewSendMessageTool(service),
	} {
		if err := registry.RegisterWithEntry(tool, tools.ToolRegistryEntry{Owner: "integration"}); err != nil {
			return err
		}
	}
	return nil
}

func emptyValue(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func normalizeSpaceName(value string) string {
	value = strings.TrimSpace(value)
	start := strings.Index(value, "spaces/")
	if start < 0 {
		return ""
	}
	value = value[start:]
	end := len(value)
	for index, r := range value {
		if index == 0 {
			continue
		}
		if r == '|' || r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			end = index
			break
		}
	}
	return strings.TrimSpace(value[:end])
}

func normalizeMemberUserNames(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !strings.HasPrefix(value, "users/") {
			value = "users/" + value
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func membershipsContainAll(members []chatconnector.Membership, required map[string]struct{}) bool {
	if len(required) == 0 {
		return false
	}
	found := map[string]struct{}{}
	for _, member := range members {
		value := strings.ToLower(strings.TrimSpace(member.MemberName))
		if value == "" {
			continue
		}
		if _, ok := required[value]; ok {
			found[value] = struct{}{}
		}
	}
	return len(found) == len(required)
}

func memberNamesForOutput(members []chatconnector.Membership) string {
	names := make([]string, 0, len(members))
	for _, member := range members {
		name := strings.TrimSpace(member.MemberName)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return "(no members)"
	}
	return strings.Join(names, ",")
}

func MapError(err error) *ErrorShape {
	if err == nil {
		return nil
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

func googleAPIErrorMessage(err *googleapi.Error) string {
	if err == nil {
		return "Google API error"
	}
	if strings.TrimSpace(err.Message) != "" {
		return err.Message
	}
	if strings.TrimSpace(err.Body) != "" {
		return err.Body
	}
	if strings.TrimSpace(err.Error()) != "" {
		return err.Error()
	}
	return fmt.Sprintf("Google API error status %d", err.Code)
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

func stringSliceArg(args map[string]any, name string) []string {
	if args == nil {
		return nil
	}
	switch value := args[name].(type) {
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
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

func boundedInt64Arg(args map[string]any, name string, fallback int64, max int64) int64 {
	value := int64Arg(args, name)
	if value < 1 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}
