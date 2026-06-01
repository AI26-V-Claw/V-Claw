package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/nxhai/vclaw/internal/audit"
	"github.com/nxhai/vclaw/internal/intent"
	"github.com/nxhai/vclaw/internal/memory"
	"github.com/nxhai/vclaw/internal/providers"
)

type Orchestrator struct {
	memory     *memory.Store
	classifier *intent.Classifier
	responder  providers.ChatClient
	auditor    *audit.Logger
	mu         sync.Mutex
	pending    map[int64]audit.Entry
}

func NewOrchestrator(memoryStore *memory.Store, classifier *intent.Classifier, responder providers.ChatClient, auditor *audit.Logger) *Orchestrator {
	return &Orchestrator{
		memory:     memoryStore,
		classifier: classifier,
		responder:  responder,
		auditor:    auditor,
		pending:    map[int64]audit.Entry{},
	}
}

func (o *Orchestrator) HandleMessage(ctx context.Context, msg InboundMessage) (OutboundMessage, error) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return OutboundMessage{}, fmt.Errorf("empty message")
	}
	sessionID := msg.EffectiveSessionID()
	channel := msg.EffectiveChannel()

	if isHistoryQuery(text) {
		reply := formatHistory(o.memory.GetHistory(sessionID))
		o.memory.Append(sessionID, memory.RoleUser, text)
		o.memory.Append(sessionID, memory.RoleAssistant, reply)
		o.mu.Lock()
		o.pending[msg.UpdateID] = audit.Entry{
			RequestID:    msg.EffectiveRequestID(),
			UpdateID:     msg.UpdateID,
			Channel:      channel,
			ChatID:       msg.ChatID,
			UserID:       msg.UserID,
			SessionID:    sessionID,
			Input:        text,
			Intent:       string(intent.IntentReadInfo),
			SystemOpType: string(intent.SystemOpNone),
			Confidence:   1.0,
			ActionTaken:  "memory_summary",
			Output:       reply,
			HitlRequired: false,
		}
		o.mu.Unlock()
		return newOutboundMessage(msg, sessionID, reply, "completed"), nil
	}

	if isPromptInjection(text) {
		reply := "Mình không thể làm theo chỉ dẫn nằm trong tin nhắn. Bạn hãy nói rõ yêu cầu của bạn nhé."
		o.memory.Append(sessionID, memory.RoleUser, text)
		o.memory.Append(sessionID, memory.RoleAssistant, reply)
		o.mu.Lock()
		o.pending[msg.UpdateID] = audit.Entry{
			RequestID:    msg.EffectiveRequestID(),
			UpdateID:     msg.UpdateID,
			Channel:      channel,
			ChatID:       msg.ChatID,
			UserID:       msg.UserID,
			SessionID:    sessionID,
			Input:        text,
			Intent:       string(intent.IntentAmbiguous),
			SystemOpType: string(intent.SystemOpNone),
			Confidence:   0.5,
			ActionTaken:  "prompt_injection_blocked",
			Output:       reply,
			HitlRequired: false,
		}
		o.mu.Unlock()
		return newOutboundMessage(msg, sessionID, reply, "need_clarification"), nil
	}

	classification := o.classifier.Classify(text)
	reply, actionTaken := o.replyFor(ctx, sessionID, text, classification)

	o.memory.Append(sessionID, memory.RoleUser, text)
	o.memory.Append(sessionID, memory.RoleAssistant, reply)

	o.mu.Lock()
	o.pending[msg.UpdateID] = audit.Entry{
		RequestID:    msg.EffectiveRequestID(),
		UpdateID:     msg.UpdateID,
		Channel:      channel,
		ChatID:       msg.ChatID,
		UserID:       msg.UserID,
		SessionID:    sessionID,
		Input:        text,
		Intent:       string(classification.Intent),
		SystemOpType: string(classification.SystemOpType),
		Confidence:   classification.Confidence,
		ActionTaken:  actionTaken,
		Output:       reply,
		HitlRequired: false,
	}
	o.mu.Unlock()

	return newOutboundMessage(msg, sessionID, reply, statusForIntent(classification.Intent)), nil
}

func (o *Orchestrator) FinalizeAudit(msg InboundMessage, err error) {
	o.mu.Lock()
	entry, ok := o.pending[msg.UpdateID]
	if ok {
		delete(o.pending, msg.UpdateID)
	}
	o.mu.Unlock()

	if !ok {
		entry = audit.Entry{
			RequestID:    msg.EffectiveRequestID(),
			UpdateID:     msg.UpdateID,
			ChatID:       msg.ChatID,
			UserID:       msg.UserID,
			SessionID:    msg.EffectiveSessionID(),
			Input:        strings.TrimSpace(msg.Text),
			Intent:       string(intent.IntentAmbiguous),
			SystemOpType: string(intent.SystemOpNone),
			ActionTaken:  "error",
			HitlRequired: false,
		}
	}

	if err != nil {
		entry.Error = err.Error()
	}

	_ = o.auditor.Record(entry)
}

func (o *Orchestrator) RecordIgnored(msg InboundMessage, actionTaken string) {
	_ = o.auditor.Record(audit.Entry{
		RequestID:    msg.EffectiveRequestID(),
		UpdateID:     msg.UpdateID,
		Channel:      msg.EffectiveChannel(),
		ChatID:       msg.ChatID,
		UserID:       msg.UserID,
		SessionID:    msg.EffectiveSessionID(),
		Input:        strings.TrimSpace(msg.Text),
		Intent:       string(intent.IntentAmbiguous),
		SystemOpType: string(intent.SystemOpNone),
		ActionTaken:  actionTaken,
		HitlRequired: false,
	})
}

func (o *Orchestrator) replyFor(ctx context.Context, sessionID, text string, classification intent.IntentResult) (string, string) {
	if isHistoryQuery(text) {
		return formatHistory(o.memory.GetHistory(sessionID)), "memory_summary"
	}

	if o.responder != nil && classification.Intent != intent.IntentSystemOp {
		reply, err := o.generateLLMReply(ctx, sessionID, text, classification)
		if err == nil && strings.TrimSpace(reply) != "" {
			return reply, "llm_reply"
		}
	}

	switch classification.Intent {
	case intent.IntentGreeting:
		return "Chào bạn, tôi là V-Claw.", "greeting_reply"
	case intent.IntentReadInfo:
		return "Tôi hiểu đây là yêu cầu đọc thông tin. Connector sẽ được nối ở bước sau.", "read_info_placeholder"
	case intent.IntentSystemOp:
		return "Đây là hành động cần xác nhận. Sprint này tôi chưa thực thi hành động nguy hiểm.", "system_op_guard"
	case intent.IntentAmbiguous:
		if strings.TrimSpace(classification.ClarifyQuestion) != "" {
			return classification.ClarifyQuestion, "clarify_question"
		}
		return "Bạn muốn tôi làm gì cụ thể hơn?", "clarify_question"
	default:
		return "Bạn muốn tôi làm gì cụ thể hơn?", "clarify_question"
	}
}

func newOutboundMessage(msg InboundMessage, sessionID, reply, status string) OutboundMessage {
	return OutboundMessage{
		RequestID: msg.EffectiveRequestID(),
		SessionID: sessionID,
		Status:    status,
		Message:   reply,
		ChatID:    msg.ChatID,
		Text:      reply,
	}
}

func statusForIntent(intentType intent.Intent) string {
	if intentType == intent.IntentAmbiguous {
		return "need_clarification"
	}
	return "completed"
}

func (o *Orchestrator) generateLLMReply(ctx context.Context, sessionID, text string, classification intent.IntentResult) (string, error) {
	history := o.memory.GetHistory(sessionID)
	messages := make([]providers.ChatMessage, 0, len(history)+1)
	for _, message := range history {
		switch message.Role {
		case memory.RoleAssistant:
			messages = append(messages, providers.ChatMessage{Role: "assistant", Content: message.Text})
		default:
			messages = append(messages, providers.ChatMessage{Role: "user", Content: message.Text})
		}
	}
	messages = append(messages, providers.ChatMessage{Role: "user", Content: text})

	systemPrompt := buildSystemPrompt(classification)
	return o.responder.Complete(ctx, systemPrompt, messages)
}

func buildSystemPrompt(classification intent.IntentResult) string {
	return strings.TrimSpace(fmt.Sprintf(`You are V-Claw, a warm and concise Telegram assistant.
Reply in the user's language.
Make your answer natural, helpful, and not overly long.
If the user asks for Gmail, Calendar, Google Chat, or other integrations that are not connected yet, say that briefly and offer the closest safe alternative.
Do not claim to have performed external actions unless the app has actually done so.
If the intent seems ambiguous, ask one short clarifying question.
Current intent hint: %s.`, classification.Intent))
}

func isPromptInjection(text string) bool {
	lowered := strings.ToLower(strings.TrimSpace(text))
	phrases := []string{
		"ignore previous instructions",
		"bỏ qua chỉ dẫn",
		"bỏ qua mọi chỉ dẫn",
		"system prompt",
		"developer message",
		"jailbreak",
		"reveal prompt",
	}
	for _, phrase := range phrases {
		if strings.Contains(lowered, phrase) {
			return true
		}
	}
	return false
}

func isHistoryQuery(text string) bool {
	lowered := strings.ToLower(strings.TrimSpace(text))
	phrases := []string{
		"nãy tôi vừa nói gì",
		"nãy t vừa nói gì",
		"nãy mình vừa nói gì",
		"tôi vừa nói gì",
		"t vừa nói gì",
		"vừa rồi tôi nói gì",
		"vừa rồi mình nói gì",
		"nãy mình nói gì",
		"nãy giờ mình nói gì",
		"vừa rồi nói gì",
		"mình vừa nói gì",
		"kể những việc tôi vừa nói",
		"kể lại những gì tôi vừa nói",
		"kể những việc mình vừa nói",
		"kể lại những gì mình vừa nói",
		"hãy kể những việc tôi vừa nói",
		"hãy kể lại những gì tôi vừa nói",
		"hãy kể những việc mình vừa nói",
		"hãy kể lại những gì mình vừa nói",
	}
	for _, phrase := range phrases {
		if strings.Contains(lowered, phrase) {
			return true
		}
	}
	return false
}

func formatHistory(history []memory.Message) string {
	if len(history) == 0 {
		return "Tôi chưa có lịch sử hội thoại nào trong phiên này."
	}

	userMessages := make([]memory.Message, 0, len(history))
	for _, message := range history {
		if message.Role == memory.RoleUser && !isHistoryQuery(message.Text) {
			userMessages = append(userMessages, message)
		}
	}

	if len(userMessages) == 0 {
		return "Tôi chưa có lịch sử hội thoại nào trong phiên này."
	}

	var builder strings.Builder
	builder.WriteString("Đây là những gì bạn đã nói gần đây:\n")
	for index, message := range userMessages {
		builder.WriteString(fmt.Sprintf("%d. Bạn: %s\n", index+1, message.Text))
	}
	return strings.TrimSpace(builder.String())
}
