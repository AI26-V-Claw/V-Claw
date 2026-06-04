package tavily

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://api.tavily.com"
	defaultTimeout = 30 * time.Second
)

type Config struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewClient(config Config) (*Client, error) {
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("tavily api key is required")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("invalid tavily base url: %w", err)
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{apiKey: apiKey, baseURL: baseURL, httpClient: httpClient}, nil
}

type SearchInput struct {
	Query          string
	SearchDepth    string
	Topic          string
	MaxResults     int
	IncludeDomains []string
	ExcludeDomains []string
}

type SearchOutput struct {
	Query        string
	Answer       string
	Results      []SearchResult
	ResponseTime float64
}

type SearchResult struct {
	Title      string
	URL        string
	Content    string
	RawContent string
	Score      float64
}

type ExtractInput struct {
	URLs         []string
	ExtractDepth string
	Format       string
	Timeout      int
}

type ExtractOutput struct {
	Results      []ExtractResult
	Failed       []ExtractFailedResult
	ResponseTime float64
}

type ExtractResult struct {
	URL        string
	Content    string
	RawContent string
}

type ExtractFailedResult struct {
	URL   string
	Error string
}

func (c *Client) Search(ctx context.Context, input SearchInput) (SearchOutput, error) {
	if c == nil {
		return SearchOutput{}, fmt.Errorf("tavily client is nil")
	}
	request := searchRequest{
		Query:          input.Query,
		SearchDepth:    input.SearchDepth,
		Topic:          input.Topic,
		MaxResults:     input.MaxResults,
		IncludeAnswer:  true,
		IncludeDomains: input.IncludeDomains,
		ExcludeDomains: input.ExcludeDomains,
	}
	var response searchResponse
	if err := c.post(ctx, "/search", request, &response); err != nil {
		return SearchOutput{}, err
	}
	out := SearchOutput{
		Query:        response.Query,
		Answer:       response.Answer,
		ResponseTime: response.ResponseTime,
	}
	for _, result := range response.Results {
		out.Results = append(out.Results, SearchResult{
			Title:      result.Title,
			URL:        result.URL,
			Content:    result.Content,
			RawContent: result.RawContent,
			Score:      result.Score,
		})
	}
	return out, nil
}

func (c *Client) Extract(ctx context.Context, input ExtractInput) (ExtractOutput, error) {
	if c == nil {
		return ExtractOutput{}, fmt.Errorf("tavily client is nil")
	}
	request := extractRequest{
		URLs:         input.URLs,
		ExtractDepth: input.ExtractDepth,
		Format:       input.Format,
		Timeout:      input.Timeout,
	}
	var response extractResponse
	if err := c.post(ctx, "/extract", request, &response); err != nil {
		return ExtractOutput{}, err
	}
	out := ExtractOutput{ResponseTime: response.ResponseTime}
	for _, result := range response.Results {
		out.Results = append(out.Results, ExtractResult{
			URL:        result.URL,
			Content:    firstNonEmpty(result.RawContent, result.Content),
			RawContent: result.RawContent,
		})
	}
	for _, failed := range response.FailedResults {
		out.Failed = append(out.Failed, ExtractFailedResult{URL: failed.URL, Error: failed.Error})
	}
	return out, nil
}

func (c *Client) post(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal tavily request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Error{Code: "PROVIDER_TIMEOUT", Message: err.Error(), Retryable: true}
		}
		return Error{Code: "PROVIDER_UNAVAILABLE", Message: err.Error(), Retryable: true}
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var errorResponse apiErrorResponse
		_ = json.NewDecoder(response.Body).Decode(&errorResponse)
		return mapHTTPError(response.StatusCode, response.Status, errorResponse.Detail)
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decode tavily response: %w", err)
	}
	return nil
}

type Error struct {
	Code      string
	Message   string
	Retryable bool
}

func (e Error) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return e.Code
	}
	return e.Message
}

func mapHTTPError(statusCode int, status string, detail any) Error {
	message := strings.TrimSpace(detailMessage(detail))
	if message == "" {
		message = status
	}
	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return Error{Code: "AUTH_EXPIRED", Message: message}
	case statusCode == http.StatusTooManyRequests:
		return Error{Code: "RATE_LIMITED", Message: message, Retryable: true}
	case statusCode >= 500:
		return Error{Code: "PROVIDER_UNAVAILABLE", Message: message, Retryable: true}
	default:
		return Error{Code: "INTERNAL_ERROR", Message: message}
	}
}

func detailMessage(detail any) string {
	switch value := detail.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			parts = append(parts, detailMessage(item))
		}
		return strings.Join(parts, "; ")
	case map[string]any:
		if msg, ok := value["msg"].(string); ok {
			return msg
		}
		if msg, ok := value["message"].(string); ok {
			return msg
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type searchRequest struct {
	Query          string   `json:"query"`
	SearchDepth    string   `json:"search_depth,omitempty"`
	Topic          string   `json:"topic,omitempty"`
	MaxResults     int      `json:"max_results,omitempty"`
	IncludeAnswer  bool     `json:"include_answer"`
	IncludeDomains []string `json:"include_domains,omitempty"`
	ExcludeDomains []string `json:"exclude_domains,omitempty"`
}

type searchResponse struct {
	Query   string `json:"query"`
	Answer  string `json:"answer"`
	Results []struct {
		Title      string  `json:"title"`
		URL        string  `json:"url"`
		Content    string  `json:"content"`
		RawContent string  `json:"raw_content"`
		Score      float64 `json:"score"`
	} `json:"results"`
	ResponseTime float64 `json:"response_time"`
}

type extractRequest struct {
	URLs         []string `json:"urls"`
	ExtractDepth string   `json:"extract_depth,omitempty"`
	Format       string   `json:"format,omitempty"`
	Timeout      int      `json:"timeout,omitempty"`
}

type extractResponse struct {
	Results []struct {
		URL        string `json:"url"`
		Content    string `json:"content"`
		RawContent string `json:"raw_content"`
	} `json:"results"`
	FailedResults []struct {
		URL   string `json:"url"`
		Error string `json:"error"`
	} `json:"failed_results"`
	ResponseTime float64 `json:"response_time"`
}

type apiErrorResponse struct {
	Detail any `json:"detail"`
}
