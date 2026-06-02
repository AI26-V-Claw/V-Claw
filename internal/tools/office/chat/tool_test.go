package chat

import (
	"context"
	"errors"
	"testing"

	chatconnector "vclaw/internal/connectors/google/chat"
	"vclaw/internal/tools"
)

type fakeConnector struct {
	listOutput chatconnector.ListMessagesOutput
	sent       chatconnector.Message
	err        error
}

func (f fakeConnector) ListMessages(context.Context, string, int64, string, bool) (chatconnector.ListMessagesOutput, error) {
	if f.err != nil {
		return chatconnector.ListMessagesOutput{}, f.err
	}
	return f.listOutput, nil
}

func (f fakeConnector) CreateTextMessage(context.Context, string, string, chatconnector.MessageCreateOptions) (chatconnector.Message, error) {
	if f.err != nil {
		return chatconnector.Message{}, f.err
	}
	return f.sent, nil
}

func (f fakeConnector) CreateCardMessage(context.Context, string, chatconnector.CardMessage, chatconnector.MessageCreateOptions) (chatconnector.Message, error) {
	if f.err != nil {
		return chatconnector.Message{}, f.err
	}
	return f.sent, nil
}

func TestListMessagesRequiresSpace(t *testing.T) {
	service := NewService(fakeConnector{})

	_, errShape := service.ListMessages(context.Background(), ListMessagesInput{})
	if errShape == nil {
		t.Fatal("expected validation error")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %q", errShape.Code)
	}
}

func TestListMessagesUsesDefaultLimit(t *testing.T) {
	service := NewService(fakeConnector{
		listOutput: chatconnector.ListMessagesOutput{
			Messages: []chatconnector.Message{{Name: "spaces/A/messages/B", Text: "hello"}},
		},
	})

	output, errShape := service.ListMessages(context.Background(), ListMessagesInput{Space: "spaces/A"})
	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Message)
	}
	if len(output.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(output.Messages))
	}
}

func TestSendMessageRequiresSpace(t *testing.T) {
	service := NewService(fakeConnector{})

	_, errShape := service.SendMessage(context.Background(), SendMessageInput{Text: "hello"})
	if errShape == nil {
		t.Fatal("expected validation error")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %q", errShape.Code)
	}
}

func TestSendMessageRequiresText(t *testing.T) {
	service := NewService(fakeConnector{})

	_, errShape := service.SendMessage(context.Background(), SendMessageInput{Space: "spaces/A"})
	if errShape == nil {
		t.Fatal("expected validation error")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %q", errShape.Code)
	}
}

func TestSendMessageRejectsCardInput(t *testing.T) {
	service := NewService(fakeConnector{})

	_, errShape := service.SendMessage(context.Background(), SendMessageInput{
		Space:     "spaces/A",
		CardTitle: "Meeting Reminder",
		CardText:  "Team sync starts at 10:00.",
	})
	if errShape == nil {
		t.Fatal("expected validation error")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %q", errShape.Code)
	}
}

func TestSendMessageMapsConnectorError(t *testing.T) {
	service := NewService(fakeConnector{err: errors.New("boom")})

	_, errShape := service.SendMessage(context.Background(), SendMessageInput{Space: "spaces/A", Text: "hello"})
	if errShape == nil {
		t.Fatal("expected connector error")
	}
	if errShape.Code != "INTERNAL_ERROR" {
		t.Fatalf("expected INTERNAL_ERROR, got %q", errShape.Code)
	}
}

func TestToolRiskMetadata(t *testing.T) {
	service := NewService(fakeConnector{})
	listTool := NewListMessagesTool(service)
	sendTool := NewSendMessageTool(service)

	if listTool.Capability() != tools.CapabilityReadOnly || listTool.RiskLevel() != tools.RiskLevelSafeRead {
		t.Fatalf("list tool should be safe read")
	}
	if sendTool.Capability() != tools.CapabilityMutating || sendTool.RiskLevel() != tools.RiskLevelExternalWrite {
		t.Fatalf("send tool should be external write")
	}
}
