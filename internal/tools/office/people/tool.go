package people

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	peopleconnector "vclaw/internal/connectors/google/people"
	"vclaw/internal/tools"

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
	return "Search Google Workspace directory people by name or email. Use this to resolve user names before matching Google Chat members and listing messages from a named group chat."
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
