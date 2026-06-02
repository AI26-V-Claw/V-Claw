package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/contracts"
)

type RuntimeMessenger struct {
	runtime *Runtime
}

func NewRuntimeMessenger(runtime *Runtime) *RuntimeMessenger {
	return &RuntimeMessenger{runtime: runtime}
}

func (m *RuntimeMessenger) HandleMessage(ctx context.Context, msg InboundMessage) (OutboundMessage, error) {
	if m == nil || m.runtime == nil {
		return OutboundMessage{}, fmt.Errorf("runtime is required")
	}

	timestamp := msg.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	response, err := m.runtime.Run(ctx, contracts.UserMessage{
		RequestID: msg.EffectiveRequestID(),
		SessionID: msg.EffectiveSessionID(),
		Channel:   msg.EffectiveChannel(),
		Text:      strings.TrimSpace(msg.Text),
		Locale:    msg.Locale,
		Timestamp: timestamp,
		Metadata:  msg.Metadata,
	})
	if err != nil {
		return OutboundMessage{}, err
	}

	text := renderAgentResponse(response)
	return OutboundMessage{
		RequestID: response.RequestID,
		SessionID: response.SessionID,
		Status:    string(response.Status),
		Message:   response.Message,
		ChatID:    msg.ChatID,
		Text:      text,
	}, nil
}

func (m *RuntimeMessenger) FinalizeAudit(_ InboundMessage, _ error) {}

func (m *RuntimeMessenger) RecordIgnored(_ InboundMessage, _ string) {}

func renderAgentResponse(response contracts.AgentResponse) string {
	if response.ApprovalRequest != nil {
		approval := response.ApprovalRequest
		var input string
		if len(approval.ToolCall.Input) > 0 {
			data, err := json.MarshalIndent(approval.ToolCall.Input, "", "  ")
			if err == nil {
				input = "\nInput:\n" + string(data)
			}
		}
		return strings.TrimSpace(fmt.Sprintf(`Approval required
Tool: %s
Risk: %s
Summary: %s
Details: %s%s`, approval.ToolCall.ToolName, approval.RiskLevel, approval.Summary, approval.Details, input))
	}
	if strings.TrimSpace(response.Message) != "" {
		return response.Message
	}
	if response.Error != nil {
		return fmt.Sprintf("Error %s: %s", response.Error.Code, response.Error.Message)
	}
	for _, result := range response.ToolResults {
		if result.Success {
			if data, ok := result.Data.(map[string]any); ok {
				if text, ok := data["contentForUser"].(string); ok && strings.TrimSpace(text) != "" {
					return text
				}
			}
		}
	}
	return string(response.Status)
}
