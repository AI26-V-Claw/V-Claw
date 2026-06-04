package slack

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/slack-go/slack"

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

func TestSlackProgressTextHidesInternalRoutingStages(t *testing.T) {
	for _, stage := range []agent.ProgressStage{
		agent.ProgressStageClassifying,
		agent.ProgressStageClassified,
		agent.ProgressStagePlanning,
		agent.ProgressStagePlanned,
		agent.ProgressStageThinking,
		agent.ProgressStageFinalizing,
	} {
		if got := slackProgressText(agent.ProgressEvent{Stage: stage}); got != "" {
			t.Fatalf("expected stage %s to be hidden, got %q", stage, got)
		}
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

func TestSlackApprovalValueRoundTrip(t *testing.T) {
	value := slackApprovalValue("approve", "appr_123", "slack_channel_C1")
	action, approvalID, sessionID, ok := parseSlackApprovalValue(value)
	if !ok {
		t.Fatalf("expected approval value to parse: %q", value)
	}
	if action != "approve" || approvalID != "appr_123" || sessionID != "slack_channel_C1" {
		t.Fatalf("unexpected parse result action=%q approvalID=%q sessionID=%q", action, approvalID, sessionID)
	}
}

func TestSlackApprovalBlocksContainMultipleChoiceButtons(t *testing.T) {
	blocks := slackApprovalBlocks("Approval required", "appr_123", "sess_1")
	if len(blocks) != 2 {
		t.Fatalf("expected section and actions blocks, got %#v", blocks)
	}
	actionBlock, ok := blocks[1].(*slack.ActionBlock)
	if !ok {
		t.Fatalf("expected action block, got %T", blocks[1])
	}
	if len(actionBlock.Elements.ElementSet) != 3 {
		t.Fatalf("expected three approval buttons, got %#v", actionBlock.Elements.ElementSet)
	}
	labels := []string{}
	for _, element := range actionBlock.Elements.ElementSet {
		button, ok := element.(*slack.ButtonBlockElement)
		if !ok {
			t.Fatalf("expected button element, got %T", element)
		}
		labels = append(labels, button.Text.Text)
	}
	for _, want := range []string{"Yes", "No", "Revise"} {
		if !containsString(labels, want) {
			t.Fatalf("expected labels to contain %q, got %#v", want, labels)
		}
	}
}

func TestSlackReviseCommentReadsModalState(t *testing.T) {
	var callback slack.InteractionCallback
	raw := `{
		"type":"view_submission",
		"view":{
			"state":{
				"values":{
					"vclaw_approval_comment":{
						"comment":{"type":"plain_text_input","value":"đổi giờ sang 10:00"}
					}
				}
			}
		}
	}`
	if err := json.Unmarshal([]byte(raw), &callback); err != nil {
		t.Fatalf("unmarshal callback: %v", err)
	}
	if got := slackReviseComment(callback); got != "đổi giờ sang 10:00" {
		t.Fatalf("unexpected revise comment: %q", got)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
