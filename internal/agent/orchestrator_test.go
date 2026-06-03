package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/intent"
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
	if strings.Contains(system, "You classify user intent for V-Claw") {
		return testIntentClassification(call.User), nil
	}
	return c.reply, nil
}

func testIntentClassification(input string) string {
	switch input {
	case "xin chào":
		return `{"intent":"GREETING","system_op_type":"NONE","confidence":0.98,"clarify_question":"","is_history_query":false}`
	case "đọc mail":
		return `{"intent":"READ_INFO","system_op_type":"NONE","confidence":0.91,"clarify_question":"","is_history_query":false}`
	case "gửi email":
		return `{"intent":"SYSTEM_OP","system_op_type":"SEND","confidence":0.92,"clarify_question":"","is_history_query":false}`
	case "tôi vừa nói gì", "xem lịch sử", "tôi đã nói những gì", "tôi đã từng nói gì":
		return `{"intent":"READ_INFO","system_op_type":"NONE","confidence":0.96,"clarify_question":"","is_history_query":true}`
	default:
		return `{"intent":"AMBIGUOUS","system_op_type":"NONE","confidence":0.31,"clarify_question":"Bạn muốn tôi làm gì cụ thể hơn?","is_history_query":false}`
	}
}

func newTestOrchestrator(reply string) (*Orchestrator, *testLLMClient) {
	client := &testLLMClient{reply: reply}
	return NewOrchestrator(memory.NewStore(), intent.NewClassifier(client), client), client
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

	response, err := orchestrator.HandleMessage(context.Background(), testMessage("xin chào"))
	if err != nil {
		t.Fatalf("HandleMessage() returned error: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("unexpected status: %s", response.Status)
	}
	if response.Message != "Chào bạn! Mình là V-Claw, mình có thể giúp gì cho bạn?" {
		t.Fatalf("unexpected reply: %q", response.Message)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected one classifier call, got %d", len(client.calls))
	}
}

func TestHandleMessageUsesLLMReplyForReadInfo(t *testing.T) {
	orchestrator, client := newTestOrchestrator("Mình vừa kiểm tra xong.")

	response, err := orchestrator.HandleMessage(context.Background(), testMessage("đọc mail"))
	if err != nil {
		t.Fatalf("HandleMessage() returned error: %v", err)
	}
	if response.Message != "Mình vừa kiểm tra xong." {
		t.Fatalf("unexpected reply: %q", response.Message)
	}
	if len(client.calls) != 2 {
		t.Fatalf("expected classifier + reply calls, got %d", len(client.calls))
	}
}

func TestHandleMessageUsesSystemGuard(t *testing.T) {
	orchestrator, client := newTestOrchestrator("should not be used")

	response, err := orchestrator.HandleMessage(context.Background(), testMessage("gửi email"))
	if err != nil {
		t.Fatalf("HandleMessage() returned error: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("unexpected status: %s", response.Status)
	}
	if response.Message != "Đây là hành động cần xác nhận." {
		t.Fatalf("unexpected reply: %q", response.Message)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected only classifier call, got %d", len(client.calls))
	}
}

func TestHandleMessageAsksClarifyForAmbiguous(t *testing.T) {
	orchestrator, client := newTestOrchestrator("should not be used")

	response, err := orchestrator.HandleMessage(context.Background(), testMessage("làm gì đó"))
	if err != nil {
		t.Fatalf("HandleMessage() returned error: %v", err)
	}
	if response.Status != contracts.AgentStatusNeedClarification {
		t.Fatalf("unexpected status: %s", response.Status)
	}
	if response.Message != "Bạn muốn tôi làm gì cụ thể hơn?" {
		t.Fatalf("unexpected reply: %q", response.Message)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected only classifier call, got %d", len(client.calls))
	}
}

func TestHandleMessageReturnsHistorySummary(t *testing.T) {
	orchestrator, _ := newTestOrchestrator("should not be used")
	orchestrator.memory.Append("sess_001", memory.RoleUserCompat, "xin chào")
	orchestrator.memory.Append("sess_001", memory.RoleAssistantCompat, "Chào bạn!")

	response, err := orchestrator.HandleMessage(context.Background(), contracts.UserMessage{
		RequestID: "req_002",
		SessionID: "sess_001",
		Channel:   "telegram",
		Text:      "tôi vừa nói gì",
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("HandleMessage() returned error: %v", err)
	}
	if response.Message == "" || response.Message == "should not be used" {
		t.Fatalf("unexpected history reply: %q", response.Message)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("unexpected status: %s", response.Status)
	}
}

func TestHandleMessageRecognizesPlainHistoryQuestion(t *testing.T) {
	orchestrator, _ := newTestOrchestrator("should not be used")
	orchestrator.memory.Append("sess_003", memory.RoleUserCompat, "đặt lịch họp mai")
	orchestrator.memory.Append("sess_003", memory.RoleAssistantCompat, "Đã ghi nhận.")

	response, err := orchestrator.HandleMessage(context.Background(), contracts.UserMessage{
		RequestID: "req_004",
		SessionID: "sess_003",
		Channel:   "telegram",
		Text:      "tôi đã nói những gì",
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("HandleMessage() returned error: %v", err)
	}
	if response.Message == "should not be used" {
		t.Fatal("expected history path to bypass llm")
	}
	if !strings.Contains(response.Message, "đặt lịch họp mai") {
		t.Fatalf("unexpected history reply: %q", response.Message)
	}
}

func TestHandleMessageRecognizesTungHistoryQuestion(t *testing.T) {
	orchestrator, _ := newTestOrchestrator("should not be used")
	orchestrator.memory.Append("sess_004", memory.RoleUserCompat, "gửi mail cho nam")
	orchestrator.memory.Append("sess_004", memory.RoleAssistantCompat, "Đã gửi yêu cầu.")

	response, err := orchestrator.HandleMessage(context.Background(), contracts.UserMessage{
		RequestID: "req_005",
		SessionID: "sess_004",
		Channel:   "telegram",
		Text:      "tôi đã từng nói gì",
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("HandleMessage() returned error: %v", err)
	}
	if response.Message == "should not be used" {
		t.Fatal("expected history path to bypass llm")
	}
	if !strings.Contains(response.Message, "gửi mail cho nam") {
		t.Fatalf("unexpected history reply: %q", response.Message)
	}
}

func TestHistoryQueryDoesNotPolluteHistory(t *testing.T) {
	orchestrator, _ := newTestOrchestrator("should not be used")
	orchestrator.memory.Append("sess_005", memory.RoleUserCompat, "ghi chú hôm nay")

	first, err := orchestrator.HandleMessage(context.Background(), contracts.UserMessage{
		RequestID: "req_006",
		SessionID: "sess_005",
		Channel:   "telegram",
		Text:      "tôi vừa nói gì",
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("first HandleMessage() returned error: %v", err)
	}
	if !strings.Contains(first.Message, "ghi chú hôm nay") {
		t.Fatalf("unexpected first history reply: %q", first.Message)
	}

	second, err := orchestrator.HandleMessage(context.Background(), contracts.UserMessage{
		RequestID: "req_007",
		SessionID: "sess_005",
		Channel:   "telegram",
		Text:      "tôi vừa nói gì",
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("second HandleMessage() returned error: %v", err)
	}
	if strings.Count(second.Message, "tôi vừa nói gì") > 0 {
		t.Fatalf("history reply should not include the query itself: %q", second.Message)
	}
	if !strings.Contains(second.Message, "ghi chú hôm nay") {
		t.Fatalf("unexpected second history reply: %q", second.Message)
	}
}
