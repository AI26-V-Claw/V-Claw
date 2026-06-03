package intent

import (
	"context"
	"strings"
)

// Intent is the lightweight classification used by the Telegram orchestrator.
type Intent string

// SystemOpType identifies the risky system operation family.
type SystemOpType string

const (
	IntentGreeting  Intent = "GREETING"
	IntentReadInfo  Intent = "READ_INFO"
	IntentSystemOp  Intent = "SYSTEM_OP"
	IntentAmbiguous Intent = "AMBIGUOUS"

	SystemOpNone   SystemOpType = "NONE"
	SystemOpSend   SystemOpType = "SEND"
	SystemOpDelete SystemOpType = "DELETE"
	SystemOpWrite  SystemOpType = "WRITE"
	SystemOpShell  SystemOpType = "SHELL"
)

// IntentResult keeps cmd/orchestrator wiring on the single classifier package.
type IntentResult struct {
	Intent          Intent
	SystemOpType    SystemOpType
	Confidence      float64
	ClarifyQuestion string
}

// Classify keeps the old orchestrator-facing API while delegating to the
// canonical classifier implementation in this package.
func (c *Classifier) Classify(text string) IntentResult {
	output, err := Classify(context.Background(), c, text)
	if err != nil || output == nil || output.Intent == nil {
		return IntentResult{Intent: IntentAmbiguous, SystemOpType: SystemOpNone, Confidence: 0.2, ClarifyQuestion: "Bạn muốn tôi làm gì cụ thể hơn?"}
	}

	result := IntentResult{
		Intent:          mapIntentType(output.Intent.Type),
		SystemOpType:    mapSystemOp(output.Intent.ToolCalls),
		Confidence:      output.Intent.Confidence,
		ClarifyQuestion: output.ClarificationMessage,
	}
	if result.ClarifyQuestion == "" && output.ClarificationOptions != nil {
		result.ClarifyQuestion = output.ClarificationOptions.Question
	}
	if result.ClarifyQuestion == "" && output.NeedsClarification {
		result.ClarifyQuestion = "Bạn muốn tôi làm gì cụ thể hơn?"
	}
	return result
}

func mapIntentType(t IntentType) Intent {
	switch t {
	case TypeGreeting:
		return IntentGreeting
	case TypeReadInfo:
		return IntentReadInfo
	case TypeDangerousAction, TypeComposite:
		return IntentSystemOp
	default:
		return IntentAmbiguous
	}
}

func mapSystemOp(calls []ToolCallInfo) SystemOpType {
	for _, call := range calls {
		name := NormalizeToolName(call.Name)
		switch {
		case name == "gmail.createDraft" || name == "gmail.sendDraft" || name == "chat.sendMessage":
			return SystemOpSend
		case name == "calendar.deleteEvent":
			return SystemOpDelete
		case name == "calendar.createEvent" || name == "calendar.updateEvent":
			return SystemOpWrite
		case name == "sandbox.runShell" || name == "sandbox.runPython":
			lower := strings.ToLower(toString(call.Parameters["command"]) + " " + toString(call.Parameters["code"]))
			if strings.Contains(lower, "delete") || strings.Contains(lower, "rm ") || strings.Contains(lower, "xóa") || strings.Contains(lower, "xoá") {
				return SystemOpDelete
			}
			return SystemOpShell
		}
	}
	return SystemOpNone
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
