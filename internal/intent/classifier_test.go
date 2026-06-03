package intent

import (
	"context"
	"testing"

	"vclaw/internal/providers"
)

type fakeChatClient struct {
	reply       string
	lastSystem  string
	lastMessage string
	calls       int
}

func (f *fakeChatClient) Complete(_ context.Context, system string, messages []providers.ChatMessage) (string, error) {
	f.calls++
	f.lastSystem = system
	if len(messages) > 0 {
		f.lastMessage = messages[len(messages)-1].Content
	}
	return f.reply, nil
}

func TestClassifierParsesLLMResponse(t *testing.T) {
	cases := []struct {
		name            string
		reply           string
		wantIntent      Intent
		wantSystemOp    SystemOpType
		wantClarifyText string
		wantHistory     bool
	}{
		{
			name:         "greeting",
			reply:        `{"intent":"GREETING","system_op_type":"NONE","confidence":0.98,"clarify_question":"","is_history_query":false}`,
			wantIntent:   IntentGreeting,
			wantSystemOp: SystemOpNone,
		},
		{
			name:         "read info",
			reply:        `{"intent":"READ_INFO","system_op_type":"NONE","confidence":0.91,"is_history_query":true}`,
			wantIntent:   IntentReadInfo,
			wantSystemOp: SystemOpNone,
			wantHistory:  true,
		},
		{
			name:         "system op",
			reply:        "```json\n{\"intent\":\"SYSTEM_OP\",\"system_op_type\":\"SEND\",\"confidence\":0.84,\"is_history_query\":false}\n```",
			wantIntent:   IntentSystemOp,
			wantSystemOp: SystemOpSend,
		},
		{
			name:            "ambiguous",
			reply:           `{"intent":"AMBIGUOUS","system_op_type":"NONE","confidence":0.31,"clarify_question":"Bạn muốn tôi làm gì cụ thể hơn?","is_history_query":false}`,
			wantIntent:      IntentAmbiguous,
			wantSystemOp:    SystemOpNone,
			wantClarifyText: "Bạn muốn tôi làm gì cụ thể hơn?",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &fakeChatClient{reply: testCase.reply}
			classifier := NewClassifier(client)

			result := classifier.Classify("đầu vào bất kỳ")

			if result.Intent != testCase.wantIntent {
				t.Fatalf("unexpected intent: got %s want %s", result.Intent, testCase.wantIntent)
			}
			if result.SystemOpType != testCase.wantSystemOp {
				t.Fatalf("unexpected system op: got %s want %s", result.SystemOpType, testCase.wantSystemOp)
			}
			if testCase.wantClarifyText != "" && result.ClarifyQuestion != testCase.wantClarifyText {
				t.Fatalf("unexpected clarify question: got %q want %q", result.ClarifyQuestion, testCase.wantClarifyText)
			}
			if result.IsHistoryQuery != testCase.wantHistory {
				t.Fatalf("unexpected history flag: got %v want %v", result.IsHistoryQuery, testCase.wantHistory)
			}
			if client.calls != 1 {
				t.Fatalf("expected one llm call, got %d", client.calls)
			}
			if client.lastSystem == "" {
				t.Fatal("expected classifier to send a system prompt")
			}
			if client.lastMessage != "đầu vào bất kỳ" {
				t.Fatalf("unexpected last user message: %q", client.lastMessage)
			}
		})
	}
}

func TestClassifierFallsBackToAmbiguousOnBadResponse(t *testing.T) {
	classifier := NewClassifier(&fakeChatClient{reply: "not json"})

	result := classifier.Classify("xem lịch")

	if result.Intent != IntentAmbiguous {
		t.Fatalf("expected ambiguous intent, got %s", result.Intent)
	}
	if result.ClarifyQuestion == "" {
		t.Fatal("expected clarify question on fallback")
	}
}
