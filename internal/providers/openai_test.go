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
	body     string
	requests []string
}

func (rt *openAITestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost {
		return nil, http.ErrUseLastResponse
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	rt.requests = append(rt.requests, string(body))
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(rt.body)),
	}, nil
}

func TestOpenAIGenerateRecordsUsageFromResponse(t *testing.T) {
	transport := &openAITestRoundTripper{body: `{
		"choices":[{"message":{"role":"assistant","content":"xin chao"}}],
		"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}
	}`}
	client, err := NewOpenAIClient(OpenAIConfig{
		APIKey: "test-key",
		Model:  "gpt-test",
		HTTPClient: &http.Client{
			Transport: transport,
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
	if len(transport.requests) != 1 || !strings.Contains(transport.requests[0], `"content":"hello"`) {
		t.Fatalf("expected text-only content string in request, got %#v", transport.requests)
	}
}

func TestOpenAIChatSendsImagePartsAsDataURL(t *testing.T) {
	transport := &openAITestRoundTripper{body: `{
		"choices":[{"message":{"role":"assistant","content":"looks like a tiny image"}}],
		"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}
	}`}
	client, err := NewOpenAIClient(OpenAIConfig{
		APIKey: "test-key",
		Model:  "gpt-test",
		HTTPClient: &http.Client{
			Transport: transport,
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	resp, err := client.Chat(context.Background(), ChatRequest{
		Messages: []Message{{
			Role:    MessageRoleUser,
			Content: "what is in this image?",
			Parts: []ContentPart{
				{Type: "text", Text: "what is in this image?"},
				{Type: "image", Image: &ImageContent{
					MIMEType: "image/png",
					Data:     []byte{1, 2, 3},
					Detail:   "auto",
				}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if strings.TrimSpace(resp.Message.Content) == "" {
		t.Fatal("expected assistant content")
	}
	if len(transport.requests) != 1 {
		t.Fatalf("expected one request, got %d", len(transport.requests))
	}
	body := transport.requests[0]
	if !strings.Contains(body, `"content":[`) {
		t.Fatalf("expected multimodal content array, got %s", body)
	}
	if !strings.Contains(body, `"type":"image_url"`) || !strings.Contains(body, `"url":"data:image/png;base64,AQID"`) {
		t.Fatalf("expected image data URL, got %s", body)
	}
	if !strings.Contains(body, `"detail":"auto"`) {
		t.Fatalf("expected image detail, got %s", body)
	}
}

func TestOpenAIClientCapabilitiesIncludeImageInput(t *testing.T) {
	client, err := NewOpenAIClient(OpenAIConfig{
		APIKey: "test-key",
		Model:  "gpt-test",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if !client.Capabilities().ImageInput {
		t.Fatal("expected OpenAI client to advertise image input support")
	}
}
