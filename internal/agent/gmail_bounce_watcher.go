package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
)

const (
	gmailBounceCheckDelay   = 6 * time.Second
	gmailBounceCheckTimeout = 20 * time.Second
	gmailBounceEarlySkew    = 2 * time.Minute
)

type sentGmailMessage struct {
	MessageID  string
	To         []string
	Subject    string
	SentAt     time.Time
	ToolCallID string
}

type gmailBounceCandidate struct {
	MessageID    string
	Snippet      string
	InternalDate time.Time
}

func (m *RuntimeMessenger) scheduleGmailBounceFollowUps(ctx context.Context, response contracts.AgentResponse) {
	sink, ok := followUpSinkFromContext(ctx)
	if !ok || m == nil || m.runtime == nil || m.runtime.registry == nil {
		return
	}
	for _, sent := range sentGmailMessagesFromResponse(response, time.Now().UTC()) {
		go m.watchGmailBounce(ctx, sink, response.SessionID, sent)
	}
}

func (m *RuntimeMessenger) watchGmailBounce(parent context.Context, sink FollowUpSink, sessionID string, sent sentGmailMessage) {
	if len(sent.To) == 0 {
		return
	}
	timer := time.NewTimer(gmailBounceCheckDelay)
	defer timer.Stop()
	select {
	case <-parent.Done():
		return
	case <-timer.C:
	}

	ctx, cancel := context.WithTimeout(context.Background(), gmailBounceCheckTimeout)
	defer cancel()

	bounce, recipient, ok := m.findRecentGmailBounce(ctx, sent)
	if !ok {
		return
	}
	text := gmailBounceFollowUpText(recipient, sent, bounce)
	if strings.TrimSpace(text) == "" {
		return
	}
	sink(ctx, FollowUpMessage{SessionID: sessionID, Text: text})
}

func (m *RuntimeMessenger) findRecentGmailBounce(ctx context.Context, sent sentGmailMessage) (gmailBounceCandidate, string, bool) {
	listResult := m.runtime.executeInternalPolicyCheckedTool(ctx, providers.ToolCall{
		ID:   "gmail_bounce_list_" + safeID(sent.ToolCallID),
		Name: "gmail.listEmails",
		Arguments: map[string]any{
			"query":      "from:mailer-daemon@googlemail.com newer_than:1d",
			"maxResults": 5,
		},
	})
	if !listResult.Success {
		return gmailBounceCandidate{}, "", false
	}

	for _, candidate := range gmailBounceCandidates(listResult.ContentForUser, sent.SentAt) {
		detailText := strings.TrimSpace(candidate.Snippet)
		if strings.TrimSpace(candidate.MessageID) != "" {
			getResult := m.runtime.executeInternalPolicyCheckedTool(ctx, providers.ToolCall{
				ID:   "gmail_bounce_get_" + safeID(candidate.MessageID),
				Name: "gmail.getEmail",
				Arguments: map[string]any{
					"messageId":    candidate.MessageID,
					"renderMode":   "text",
					"full":         true,
					"previewChars": 6000,
				},
			})
			if getResult.Success {
				detailText = strings.TrimSpace(detailText + "\n" + getResult.ContentForUser + "\n" + getResult.ContentForLLM)
			}
		}
		for _, recipient := range sent.To {
			if gmailBounceMentionsRecipient(detailText, recipient) {
				return candidate, recipient, true
			}
		}
	}
	return gmailBounceCandidate{}, "", false
}

func sentGmailMessagesFromResponse(response contracts.AgentResponse, now time.Time) []sentGmailMessage {
	var out []sentGmailMessage
	for _, result := range response.ToolResults {
		if !result.Success || result.ToolName != "gmail.sendDraft" {
			continue
		}
		data, ok := result.Data.(map[string]any)
		if !ok {
			continue
		}
		content, ok := data["contentForUser"].(string)
		if !ok {
			continue
		}
		sent, ok := sentGmailMessageFromContent(content)
		if !ok || len(sent.To) == 0 {
			continue
		}
		if sent.SentAt.IsZero() {
			sent.SentAt = now
		}
		sent.ToolCallID = result.ToolCallID
		out = append(out, sent)
	}
	return out
}

func sentGmailMessageFromContent(content string) (sentGmailMessage, bool) {
	value, ok := extractJSONValue(content)
	if !ok {
		return sentGmailMessage{}, false
	}
	root, ok := value.(map[string]any)
	if !ok {
		return sentGmailMessage{}, false
	}
	message, ok := nestedMap(root, "Message")
	if !ok {
		return sentGmailMessage{}, false
	}
	to := splitEmailHeader(firstStringValue(message, "To", "to"))
	return sentGmailMessage{
		MessageID: firstStringValue(message, "ID", "Id", "id"),
		To:        to,
		Subject:   firstStringValue(message, "Subject", "subject"),
		SentAt:    parseEmailDate(firstStringValue(message, "Date", "date")),
	}, len(to) > 0
}

func gmailBounceCandidates(content string, sentAt time.Time) []gmailBounceCandidate {
	value, ok := extractJSONValue(content)
	if !ok {
		return nil
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	rawMessages, ok := root["Messages"].([]any)
	if !ok {
		return nil
	}
	candidates := make([]gmailBounceCandidate, 0, len(rawMessages))
	for _, raw := range rawMessages {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		candidate := gmailBounceCandidate{
			MessageID: firstStringValue(message, "ID", "Id", "id"),
			Snippet:   firstStringValue(message, "Snippet", "snippet"),
		}
		if internalDate := millisTime(firstNumberValue(message, "InternalDate", "internalDate")); !internalDate.IsZero() {
			candidate.InternalDate = internalDate
			if !sentAt.IsZero() && internalDate.Before(sentAt.Add(-gmailBounceEarlySkew)) {
				continue
			}
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func gmailBounceMentionsRecipient(text string, recipient string) bool {
	lower := strings.ToLower(text)
	email := strings.ToLower(strings.TrimSpace(recipient))
	if email == "" || !strings.Contains(lower, email) {
		return false
	}
	for _, marker := range []string{
		"address not found",
		"your message wasn't delivered",
		"your message was not delivered",
		"couldn't be found",
		"could not be found",
		"unable to receive mail",
		"wasn't delivered",
		"undelivered mail",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func gmailBounceFollowUpText(recipient string, sent sentGmailMessage, _ gmailBounceCandidate) string {
	subject := strings.TrimSpace(sent.Subject)
	if subject != "" {
		return fmt.Sprintf("Email vừa gửi tới %s đã gặp lỗi: địa chỉ không tồn tại hoặc không thể nhận mail.\n\nChủ đề: %s", recipient, subject)
	}
	return fmt.Sprintf("Email vừa gửi tới %s đã gặp lỗi: địa chỉ không tồn tại hoặc không thể nhận mail.", recipient)
}

func splitEmailHeader(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if start := strings.LastIndex(trimmed, "<"); start >= 0 {
			if end := strings.LastIndex(trimmed, ">"); end > start {
				trimmed = strings.TrimSpace(trimmed[start+1 : end])
			}
		}
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func parseEmailDate(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if parsed, err := mail.ParseDate(value); err == nil {
		return parsed.UTC()
	}
	for _, layout := range []string{
		time.RFC1123Z,
		time.RFC1123,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"2 Jan 2006 15:04:05 -0700",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func firstNumberValue(payload map[string]any, keys ...string) float64 {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return typed
		case int64:
			return float64(typed)
		case int:
			return float64(typed)
		case json.Number:
			number, _ := typed.Float64()
			return number
		}
	}
	return 0
}

func millisTime(value float64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(int64(value)).UTC()
}
