package tavily

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientSearchSerializesRequestAndParsesResponse(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody searchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"query":"golang tavily",
			"answer":"Use Tavily for web search.",
			"results":[{"title":"Tavily","url":"https://example.com","content":"snippet","raw_content":"raw","score":0.9}],
			"response_time":0.42
		}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{APIKey: "tvly-test", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	out, err := client.Search(context.Background(), SearchInput{
		Query:          "golang tavily",
		SearchDepth:    "advanced",
		Topic:          "news",
		MaxResults:     3,
		IncludeDomains: []string{"example.com"},
		ExcludeDomains: []string{"blocked.com"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if gotPath != "/search" {
		t.Fatalf("path = %q, want /search", gotPath)
	}
	if gotAuth != "Bearer tvly-test" {
		t.Fatalf("authorization header mismatch: %q", gotAuth)
	}
	if gotBody.Query != "golang tavily" || gotBody.SearchDepth != "advanced" || gotBody.Topic != "news" || gotBody.MaxResults != 3 {
		t.Fatalf("unexpected request body: %#v", gotBody)
	}
	if !gotBody.IncludeAnswer {
		t.Fatalf("expected include_answer=true")
	}
	if len(out.Results) != 1 || out.Results[0].URL != "https://example.com" || out.Answer == "" {
		t.Fatalf("unexpected search output: %#v", out)
	}
}

func TestClientExtractSerializesRequestAndParsesFailedResults(t *testing.T) {
	var gotBody extractRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract" {
			t.Fatalf("path = %q, want /extract", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results":[{"url":"https://example.com","raw_content":"markdown body"}],
			"failed_results":[{"url":"https://bad.example","error":"not found"}],
			"response_time":0.8
		}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{APIKey: "tvly-test", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	out, err := client.Extract(context.Background(), ExtractInput{
		URLs:         []string{"https://example.com"},
		ExtractDepth: "advanced",
		Format:       "markdown",
		Timeout:      12,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(gotBody.URLs) != 1 || gotBody.ExtractDepth != "advanced" || gotBody.Format != "markdown" || gotBody.Timeout != 12 {
		t.Fatalf("unexpected request body: %#v", gotBody)
	}
	if len(out.Results) != 1 || out.Results[0].Content != "markdown body" {
		t.Fatalf("unexpected extract results: %#v", out.Results)
	}
	if len(out.Failed) != 1 || out.Failed[0].Error != "not found" {
		t.Fatalf("unexpected failed results: %#v", out.Failed)
	}
}

func TestClientMapsHTTPErrorCodes(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		wantCode  string
		retryable bool
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, wantCode: "AUTH_EXPIRED"},
		{name: "forbidden", status: http.StatusForbidden, wantCode: "AUTH_EXPIRED"},
		{name: "rate limited", status: http.StatusTooManyRequests, wantCode: "RATE_LIMITED", retryable: true},
		{name: "server error", status: http.StatusBadGateway, wantCode: "PROVIDER_UNAVAILABLE", retryable: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"detail":"api error"}`))
			}))
			defer server.Close()

			client, err := NewClient(Config{APIKey: "tvly-test", BaseURL: server.URL})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			_, err = client.Search(context.Background(), SearchInput{Query: "test"})
			var tavilyErr Error
			if !errors.As(err, &tavilyErr) {
				t.Fatalf("expected tavily Error, got %T %v", err, err)
			}
			if tavilyErr.Code != tc.wantCode || tavilyErr.Retryable != tc.retryable {
				t.Fatalf("mapped error = %#v, want code=%s retryable=%v", tavilyErr, tc.wantCode, tc.retryable)
			}
		})
	}
}
