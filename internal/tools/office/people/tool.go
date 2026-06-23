package people

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	googleconnector "vclaw/internal/connectors/google"
	peopleconnector "vclaw/internal/connectors/google/people"
	"vclaw/internal/tools"
	"vclaw/internal/tools/office"

	"google.golang.org/api/googleapi"
)

const ToolNameSearchDirectory = "people.searchDirectory"

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
		Name:             ToolNameSearchDirectory,
		Owner:            "integration",
		Description:      "Search Google Workspace directory people by name or email.",
		DefaultRiskLevel: "safe_read",
		RequiresApproval: false,
	},
}

type ErrorShape struct {
	Code      string
	Message   string
	Retryable bool
}

type Connector interface {
	SearchDirectoryPeople(ctx context.Context, query string, pageSize int64, pageToken string) (peopleconnector.SearchDirectoryOutput, error)
}

type Service struct {
	connector Connector
}

func NewService(connector Connector) *Service {
	return &Service{connector: connector}
}

type SearchDirectoryInput struct {
	Query      string
	MaxResults int64
	PageToken  string
}

type SearchDirectoryOutput struct {
	People        []peopleconnector.DirectoryPerson
	NextPageToken string
	TotalSize     int64
}

func (s *Service) SearchDirectory(ctx context.Context, input SearchDirectoryInput) (SearchDirectoryOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return SearchDirectoryOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "people connector is not configured"}
	}
	if strings.TrimSpace(input.Query) == "" {
		return SearchDirectoryOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "query is required"}
	}
	maxResults, errShape := normalizeMaxResults(input.MaxResults)
	if errShape != nil {
		return SearchDirectoryOutput{}, errShape
	}

	output, err := s.connector.SearchDirectoryPeople(ctx, input.Query, maxResults, input.PageToken)
	if err != nil {
		return SearchDirectoryOutput{}, MapError(err)
	}
	return SearchDirectoryOutput{
		People:        output.People,
		NextPageToken: output.NextPageToken,
		TotalSize:     output.TotalSize,
	}, nil
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

type SearchDirectoryTool struct {
	service *Service
}

func NewSearchDirectoryTool(service *Service) SearchDirectoryTool {
	return SearchDirectoryTool{service: service}
}

func (SearchDirectoryTool) Name() string {
	return ToolNameSearchDirectory
}

func (SearchDirectoryTool) Description() string {
	return "Search Google Workspace directory for individual people by name or email. Use this to resolve a specific person's name to their users/... resource name or email address. Do NOT use this to look up a Chat group or space by its display name — to find members of a named Chat space, call chat.listSpaces first to get the space resource name, then chat.listMembers."
}

func (SearchDirectoryTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"query":      map[string]any{"type": "string"},
			"maxResults": map[string]any{"type": "number", "minimum": 1, "maximum": maxAllowedResults},
			"pageToken":  map[string]any{"type": "string"},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
}

func (SearchDirectoryTool) Capability() tools.Capability {
	return tools.CapabilityReadOnly
}

func (SearchDirectoryTool) RiskLevel() tools.RiskLevel {
	return tools.RiskLevelSafeRead
}

func (t SearchDirectoryTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.SearchDirectory(ctx, SearchDirectoryInput{
		Query:      stringArg(call.Arguments, "query"),
		MaxResults: boundedInt64Arg(call.Arguments, "maxResults", defaultMaxResults, maxAllowedResults),
		PageToken:  stringArg(call.Arguments, "pageToken"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}

	lines := []string{}
	for _, person := range output.People {
		lines = append(lines, formatPerson(person))
	}
	if len(lines) == 0 {
		lines = append(lines, "No directory people found.")
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

func RegisterTools(registry *tools.ToolRegistry, service *Service) error {
	return registry.RegisterWithEntry(NewSearchDirectoryTool(service), tools.ToolRegistryEntry{Owner: "integration", Group: "google_workspace"})
}

func formatPerson(person peopleconnector.DirectoryPerson) string {
	return fmt.Sprintf("- %s | %s | emails: %s | candidates: %s | sources: %s",
		emptyValue(person.ResourceName, "(no resource name)"),
		emptyValue(person.DisplayName, "(no display name)"),
		emptyList(person.EmailAddresses),
		emptyList(person.CandidateUserNames),
		emptyList(person.SourceTypes),
	)
}

func emptyValue(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func emptyList(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	return strings.Join(values, ",")
}

func MapError(err error) *ErrorShape {
	if err == nil {
		return nil
	}
	if googleconnector.IsNetworkError(err) {
		return &ErrorShape{Code: "PROVIDER_TIMEOUT", Message: "network error contacting People API: " + err.Error(), Retryable: true}
	}
	var gerr *googleapi.Error
	if !errors.As(err, &gerr) {
		return &ErrorShape{Code: "INTERNAL_ERROR", Message: err.Error()}
	}
	message := googleAPIErrorMessage(gerr)

	switch {
	case gerr.Code == http.StatusUnauthorized:
		return &ErrorShape{Code: office.ErrorAuthExpired, Message: office.FriendlyGoogleToolError(office.ErrorAuthExpired, "Google People", message), Retryable: true}
	case gerr.Code == http.StatusForbidden && hasMissingScopeReason(gerr):
		return &ErrorShape{Code: office.ErrorAuthMissingScope, Message: office.FriendlyGoogleToolError(office.ErrorAuthMissingScope, "Google People", message)}
	case gerr.Code == http.StatusForbidden:
		return &ErrorShape{Code: office.ErrorActionBlockedByPolicy, Message: office.FriendlyGoogleToolError(office.ErrorActionBlockedByPolicy, "Google People", message)}
	case gerr.Code == http.StatusNotFound:
		return &ErrorShape{Code: office.ErrorResourceNotFound, Message: office.FriendlyGoogleToolError(office.ErrorResourceNotFound, "Google People", message)}
	case gerr.Code == http.StatusTooManyRequests:
		return &ErrorShape{Code: office.ErrorRateLimited, Message: office.FriendlyGoogleToolError(office.ErrorRateLimited, "Google People", message), Retryable: true}
	case gerr.Code >= 500:
		return &ErrorShape{Code: office.ErrorProviderUnavailable, Message: office.FriendlyGoogleToolError(office.ErrorProviderUnavailable, "Google People", message), Retryable: true}
	default:
		return &ErrorShape{Code: "INTERNAL_ERROR", Message: message}
	}
}

func hasMissingScopeReason(err *googleapi.Error) bool {
	text := strings.ToLower(err.Message + " " + err.Body)
	for _, item := range err.Errors {
		text += " " + strings.ToLower(item.Reason+" "+item.Message)
	}
	return strings.Contains(text, "insufficient authentication scopes") ||
		strings.Contains(text, "insufficient permissions")
}

func googleAPIErrorMessage(err *googleapi.Error) string {
	if err == nil {
		return "Google People API error"
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
	return fmt.Sprintf("Google People API error status %d", err.Code)
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
