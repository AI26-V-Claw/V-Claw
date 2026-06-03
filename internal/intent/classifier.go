package intent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"vclaw/internal/providers"
)

type Intent string

const (
	IntentGreeting  Intent = "GREETING"
	IntentReadInfo  Intent = "READ_INFO"
	IntentSystemOp  Intent = "SYSTEM_OP"
	IntentAmbiguous Intent = "AMBIGUOUS"
)

type SystemOpType string

const (
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
	IsHistoryQuery  bool
}

type Classifier struct {
	responder providers.ChatClient
}

func NewClassifier(responder providers.ChatClient) *Classifier {
	return &Classifier{responder: responder}
}

func (c *Classifier) Classify(text string) IntentResult {
	if c == nil || c.responder == nil {
		return ambiguousResult()
	}

	systemPrompt := strings.TrimSpace(`You classify user intent for V-Claw.
Return strict JSON only with keys: intent, system_op_type, confidence, clarify_question, is_history_query.
intent must be one of GREETING, READ_INFO, SYSTEM_OP, AMBIGUOUS.
system_op_type must be one of NONE, SEND, DELETE, WRITE, SHELL.
Use SYSTEM_OP for side-effecting or local-execution requests.
Use READ_INFO for read-only information requests.
Set is_history_query to true when the user asks to recall, summarize, or list what they just said or said recently.
Use GREETING for greetings and casual salutations.
Use AMBIGUOUS when the request is unclear and set clarify_question to one short Vietnamese question.
Keep confidence between 0 and 1.
No markdown, no code fences.`)

	reply, err := c.responder.Complete(context.Background(), systemPrompt, []providers.ChatMessage{
		{Role: "user", Content: strings.TrimSpace(text)},
	})
	if err != nil {
		return ambiguousResult()
	}

	result, parseErr := parseClassifierResponse(reply)
	if parseErr != nil {
		return ambiguousResult()
	}
	return result
}

func parseClassifierResponse(reply string) (IntentResult, error) {
	reply = strings.TrimSpace(reply)
	reply = strings.TrimPrefix(reply, "```json")
	reply = strings.TrimPrefix(reply, "```")
	reply = strings.TrimSuffix(reply, "```")
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return IntentResult{}, fmt.Errorf("empty classifier response")
	}

	var payload struct {
		Intent          string  `json:"intent"`
		SystemOpType    string  `json:"system_op_type"`
		Confidence      float64 `json:"confidence"`
		ClarifyQuestion string  `json:"clarify_question"`
		IsHistoryQuery  bool    `json:"is_history_query"`
	}
	if err := json.Unmarshal([]byte(reply), &payload); err != nil {
		return IntentResult{}, err
	}

	intentType := normalizeIntent(payload.Intent)
	systemOpType := normalizeSystemOpType(payload.SystemOpType)
	if intentType == IntentAmbiguous && strings.TrimSpace(payload.ClarifyQuestion) == "" {
		payload.ClarifyQuestion = "Bạn muốn tôi làm gì cụ thể hơn?"
	}
	if intentType == IntentSystemOp && systemOpType == SystemOpNone {
		systemOpType = SystemOpWrite
	}

	return IntentResult{
		Intent:          intentType,
		SystemOpType:    systemOpType,
		Confidence:      clampConfidence(payload.Confidence),
		ClarifyQuestion: strings.TrimSpace(payload.ClarifyQuestion),
		IsHistoryQuery:  payload.IsHistoryQuery,
	}, nil
}

func normalizeIntent(value string) Intent {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case string(IntentGreeting):
		return IntentGreeting
	case string(IntentReadInfo):
		return IntentReadInfo
	case string(IntentSystemOp):
		return IntentSystemOp
	case string(IntentAmbiguous):
		return IntentAmbiguous
	default:
		return IntentAmbiguous
	}
}

func normalizeSystemOpType(value string) SystemOpType {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case string(SystemOpSend):
		return SystemOpSend
	case string(SystemOpDelete):
		return SystemOpDelete
	case string(SystemOpWrite):
		return SystemOpWrite
	case string(SystemOpShell):
		return SystemOpShell
	default:
		return SystemOpNone
	}
}

func clampConfidence(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

func ambiguousResult() IntentResult {
	return IntentResult{
		Intent:          IntentAmbiguous,
		SystemOpType:    SystemOpNone,
		Confidence:      0.31,
		ClarifyQuestion: "Bạn muốn tôi làm gì cụ thể hơn?",
		IsHistoryQuery:  false,
	}
}
