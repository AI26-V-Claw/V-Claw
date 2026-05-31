package intent

import (
	"strings"
)

type Intent string

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

type IntentResult struct {
	Intent          Intent
	SystemOpType    SystemOpType
	Confidence      float64
	ClarifyQuestion string
}

type Result struct {
	Intent     string
	ActionType string
}

const (
	LegacyIntentGreeting     = "greeting"
	LegacyIntentReadMail     = "read_mail"
	LegacyIntentReadCalendar = "read_calendar"
	LegacyIntentWriteAction  = "write_action"
	LegacyIntentSandboxTask  = "sandbox_task"
	LegacyIntentUnknown      = "unknown"
)

type Classifier struct{}

func NewClassifier() *Classifier {
	return &Classifier{}
}

func (c *Classifier) Classify(text string) IntentResult {
	lowered := strings.ToLower(strings.TrimSpace(text))

	switch {
	case isGreeting(lowered):
		return IntentResult{Intent: IntentGreeting, SystemOpType: SystemOpNone, Confidence: 0.98}
	case strings.Contains(lowered, "gửi"):
		return IntentResult{Intent: IntentSystemOp, SystemOpType: SystemOpSend, Confidence: 0.92}
	case strings.Contains(lowered, "xóa") || strings.Contains(lowered, "xoá"):
		return IntentResult{Intent: IntentSystemOp, SystemOpType: SystemOpDelete, Confidence: 0.92}
	case strings.Contains(lowered, "tạo") || strings.Contains(lowered, "sửa") || strings.Contains(lowered, "ghi file") || strings.Contains(lowered, "draft"):
		return IntentResult{Intent: IntentSystemOp, SystemOpType: SystemOpWrite, Confidence: 0.9}
	case strings.Contains(lowered, "chạy") || strings.Contains(lowered, "script") || strings.Contains(lowered, "shell") || strings.Contains(lowered, "python"):
		return IntentResult{Intent: IntentSystemOp, SystemOpType: SystemOpShell, Confidence: 0.9}
	case strings.Contains(lowered, "mail") || strings.Contains(lowered, "email") || strings.Contains(lowered, "lịch") || strings.Contains(lowered, "calendar") || strings.Contains(lowered, "họp"):
		return IntentResult{Intent: IntentReadInfo, SystemOpType: SystemOpNone, Confidence: 0.9}
	default:
		return IntentResult{
			Intent:          IntentAmbiguous,
			SystemOpType:    SystemOpNone,
			Confidence:      0.2,
			ClarifyQuestion: "Bạn muốn tôi làm gì cụ thể hơn?",
		}
	}
}

func isGreeting(lowered string) bool {
	if lowered == "hello" || lowered == "hi" {
		return true
	}

	for _, token := range strings.Fields(lowered) {
		if token == "hi" {
			return true
		}
	}

	return strings.Contains(lowered, "xin chào") || strings.Contains(lowered, "chào")
}

func (c *Classifier) ClassifyLegacy(text string) Result {
	lowered := strings.ToLower(strings.TrimSpace(text))

	switch {
	case strings.Contains(lowered, "xin chao") || strings.Contains(lowered, "hello"):
		return Result{Intent: LegacyIntentGreeting, ActionType: "safe_read"}
	case strings.Contains(lowered, "mail") || strings.Contains(lowered, "email") || strings.Contains(lowered, "gmail"):
		if strings.Contains(lowered, "gui") || strings.Contains(lowered, "send") {
			return Result{Intent: LegacyIntentWriteAction, ActionType: "gmail_send"}
		}
		return Result{Intent: LegacyIntentReadMail, ActionType: "gmail_read"}
	case strings.Contains(lowered, "lich") || strings.Contains(lowered, "calendar"):
		if strings.Contains(lowered, "tao") || strings.Contains(lowered, "xoa") || strings.Contains(lowered, "sua") {
			return Result{Intent: LegacyIntentWriteAction, ActionType: "calendar_write"}
		}
		return Result{Intent: LegacyIntentReadCalendar, ActionType: "calendar_read"}
	case strings.Contains(lowered, "python") || strings.Contains(lowered, "shell") || strings.Contains(lowered, "file"):
		return Result{Intent: LegacyIntentSandboxTask, ActionType: "sandbox_exec"}
	default:
		return Result{Intent: LegacyIntentUnknown, ActionType: "unknown"}
	}
}
