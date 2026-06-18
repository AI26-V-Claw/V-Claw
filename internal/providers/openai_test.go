package providers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type openAITestRoundTripper struct {
	body string
}

func (rt openAITestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost {
		return nil, http.ErrUseLastResponse
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(rt.body)),
	}, nil
}

func TestOpenAIGenerateRecordsUsageFromResponse(t *testing.T) {
	client, err := NewOpenAIClient(OpenAIConfig{
		APIKey: "test-key",
		Model:  "gpt-test",
		HTTPClient: &http.Client{
			Transport: openAITestRoundTripper{body: `{
				"choices":[{"message":{"role":"assistant","content":"xin chao"}}],
				"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}
			}`},
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	var recorded *Usage
	ctx := WithUsageRecorder(context.Background(), func(usage *Usage) {
		recorded = usage
	})

	resp, err := client.Generate(ctx, &GenerateRequest{UserPrompt: "hello"})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Usage == nil {
		t.Fatal("expected usage on response")
	}
	if resp.Usage.PromptTokens != 11 || resp.Usage.CompletionTokens != 7 || resp.Usage.TotalTokens != 18 {
		t.Fatalf("unexpected response usage: %#v", resp.Usage)
	}
	if recorded == nil {
		t.Fatal("expected usage callback")
	}
	if recorded.PromptTokens != 11 || recorded.CompletionTokens != 7 || recorded.TotalTokens != 18 {
		t.Fatalf("unexpected recorded usage: %#v", recorded)
	}
	if got := strings.TrimSpace(resp.Text); got != "xin chao" {
		t.Fatalf("response text = %q", got)
	}
}
