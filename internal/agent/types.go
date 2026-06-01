package agent

import (
	"fmt"
	"strings"
	"time"
)

type InboundMessage struct {
	RequestID string
	SessionID string
	Channel   string
	UpdateID  int64
	ChatID    int64
	UserID    int64
	Text      string
	Locale    string
	Metadata  map[string]any
	Source    string
	Timestamp time.Time
}

func (m InboundMessage) EffectiveRequestID() string {
	if strings.TrimSpace(m.RequestID) != "" {
		return strings.TrimSpace(m.RequestID)
	}
	if m.UpdateID != 0 {
		return fmt.Sprintf("update_%d", m.UpdateID)
	}
	return "request_default"
}

func (m InboundMessage) EffectiveSessionID() string {
	if strings.TrimSpace(m.SessionID) != "" {
		return strings.TrimSpace(m.SessionID)
	}
	if m.UserID != 0 {
		return fmt.Sprintf("telegram_user_%d", m.UserID)
	}
	if m.ChatID != 0 {
		return fmt.Sprintf("telegram_chat_%d", m.ChatID)
	}
	return "default"
}

func (m InboundMessage) EffectiveChannel() string {
	if strings.TrimSpace(m.Channel) != "" {
		return strings.TrimSpace(m.Channel)
	}
	if strings.TrimSpace(m.Source) != "" {
		return strings.TrimSpace(m.Source)
	}
	return "telegram"
}

type OutboundMessage struct {
	RequestID string
	SessionID string
	Status    string
	Message   string
	ChatID    int64
	Text      string
}
