package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"vclaw/internal/providers"
)

func TestTurnRouterPromptDefinesToolExposureOnly(t *testing.T) {
	prompt := turnRouterSystemPrompt()
	for _, want := range []string{
		"Your only job is tool exposure",
		"Do NOT classify the user's domain or action",
		"Do NOT choose tools",
		"Do NOT decide clarification",
		"Do NOT decide risk or approval",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected router prompt to contain %q, got:\n%s", want, prompt)
		}
	}
}

func TestLLMTurnRouterParsesModeAndReason(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: `{"mode":"no_tool","reason":"identity question"}`},
	}}}
	router := NewLLMTurnRouter(provider, "test-model")

	route, err := router.RouteTurn(context.Background(), TurnRouteInput{
		Message: "bạn là ai",
		Now:     time.Date(2026, 6, 4, 12, 0, 0, 0, time.FixedZone("ICT", 7*60*60)),
	})
	if err != nil {
		t.Fatalf("route turn: %v", err)
	}
	if route.Mode != TurnModeNoTool {
		t.Fatalf("expected no_tool, got %#v", route)
	}
	if route.Reason != "identity question" {
		t.Fatalf("unexpected reason: %#v", route)
	}
}

func TestLLMTurnRouterRejectsClassifierFields(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: `{"mode":"tool_enabled","reason":"needs tool","intent":"calendar","selected_tool":"calendar.listEvents"}`},
	}}}
	router := NewLLMTurnRouter(provider, "test-model")

	_, err := router.RouteTurn(context.Background(), TurnRouteInput{Message: "calendar hôm nay có gì"})
	if err == nil {
		t.Fatalf("expected forbidden classifier fields to be rejected")
	}
	if !strings.Contains(err.Error(), "forbidden classifier fields") {
		t.Fatalf("unexpected error: %v", err)
	}
}
