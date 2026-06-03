package agent

import (
	"context"
	"fmt"
	"strings"

	"vclaw/internal/agent/intent"
	"vclaw/internal/contracts"
	"vclaw/internal/memory"
	"vclaw/internal/providers"
)

type Orchestrator struct {
	classifier *intent.Classifier
	responder  providers.ChatClient
	memory     *memory.Store
}

func NewOrchestrator(memoryStore *memory.Store, classifier *intent.Classifier, responder providers.ChatClient) *Orchestrator {
	return &Orchestrator{
		classifier: classifier,
		responder:  responder,
		memory:     memoryStore,
	}
}

func (o *Orchestrator) HandleMessage(ctx context.Context, msg contracts.UserMessage) (contracts.AgentResponse, error) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return contracts.AgentResponse{}, fmt.Errorf("empty message")
	}

	sessionID := strings.TrimSpace(msg.SessionID)
	if sessionID == "" {
		sessionID = "default"
	}

	if o.memory != nil && isHistoryQuery(text) {
		reply := formatHistory(o.memory.GetHistory(sessionID))
		return contracts.AgentResponse{
			RequestID: msg.RequestID,
			SessionID: sessionID,
			Status:    contracts.AgentStatusCompleted,
			Message:   reply,
			Data: map[string]any{
				"intent":       string(intent.IntentReadInfo),
				"systemOpType": string(intent.SystemOpNone),
				"confidence":   1.0,
				"actionTaken":  "memory_summary",
			},
		}, nil
	}

	classification := o.classifier.Classify(text)
	reply, actionTaken := o.replyFor(ctx, text, classification)

	if o.memory != nil {
		o.memory.Append(sessionID, memory.RoleUserCompat, text)
		o.memory.Append(sessionID, memory.RoleAssistantCompat, reply)
	}

	return contracts.AgentResponse{
		RequestID: msg.RequestID,
		SessionID: sessionID,
		Status:    statusForIntent(classification.Intent),
		Message:   reply,
		Data: map[string]any{
			"intent":       string(classification.Intent),
			"systemOpType": string(classification.SystemOpType),
			"confidence":   classification.Confidence,
			"actionTaken":  actionTaken,
		},
	}, nil
}

func (o *Orchestrator) FinalizeAudit(contracts.UserMessage, error) {}

func (o *Orchestrator) RecordIgnored(contracts.UserMessage, string) {}

func (o *Orchestrator) replyFor(ctx context.Context, text string, classification intent.IntentResult) (string, string) {
	if o.responder != nil && classification.Intent == intent.IntentReadInfo {
		reply, err := o.generateLLMReply(ctx, text, classification)
		if err == nil && strings.TrimSpace(reply) != "" {
			return reply, "llm_reply"
		}
	}

	switch classification.Intent {
	case intent.IntentGreeting:
		return "Chào bạn! Mình là V-Claw, mình có thể giúp gì cho bạn?", "greeting_reply"
	case intent.IntentReadInfo:
		return "Tôi hiểu đây là yêu cầu đọc thông tin.", "read_info_placeholder"
	case intent.IntentSystemOp:
		return "Đây là hành động cần xác nhận.", "system_op_guard"
	case intent.IntentAmbiguous:
		if strings.TrimSpace(classification.ClarifyQuestion) != "" {
			return classification.ClarifyQuestion, "clarify_question"
		}
		return "Bạn muốn tôi làm gì cụ thể hơn?", "clarify_question"
	default:
		return "Bạn muốn tôi làm gì cụ thể hơn?", "clarify_question"
	}
}

func statusForIntent(intentType intent.Intent) contracts.AgentStatus {
	if intentType == intent.IntentAmbiguous {
		return contracts.AgentStatusNeedClarification
	}
	return contracts.AgentStatusCompleted
}

func (o *Orchestrator) generateLLMReply(ctx context.Context, text string, classification intent.IntentResult) (string, error) {
	reply, err := o.responder.Complete(ctx, buildSystemPrompt(classification), []providers.ChatMessage{
		{Role: "user", Content: text},
	})
	if err != nil {
		return "", err
	}
	return normalizeLLMReply(reply), nil
}

func buildSystemPrompt(classification intent.IntentResult) string {
	return strings.TrimSpace(fmt.Sprintf(`You are V-Claw, a warm and concise Telegram assistant.
Reply in the user's language.
Make your answer natural, helpful, and not overly long.
Current intent hint: %s.`, classification.Intent))
}

func normalizeLLMReply(reply string) string {
	trimmed := strings.TrimSpace(reply)
	if trimmed == "" {
		return ""
	}

	lines := strings.Split(trimmed, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		joined := strings.Join(strings.Fields(line), " ")
		if len([]rune(joined)) <= 240 {
			return joined
		}
		runes := []rune(joined)
		return strings.TrimSpace(string(runes[:237])) + "..."
	}
	return trimmed
}

func formatHistory(history []memory.StoreMessage) string {
	if len(history) == 0 {
		return "Tôi chưa có lịch sử hội thoại nào trong phiên này."
	}

	userMessages := make([]string, 0, len(history))
	for _, message := range history {
		if message.Role == memory.RoleUserCompat {
			userMessages = append(userMessages, message.Text)
		}
	}
	if len(userMessages) == 0 {
		return "Tôi chưa có lịch sử hội thoại nào trong phiên này."
	}

	var builder strings.Builder
	builder.WriteString("Đây là những gì bạn đã nói gần đây:\n")
	for index, text := range userMessages {
		builder.WriteString(fmt.Sprintf("%d. Bạn: %s\n", index+1, text))
	}
	return strings.TrimSpace(builder.String())
}

func isHistoryQuery(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return containsHistoryPattern(
		lower,
		"tôi vừa nói gì",
		"toi vua noi gi",
		"xem lịch sử",
		"xem lich su",
		"tôi đã nói những gì",
		"toi da noi nhung gi",
		"tôi đã từng nói gì",
		"toi da tung noi gi",
	)
}

func containsHistoryPattern(text string, patterns ...string) bool {
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}
