package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOpenAIToolNameMapPreservesContractNames(t *testing.T) {
	nameMap := newOpenAIToolNameMap([]ToolDefinition{
		{Name: "gmail.listEmails"},
		{Name: "calendar.createEvent"},
	})

	if got := nameMap.safeName("gmail.listEmails"); got != "gmail__dot__listEmails" {
		t.Fatalf("unexpected safe name: %q", got)
	}
	if got := nameMap.contractName("gmail__dot__listEmails"); got != "gmail.listEmails" {
		t.Fatalf("unexpected contract name: %q", got)
	}
	if got := nameMap.contractName("calendar__dot__createEvent"); got != "calendar.createEvent" {
		t.Fatalf("unexpected calendar contract name: %q", got)
	}
}

func TestOpenAIMessageMappingUsesSafeNamesAndRestoresContractNames(t *testing.T) {
	nameMap := newOpenAIToolNameMap([]ToolDefinition{{Name: "gmail.listEmails"}})

	wire := openAIMessageFromProvider(Message{
		Role: MessageRoleAssistant,
		ToolCalls: []ToolCall{{
			ID:        "call_1",
			Name:      "gmail.listEmails",
			Arguments: map[string]any{"query": "newer_than:1d"},
		}},
	}, nameMap.safeName)

	if len(wire.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(wire.ToolCalls))
	}
	if got := wire.ToolCalls[0].Function.Name; got != "gmail__dot__listEmails" {
		t.Fatalf("unexpected safe function name: %q", got)
	}

	providerMessage := providerMessageFromOpenAI(wire, nameMap.contractName)
	if len(providerMessage.ToolCalls) != 1 {
		t.Fatalf("expected one provider tool call, got %d", len(providerMessage.ToolCalls))
	}
	if got := providerMessage.ToolCalls[0].Name; got != "gmail.listEmails" {
		t.Fatalf("unexpected restored tool name: %q", got)
	}
}

func TestOpenAIClientRetriesTransientHTTPError(t *testing.T) {
	calls := 0
	client, err := NewOpenAIClient(OpenAIConfig{
		APIKey:  "test-key",
		Model:   "test-model",
		BaseURL: "https://api.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return nil, errors.New("connection reset")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)),
				Header:     make(http.Header),
			}, nil
		})},
	})
	if err != nil {
		t.Fatalf("NewOpenAIClient() error = %v", err)
	}

	response, err := client.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls)
	}
	if response.Message.Content != "ok" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
