package slack

import (
	"context"
	"testing"
)

func TestHandleSlackMessageIgnoresBotOwnMessages(t *testing.T) {
	bot := &Bot{botUserID: "U123"}

	if err := bot.handleSlackMessage(context.Background(), "C123", "U123", "hello", "123.45", "", "im"); err != nil {
		t.Fatalf("handleSlackMessage() returned error: %v", err)
	}
}
