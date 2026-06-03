package intent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"vclaw/internal/providers"
)

type captureProvider struct {
	request *providers.GenerateRequest
	text    string
}

func (p *captureProvider) Chat(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{}, nil
}

func (p *captureProvider) Generate(_ context.Context, req *providers.GenerateRequest) (*providers.GenerateResponse, error) {
	p.request = req
	return &providers.GenerateResponse{Text: p.text}, nil
}

func (p *captureProvider) Name() string { return "capture" }

func (p *captureProvider) Close() error { return nil }

type failingClassifier struct{}

func (failingClassifier) Classify(context.Context, string) (*ClassificationOutput, error) {
	return nil, fmt.Errorf("provider unavailable")
}

func TestLLMClassifierUsesXMLSystemPrompt(t *testing.T) {
	provider := &captureProvider{text: `{
		"intent_type": "READ_INFO",
		"confidence": 0.91,
		"required_params": [],
		"provided_params": {},
		"missing_params": [],
		"tool_calls": [],
		"needs_confirm": false,
		"reasoning": "Người dùng muốn đọc thông tin.",
		"timestamp": "2026-06-03T10:00:00Z"
	}`}
	classifier, err := NewLLMClassifier(provider, DefaultConfig)
	if err != nil {
		t.Fatalf("create classifier: %v", err)
	}

	output, err := classifier.Classify(context.Background(), "liệt kê 10 email gần đây")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if output.Intent.Type != TypeReadInfo {
		t.Fatalf("expected READ_INFO, got %#v", output.Intent.Type)
	}
	for _, want := range []string{
		"<persona>",
		"<rules>",
		"<tools_instruction>",
		"<response_format>",
		"<constraints>",
		"tiếng Việt",
		"gmail.listEmails",
		"calendar.createEvent",
	} {
		if !strings.Contains(provider.request.SystemPrompt, want) {
			t.Fatalf("expected system prompt to contain %q, got:\n%s", want, provider.request.SystemPrompt)
		}
	}
	if !strings.Contains(provider.request.UserPrompt, "<intent_classification_request>") {
		t.Fatalf("expected XML user prompt, got %q", provider.request.UserPrompt)
	}
}

func TestLLMClassifierMemoryPromptAllowsActiveClarificationContext(t *testing.T) {
	provider := &captureProvider{text: `{
		"intent_type": "DANGEROUS_ACTION",
		"confidence": 0.9,
		"required_params": [],
		"provided_params": {},
		"missing_params": [],
		"tool_calls": [],
		"needs_confirm": true,
		"reasoning": "Nguoi dung dang tra loi cau hoi lam ro.",
		"timestamp": "2026-06-03T10:00:00Z"
	}`}
	classifier, err := NewLLMClassifier(provider, DefaultConfig)
	if err != nil {
		t.Fatalf("create classifier: %v", err)
	}

	if _, err := classifier.ClassifyWithMemoryIsolation(context.Background(), "thoi gian hop la 1 tieng", []string{
		"user: Tao lich hop voi Bao ngay mai luc 10am",
		"assistant: Ban co the cung cap thoi gian ket thuc khong?",
	}); err != nil {
		t.Fatalf("classify with memory: %v", err)
	}

	for _, want := range []string{
		"latest assistant message asked a clarification question",
		"current user message directly answers it",
		"keep needs_confirm=true",
		"negative answer to an optional clarification question",
		"recent_history",
		"thoi gian hop la 1 tieng",
	} {
		if !strings.Contains(provider.request.UserPrompt, want) {
			t.Fatalf("expected memory prompt to contain %q, got:\n%s", want, provider.request.UserPrompt)
		}
	}
}

func TestFallbackClassifierUsesHeuristicWhenPrimaryFails(t *testing.T) {
	classifier := NewFallbackClassifier(failingClassifier{}, NewHeuristicRunner(DefaultConfig))

	output, err := classifier.Classify(context.Background(), "liệt kê 10 email gần đây")
	if err != nil {
		t.Fatalf("classify with fallback: %v", err)
	}
	if output == nil || output.Intent == nil {
		t.Fatalf("expected fallback output")
	}
	if output.Intent.Type != TypeReadInfo {
		t.Fatalf("expected READ_INFO from heuristic fallback, got %#v", output.Intent.Type)
	}
	if len(output.Intent.ToolCalls) == 0 || output.Intent.ToolCalls[0].Name != "gmail.listEmails" {
		t.Fatalf("expected gmail.listEmails fallback tool, got %#v", output.Intent.ToolCalls)
	}
}
