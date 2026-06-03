package slack

import (
	"context"
	"strings"
	"testing"

	"vclaw/internal/agent"
	"vclaw/internal/contracts"
)

func TestHandleSlackMessageIgnoresBotOwnMessages(t *testing.T) {
	bot := &Bot{botUserID: "U123"}

	if err := bot.handleSlackMessage(context.Background(), "C123", "U123", "hello", "123.45", "", "im"); err != nil {
		t.Fatalf("handleSlackMessage() returned error: %v", err)
	}
}

func TestIsAllowedRequiresSingleOwner(t *testing.T) {
	bot := &Bot{ownerUserID: "U1"}

	if !bot.isAllowed("C1", "U1") {
		t.Fatal("expected owner to be allowed")
	}
	if bot.isAllowed("C1", "U2") {
		t.Fatal("expected non-owner to be blocked")
	}
}

func TestIsAllowedRestrictsChannelsWhenConfigured(t *testing.T) {
	bot := &Bot{
		ownerUserID:     "U1",
		allowedChannels: makeAllowSet([]string{"C1"}),
	}

	if !bot.isAllowed("C1", "U1") {
		t.Fatal("expected owner in allowed channel to be allowed")
	}
	if bot.isAllowed("C2", "U1") {
		t.Fatal("expected owner in unlisted channel to be blocked")
	}
}

func TestSlackProgressTextMapsKnownTools(t *testing.T) {
	text := slackProgressText(agent.ProgressEvent{
		Stage:    agent.ProgressStageToolStarted,
		ToolName: "gmail.listEmails",
	})
	if !strings.Contains(text, "Gmail") {
		t.Fatalf("unexpected progress text: %q", text)
	}
}

func TestSlackTextFromFailedResponseHidesDetails(t *testing.T) {
	text := slackTextFromResponse(contracts.AgentResponse{
		Status:  contracts.AgentStatusFailed,
		Message: "provider chat failed: secret stack trace",
	})
	if strings.Contains(text, "provider chat failed") {
		t.Fatalf("failed response leaked detail: %q", text)
	}
	if text == "" {
		t.Fatal("expected generic error text")
	}
}
