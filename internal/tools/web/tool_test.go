package web

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vclaw/internal/connectors/tavily"
	"vclaw/internal/tools"
)

type fakeConnector struct {
	searchInput  tavily.SearchInput
	extractInput tavily.ExtractInput
}

func (f *fakeConnector) Search(_ context.Context, input tavily.SearchInput) (tavily.SearchOutput, error) {
	f.searchInput = input
	return tavily.SearchOutput{
		Answer: "answer",
		Results: []tavily.SearchResult{{
			Title:   "Result",
			URL:     "https://example.com",
			Content: "snippet",
		}},
	}, nil
}

func (f *fakeConnector) Extract(_ context.Context, input tavily.ExtractInput) (tavily.ExtractOutput, error) {
	f.extractInput = input
	return tavily.ExtractOutput{
		Results: []tavily.ExtractResult{{
			URL:     input.URLs[0],
			Content: strings.Repeat("content ", 1000),
		}},
	}, nil
}

func TestSearchToolMetadata(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry, NewService(&fakeConnector{})); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}
	definition, ok := registry.GetDefinition(ToolNameSearch)
	if !ok {
		t.Fatalf("missing %s", ToolNameSearch)
	}
	if definition.Capability != tools.CapabilityReadOnly || definition.RiskLevel != tools.RiskLevelSafeRead {
		t.Fatalf("metadata mismatch: %#v", definition)
	}
	if definition.RequiresApproval {
		t.Fatalf("web.search should not require approval")
	}
}

func TestRegistryEntriesMatchContractMetadata(t *testing.T) {
	if len(RegistryEntries) != 2 {
		t.Fatalf("expected 2 registry entries, got %d", len(RegistryEntries))
	}
	for _, entry := range RegistryEntries {
		if entry.Owner != "integration" {
			t.Fatalf("%s Owner = %q, want integration", entry.Name, entry.Owner)
		}
		if entry.DefaultRiskLevel != "safe_read" {
			t.Fatalf("%s DefaultRiskLevel = %q, want safe_read", entry.Name, entry.DefaultRiskLevel)
		}
		if entry.RequiresApproval {
			t.Fatalf("%s should not require approval", entry.Name)
		}
	}
}

func TestSearchToolExecuteValidatesAndClampsInput(t *testing.T) {
	connector := &fakeConnector{}
	tool := NewSearchTool(NewService(connector))

	missing := tool.Execute(context.Background(), tools.ToolCall{Name: ToolNameSearch, Arguments: map[string]any{}})
	if missing.Success || missing.Error == nil || missing.Error.Code != "INVALID_INPUT" {
		t.Fatalf("expected invalid query error, got %#v", missing)
	}

	invalidDepth := tool.Execute(context.Background(), tools.ToolCall{
		Name: ToolNameSearch,
		Arguments: map[string]any{
			"query":       "test",
			"searchDepth": "deep",
		},
	})
	if invalidDepth.Success || invalidDepth.Error == nil || invalidDepth.Error.Code != "INVALID_INPUT" {
		t.Fatalf("expected invalid searchDepth error, got %#v", invalidDepth)
	}

	result := tool.Execute(context.Background(), tools.ToolCall{
		Name: ToolNameSearch,
		Arguments: map[string]any{
			"query":          "test",
			"maxResults":     float64(99),
			"searchDepth":    "advanced",
			"topic":          "news",
			"includeDomains": []any{"example.com", " "},
		},
	})
	if !result.Success {
		t.Fatalf("expected success, got %#v", result)
	}
	if connector.searchInput.MaxResults != maxSearchResults {
		t.Fatalf("MaxResults = %d, want %d", connector.searchInput.MaxResults, maxSearchResults)
	}
	if connector.searchInput.SearchDepth != "advanced" || connector.searchInput.Topic != "news" {
		t.Fatalf("unexpected normalized search input: %#v", connector.searchInput)
	}
	if len(connector.searchInput.IncludeDomains) != 1 || connector.searchInput.IncludeDomains[0] != "example.com" {
		t.Fatalf("unexpected include domains: %#v", connector.searchInput.IncludeDomains)
	}
	if !strings.Contains(result.ContentForLLM, "URL: https://example.com") {
		t.Fatalf("expected citation-ready URL in content: %s", result.ContentForLLM)
	}
}

func TestSearchToolReportsMissingWebConnectorClearly(t *testing.T) {
	tool := NewSearchTool(NewService(nil))

	result := tool.Execute(context.Background(), tools.ToolCall{
		Name:      ToolNameSearch,
		Arguments: map[string]any{"query": "latest release"},
	})
	if result.Success {
		t.Fatal("expected missing connector to fail")
	}
	if result.Error == nil || result.Error.Code != "AUTH_MISSING_SCOPE" {
		t.Fatalf("expected AUTH_MISSING_SCOPE, got %#v", result.Error)
	}
	if !strings.Contains(result.ContentForUser, "TAVILY_API_KEY") {
		t.Fatalf("expected actionable setup message, got %q", result.ContentForUser)
	}
}

func TestMapErrorReportsTavilyAPIKeySetup(t *testing.T) {
	errShape := mapError(errors.New("tavily api key is required"))
	if errShape == nil || errShape.Code != "AUTH_MISSING_SCOPE" {
		t.Fatalf("expected AUTH_MISSING_SCOPE, got %#v", errShape)
	}
}

func TestFetchToolExecuteValidatesURLAndClampsTimeout(t *testing.T) {
	connector := &fakeConnector{}
	tool := NewFetchTool(NewService(connector))

	badURL := tool.Execute(context.Background(), tools.ToolCall{
		Name:      ToolNameFetch,
		Arguments: map[string]any{"url": "file:///secret.txt"},
	})
	if badURL.Success || badURL.Error == nil || badURL.Error.Code != "INVALID_INPUT" {
		t.Fatalf("expected invalid URL error, got %#v", badURL)
	}

	badFormat := tool.Execute(context.Background(), tools.ToolCall{
		Name:      ToolNameFetch,
		Arguments: map[string]any{"url": "https://example.com", "format": "html"},
	})
	if badFormat.Success || badFormat.Error == nil || badFormat.Error.Code != "INVALID_INPUT" {
		t.Fatalf("expected invalid format error, got %#v", badFormat)
	}

	result := tool.Execute(context.Background(), tools.ToolCall{
		Name: ToolNameFetch,
		Arguments: map[string]any{
			"url":            "https://example.com/page",
			"format":         "text",
			"extractDepth":   "advanced",
			"timeoutSeconds": float64(99),
		},
	})
	if !result.Success {
		t.Fatalf("expected success, got %#v", result)
	}
	if connector.extractInput.Timeout != maxFetchTimeout {
		t.Fatalf("Timeout = %d, want %d", connector.extractInput.Timeout, maxFetchTimeout)
	}
	if connector.extractInput.Format != "text" || connector.extractInput.ExtractDepth != "advanced" {
		t.Fatalf("unexpected extract input: %#v", connector.extractInput)
	}
	if !strings.Contains(result.ContentForLLM, "[truncated]") {
		t.Fatalf("expected truncated fetch output")
	}
}
