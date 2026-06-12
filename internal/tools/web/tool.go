package web

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"vclaw/internal/connectors/tavily"
	"vclaw/internal/tools"
)

const (
	ToolNameSearch = "web.search"
	ToolNameFetch  = "web.fetch"

	defaultSearchMaxResults = 5
	maxSearchResults        = 10
	defaultFetchTimeout     = 10
	maxFetchTimeout         = 60
	maxContentChars         = 6000
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
		Name:             ToolNameSearch,
		Owner:            "integration",
		Description:      "Search the public web for current information.",
		DefaultRiskLevel: "safe_read",
		RequiresApproval: false,
	},
	{
		Name:             ToolNameFetch,
		Owner:            "integration",
		Description:      "Fetch and extract readable content from a public web page URL.",
		DefaultRiskLevel: "safe_read",
		RequiresApproval: false,
	},
}

type Connector interface {
	Search(ctx context.Context, input tavily.SearchInput) (tavily.SearchOutput, error)
	Extract(ctx context.Context, input tavily.ExtractInput) (tavily.ExtractOutput, error)
}

type Service struct {
	connector Connector
}

func NewService(connector Connector) *Service {
	return &Service{connector: connector}
}

type SearchInput struct {
	Query          string
	MaxResults     int
	SearchDepth    string
	Topic          string
	IncludeDomains []string
	ExcludeDomains []string
}

type FetchInput struct {
	URL            string
	Format         string
	ExtractDepth   string
	TimeoutSeconds int
}

type ErrorShape struct {
	Code      string
	Message   string
	Retryable bool
}

func (s *Service) Search(ctx context.Context, input SearchInput) (tavily.SearchOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return tavily.SearchOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "web connector is not configured"}
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		return tavily.SearchOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "query is required"}
	}
	searchDepth, errShape := normalizeEnum(input.SearchDepth, "searchDepth", "basic", "basic", "advanced")
	if errShape != nil {
		return tavily.SearchOutput{}, errShape
	}
	topic, errShape := normalizeEnum(input.Topic, "topic", "general", "general", "news")
	if errShape != nil {
		return tavily.SearchOutput{}, errShape
	}
	output, err := s.connector.Search(ctx, tavily.SearchInput{
		Query:          input.Query,
		SearchDepth:    searchDepth,
		Topic:          topic,
		MaxResults:     boundedInt(input.MaxResults, defaultSearchMaxResults, maxSearchResults),
		IncludeDomains: cleanStrings(input.IncludeDomains),
		ExcludeDomains: cleanStrings(input.ExcludeDomains),
	})
	if err != nil {
		return tavily.SearchOutput{}, mapError(err)
	}
	return output, nil
}

func (s *Service) Fetch(ctx context.Context, input FetchInput) (tavily.ExtractOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return tavily.ExtractOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "web connector is not configured"}
	}
	normalizedURL, errShape := normalizeURL(input.URL)
	if errShape != nil {
		return tavily.ExtractOutput{}, errShape
	}
	format, errShape := normalizeEnum(input.Format, "format", "markdown", "markdown", "text")
	if errShape != nil {
		return tavily.ExtractOutput{}, errShape
	}
	extractDepth, errShape := normalizeEnum(input.ExtractDepth, "extractDepth", "basic", "basic", "advanced")
	if errShape != nil {
		return tavily.ExtractOutput{}, errShape
	}
	output, err := s.connector.Extract(ctx, tavily.ExtractInput{
		URLs:         []string{normalizedURL},
		ExtractDepth: extractDepth,
		Format:       format,
		Timeout:      boundedInt(input.TimeoutSeconds, defaultFetchTimeout, maxFetchTimeout),
	})
	if err != nil {
		return tavily.ExtractOutput{}, mapError(err)
	}
	return output, nil
}

type SearchTool struct {
	service *Service
}

func NewSearchTool(service *Service) SearchTool {
	return SearchTool{service: service}
}

func (SearchTool) Name() string { return ToolNameSearch }

func (SearchTool) Description() string {
	return "Search the public web for current information. Returns concise results with titles, URLs, snippets, and optional answer text."
}

func (SearchTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"query":          map[string]any{"type": "string"},
			"maxResults":     map[string]any{"type": "number", "minimum": 1, "maximum": maxSearchResults},
			"searchDepth":    map[string]any{"type": "string", "enum": []string{"basic", "advanced"}},
			"topic":          map[string]any{"type": "string", "enum": []string{"general", "news"}},
			"includeDomains": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"excludeDomains": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
}

func (SearchTool) Capability() tools.Capability { return tools.CapabilityReadOnly }

func (SearchTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelSafeRead }

func (t SearchTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.Search(ctx, SearchInput{
		Query:          stringArg(call.Arguments, "query"),
		MaxResults:     intArg(call.Arguments, "maxResults"),
		SearchDepth:    stringArg(call.Arguments, "searchDepth"),
		Topic:          stringArg(call.Arguments, "topic"),
		IncludeDomains: stringSliceArg(call.Arguments, "includeDomains"),
		ExcludeDomains: stringSliceArg(call.Arguments, "excludeDomains"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}
	content := formatSearchOutput(output)
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
		Metadata: map[string]any{
			"query":        stringArg(call.Arguments, "query"),
			"result_count": len(output.Results),
		},
	}
}

type FetchTool struct {
	service *Service
}

func NewFetchTool(service *Service) FetchTool {
	return FetchTool{service: service}
}

func (FetchTool) Name() string { return ToolNameFetch }

func (FetchTool) Description() string {
	return "Fetch and extract readable content from a public web page URL. Returns truncated content suitable for citations and summarization."
}

func (FetchTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"url":            map[string]any{"type": "string"},
			"format":         map[string]any{"type": "string", "enum": []string{"markdown", "text"}},
			"extractDepth":   map[string]any{"type": "string", "enum": []string{"basic", "advanced"}},
			"timeoutSeconds": map[string]any{"type": "number", "minimum": 1, "maximum": maxFetchTimeout},
		},
		"required":             []string{"url"},
		"additionalProperties": false,
	}
}

func (FetchTool) Capability() tools.Capability { return tools.CapabilityReadOnly }

func (FetchTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelSafeRead }

func (t FetchTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.Fetch(ctx, FetchInput{
		URL:            stringArg(call.Arguments, "url"),
		Format:         stringArg(call.Arguments, "format"),
		ExtractDepth:   stringArg(call.Arguments, "extractDepth"),
		TimeoutSeconds: intArg(call.Arguments, "timeoutSeconds"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}
	rawURL := stringArg(call.Arguments, "url")
	content, wasTruncated := formatFetchOutputWithTruncation(output)
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
		Truncated:      wasTruncated,
		ArtifactRef:    &tools.ToolArtifactRef{Kind: "url", URI: rawURL, Label: rawURL},
		Metadata:       map[string]any{"url": rawURL},
	}
}

func RegisterTools(registry *tools.ToolRegistry, service *Service) error {
	if err := registry.RegisterWithEntry(NewSearchTool(service), tools.ToolRegistryEntry{Owner: "integration", Group: "web"}); err != nil {
		return err
	}
	if err := registry.RegisterWithEntry(NewFetchTool(service), tools.ToolRegistryEntry{Owner: "integration", Group: "web"}); err != nil {
		return err
	}
	return nil
}

func normalizeEnum(value, name, fallback string, allowed ...string) (string, *ErrorShape) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback, nil
	}
	for _, allowedValue := range allowed {
		if value == allowedValue {
			return value, nil
		}
	}
	return "", &ErrorShape{
		Code:    "INVALID_INPUT",
		Message: fmt.Sprintf("%s must be one of: %s", name, strings.Join(allowed, ", ")),
	}
}

func normalizeURL(value string) (string, *ErrorShape) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", &ErrorShape{Code: "INVALID_INPUT", Message: "url is required"}
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", &ErrorShape{Code: "INVALID_INPUT", Message: "url must be a valid absolute URL"}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", &ErrorShape{Code: "INVALID_INPUT", Message: "url scheme must be http or https"}
	}
	return parsed.String(), nil
}

func boundedInt(value, fallback, max int) int {
	if value < 1 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func formatSearchOutput(output tavily.SearchOutput) string {
	lines := []string{}
	if strings.TrimSpace(output.Answer) != "" {
		lines = append(lines, "Answer: "+output.Answer)
	}
	if len(output.Results) == 0 {
		lines = append(lines, "No web results found.")
	}
	for i, result := range output.Results {
		snippet := firstNonEmpty(result.Content, result.RawContent)
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, emptyValue(result.Title, "(no title)")))
		lines = append(lines, "URL: "+result.URL)
		if strings.TrimSpace(snippet) != "" {
			lines = append(lines, "Snippet: "+truncate(snippet, 700))
		}
	}
	return strings.Join(lines, "\n")
}

func formatFetchOutput(output tavily.ExtractOutput) string {
	content, _ := formatFetchOutputWithTruncation(output)
	return content
}

func formatFetchOutputWithTruncation(output tavily.ExtractOutput) (string, bool) {
	lines := []string{}
	truncated := false
	if len(output.Results) == 0 {
		lines = append(lines, "No page content extracted.")
	}
	for _, result := range output.Results {
		lines = append(lines, "URL: "+result.URL)
		lines = append(lines, "Content:")
		body := firstNonEmpty(result.Content, result.RawContent)
		if len(strings.TrimSpace(body)) > maxContentChars {
			truncated = true
		}
		lines = append(lines, truncate(body, maxContentChars))
	}
	for _, failed := range output.Failed {
		lines = append(lines, fmt.Sprintf("Failed: %s - %s", failed.URL, failed.Error))
	}
	return strings.Join(lines, "\n"), truncated
}

func truncate(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	if max <= 20 {
		return value[:max]
	}
	return strings.TrimSpace(value[:max-15]) + "\n[truncated]"
}

func mapError(err error) *ErrorShape {
	if err == nil {
		return nil
	}
	var tavilyErr tavily.Error
	if errors.As(err, &tavilyErr) {
		return &ErrorShape{Code: tavilyErr.Code, Message: tavilyErr.Message, Retryable: tavilyErr.Retryable}
	}
	return &ErrorShape{Code: "INTERNAL_ERROR", Message: err.Error()}
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
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func emptyValue(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
