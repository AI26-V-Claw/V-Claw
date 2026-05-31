package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/nxhai/vclaw/internal/audit"
	"github.com/nxhai/vclaw/internal/intent"
	"github.com/nxhai/vclaw/internal/memory"
)

type Orchestrator struct {
	memory     *memory.Store
	classifier *intent.Classifier
	auditor    *audit.Logger
	mu         sync.Mutex
	pending    map[int64]audit.Entry
}

func NewOrchestrator(memoryStore *memory.Store, classifier *intent.Classifier, auditor *audit.Logger) *Orchestrator {
	return &Orchestrator{
		memory:     memoryStore,
		classifier: classifier,
		auditor:    auditor,
		pending:    map[int64]audit.Entry{},
	}
}

func (o *Orchestrator) HandleMessage(_ context.Context, msg InboundMessage) (OutboundMessage, error) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return OutboundMessage{}, fmt.Errorf("empty message")
	}

	if isHistoryQuery(text) {
		reply := formatHistory(o.memory.GetHistory())
		o.memory.Append(memory.RoleUser, text)
		o.memory.Append(memory.RoleAssistant, reply)
		o.mu.Lock()
		o.pending[msg.UpdateID] = audit.Entry{
			UpdateID:     msg.UpdateID,
			ChatID:       msg.ChatID,
			UserID:       msg.UserID,
			Input:        text,
			Intent:       string(intent.IntentReadInfo),
			SystemOpType: string(intent.SystemOpNone),
			Confidence:   1.0,
			ActionTaken:  "memory_summary",
			Output:       reply,
			HitlRequired: false,
		}
		o.mu.Unlock()
		return OutboundMessage{ChatID: msg.ChatID, Text: reply}, nil
	}

	classification := o.classifier.Classify(text)
	reply, actionTaken := o.replyFor(text, classification)

	o.memory.Append(memory.RoleUser, text)
	o.memory.Append(memory.RoleAssistant, reply)

	o.mu.Lock()
	o.pending[msg.UpdateID] = audit.Entry{
		UpdateID:     msg.UpdateID,
		ChatID:       msg.ChatID,
		UserID:       msg.UserID,
		Input:        text,
		Intent:       string(classification.Intent),
		SystemOpType: string(classification.SystemOpType),
		Confidence:   classification.Confidence,
		ActionTaken:  actionTaken,
		Output:       reply,
		HitlRequired: false,
	}
	o.mu.Unlock()

	return OutboundMessage{ChatID: msg.ChatID, Text: reply}, nil
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
			UpdateID:     msg.UpdateID,
			ChatID:       msg.ChatID,
			UserID:       msg.UserID,
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
		UpdateID:     msg.UpdateID,
		ChatID:       msg.ChatID,
		UserID:       msg.UserID,
		Input:        strings.TrimSpace(msg.Text),
		Intent:       string(intent.IntentAmbiguous),
		SystemOpType: string(intent.SystemOpNone),
		ActionTaken:  actionTaken,
		HitlRequired: false,
	})
}

func (o *Orchestrator) replyFor(text string, classification intent.IntentResult) (string, string) {
	if isHistoryQuery(text) {
		return formatHistory(o.memory.GetHistory()), "memory_summary"
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
