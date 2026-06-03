package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"vclaw/internal/agent/intent"
	"vclaw/internal/contracts"
	"vclaw/internal/memory"
	"vclaw/internal/providers"
)

type testLLMClient struct {
	reply string
	calls []testLLMCall
}

type testLLMCall struct {
	System string
	User   string
}

func (c *testLLMClient) Complete(_ context.Context, system string, messages []providers.ChatMessage) (string, error) {
	call := testLLMCall{System: system}
	if len(messages) > 0 {
		call.User = messages[len(messages)-1].Content
	}
	c.calls = append(c.calls, call)
	return c.reply, nil
}

func newTestOrchestrator(reply string) (*Orchestrator, *testLLMClient) {
	client := &testLLMClient{reply: reply}
	return NewOrchestrator(memory.NewStore(), intent.NewClassifier(), client), client
}

func testMessage(text string) contracts.UserMessage {
	return contracts.UserMessage{
		RequestID: "req_001",
		SessionID: "sess_001",
		Channel:   "telegram",
		Text:      text,
		Timestamp: time.Now().UTC(),
	}
}

func TestHandleMessageReturnsGreeting(t *testing.T) {
	orchestrator, client := newTestOrchestrator("should not be used")

	response, err := orchestrator.HandleMessage(context.Background(), testMessage("hello"))
	if err != nil {
		t.Fatalf("HandleMessage() returned error: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("unexpected status: %s", response.Status)
	}
	if response.Message == "" {
		t.Fatal("expected greeting reply")
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected no llm reply calls, got %d", len(client.calls))
	}
}

func TestHandleMessageUsesLLMReplyForReadInfo(t *testing.T) {
	orchestrator, client := newTestOrchestrator("Checked your inbox.")

	response, err := orchestrator.HandleMessage(context.Background(), testMessage("read email"))
	if err != nil {
		t.Fatalf("HandleMessage() returned error: %v", err)
	}
	if response.Message != "Checked your inbox." {
		t.Fatalf("unexpected reply: %q", response.Message)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected one llm reply call, got %d", len(client.calls))
	}
}

func TestHandleMessageUsesSystemGuard(t *testing.T) {
	orchestrator, client := newTestOrchestrator("should not be used")

	response, err := orchestrator.HandleMessage(context.Background(), testMessage("send email"))
	if err != nil {
		t.Fatalf("HandleMessage() returned error: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("unexpected status: %s", response.Status)
	}
	if response.Message == "" {
		t.Fatal("expected system guard reply")
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected no llm reply calls, got %d", len(client.calls))
	}
}

func TestHandleMessageAsksClarifyForAmbiguous(t *testing.T) {
	orchestrator, client := newTestOrchestrator("should not be used")

	response, err := orchestrator.HandleMessage(context.Background(), testMessage("do something"))
	if err != nil {
		t.Fatalf("HandleMessage() returned error: %v", err)
	}
	if response.Status != contracts.AgentStatusNeedClarification {
		t.Fatalf("unexpected status: %s", response.Status)
	}
	if response.Message == "" {
		t.Fatal("expected clarify reply")
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected no llm reply calls, got %d", len(client.calls))
	}
}

func TestHandleMessageReturnsHistorySummary(t *testing.T) {
	orchestrator, _ := newTestOrchestrator("should not be used")
	orchestrator.memory.Append("sess_001", memory.RoleUserCompat, "create report")
	orchestrator.memory.Append("sess_001", memory.RoleAssistantCompat, "Noted.")

	response, err := orchestrator.HandleMessage(context.Background(), contracts.UserMessage{
		RequestID: "req_002",
		SessionID: "sess_001",
		Channel:   "telegram",
		Text:      "toi vua noi gi",
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("HandleMessage() returned error: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("unexpected status: %s", response.Status)
	}
	if !strings.Contains(response.Message, "create report") {
		t.Fatalf("unexpected history reply: %q", response.Message)
	}
}

func TestHistoryQueryDoesNotPolluteHistory(t *testing.T) {
	orchestrator, _ := newTestOrchestrator("should not be used")
	orchestrator.memory.Append("sess_005", memory.RoleUserCompat, "today note")

	first, err := orchestrator.HandleMessage(context.Background(), contracts.UserMessage{
		RequestID: "req_006",
		SessionID: "sess_005",
		Channel:   "telegram",
		Text:      "toi vua noi gi",
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("first HandleMessage() returned error: %v", err)
	}
	if !strings.Contains(first.Message, "today note") {
		t.Fatalf("unexpected first history reply: %q", first.Message)
	}

	second, err := orchestrator.HandleMessage(context.Background(), contracts.UserMessage{
		RequestID: "req_007",
		SessionID: "sess_005",
		Channel:   "telegram",
		Text:      "toi vua noi gi",
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("second HandleMessage() returned error: %v", err)
	}
	if strings.Contains(second.Message, "toi vua noi gi") {
		t.Fatalf("history reply should not include the query itself: %q", second.Message)
	}
	if !strings.Contains(second.Message, "today note") {
		t.Fatalf("unexpected second history reply: %q", second.Message)
	}
}
