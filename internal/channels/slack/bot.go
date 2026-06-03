package slack

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"vclaw/internal/agent"
	"vclaw/internal/contracts"
)

type Config struct {
	BotToken          string
	AppToken          string
	OwnerUserID       string
	AllowedChannelIDs []string
}

type Bot struct {
	config          Config
	orchestrator    messageHandler
	logger          *slog.Logger
	api             *slack.Client
	socketClient    *socketmode.Client
	botUserID       string
	ownerUserID     string
	allowedChannels map[string]struct{}
}

type messageHandler interface {
	HandleMessage(ctx context.Context, msg contracts.UserMessage) (contracts.AgentResponse, error)
	FinalizeAudit(msg contracts.UserMessage, err error)
	RecordIgnored(msg contracts.UserMessage, actionTaken string)
}

func New(cfg Config, orchestrator messageHandler, logger *slog.Logger) (*Bot, error) {
	if strings.TrimSpace(cfg.BotToken) == "" {
		return nil, fmt.Errorf("slack bot token is required")
	}
	if strings.TrimSpace(cfg.AppToken) == "" {
		return nil, fmt.Errorf("slack app token is required")
	}
	if strings.TrimSpace(cfg.OwnerUserID) == "" {
		return nil, fmt.Errorf("slack owner user id is required")
	}

	api := slack.New(cfg.BotToken, slack.OptionAppLevelToken(cfg.AppToken))
	socketClient := socketmode.New(api)

	bot := &Bot{
		config:          cfg,
		orchestrator:    orchestrator,
		logger:          logger,
		api:             api,
		socketClient:    socketClient,
		ownerUserID:     strings.TrimSpace(cfg.OwnerUserID),
		allowedChannels: makeAllowSet(cfg.AllowedChannelIDs),
	}

	auth, err := api.AuthTest()
	if err != nil {
		return nil, err
	}
	bot.botUserID = auth.UserID

	return bot, nil
}

func (b *Bot) Run(ctx context.Context) error {
	go b.socketClient.Run()

	b.logger.Info("slack bot ready", "bot_user_id", b.botUserID)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-b.socketClient.Events:
			if !ok {
				return nil
			}
			if err := b.handleEvent(ctx, event); err != nil {
				b.logger.Error("slack event failed", "type", event.Type, "error", err)
			}
		}
	}
}

func (b *Bot) handleEvent(ctx context.Context, event socketmode.Event) error {
	switch event.Type {
	case socketmode.EventTypeConnecting, socketmode.EventTypeConnected, socketmode.EventTypeConnectionError:
		return nil
	case socketmode.EventTypeEventsAPI:
		if event.Request != nil {
			b.socketClient.Ack(*event.Request)
		}
		eventsAPIEvent, ok := event.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return nil
		}
		if eventsAPIEvent.Type != slackevents.CallbackEvent {
			return nil
		}
		switch inner := eventsAPIEvent.InnerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			return b.handleSlackMessage(ctx, inner.Channel, inner.User, inner.Text, inner.TimeStamp, inner.ThreadTimeStamp, "app_mention")
		case *slackevents.MessageEvent:
			if inner.SubType != "" {
				return nil
			}
			if inner.ChannelType != "im" {
				return nil
			}
			return b.handleSlackMessage(ctx, inner.Channel, inner.User, inner.Text, inner.TimeStamp, inner.ThreadTimeStamp, inner.ChannelType)
		default:
			return nil
		}
	default:
		return nil
	}
}

func (b *Bot) handleSlackMessage(ctx context.Context, channelID, userID, text, timestamp, threadTimestamp, channelType string) error {
	if strings.TrimSpace(userID) == strings.TrimSpace(b.botUserID) {
		return nil
	}
	text = stripSlackMention(text, b.botUserID)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if !b.isAllowed(channelID, userID) {
		inbound := b.inboundMessage(channelID, userID, text, timestamp, threadTimestamp, channelType)
		b.orchestrator.RecordIgnored(inbound, "ignored_unauthorized")
		return nil
	}

	inbound := b.inboundMessage(channelID, userID, text, timestamp, threadTimestamp, channelType)
	replyThreadTimestamp := threadTimestamp
	if strings.TrimSpace(replyThreadTimestamp) == "" && channelType != "im" {
		replyThreadTimestamp = timestamp
	}

	processingTimestamp, err := b.sendMessage(ctx, channelID, "Đang xử lý...", replyThreadTimestamp)
	if err != nil {
		b.orchestrator.FinalizeAudit(inbound, err)
		return err
	}
	progress := newSlackProgressEditor(b, channelID, processingTimestamp)

	progressCtx := agent.WithProgressSink(ctx, func(progressCtx context.Context, event agent.ProgressEvent) {
		progressText := slackProgressText(event)
		if strings.TrimSpace(progressText) == "" {
			return
		}
		if err := progress.Update(progressCtx, progressText); err != nil {
			b.logger.Error("slack progress update failed", "error", err)
		}
	})

	outbound, err := b.orchestrator.HandleMessage(progressCtx, inbound)
	if err != nil {
		b.orchestrator.FinalizeAudit(inbound, err)
		b.logger.Error("agent handler failed", "request_id", inbound.RequestID, "session_id", inbound.SessionID, "error", err)
		_ = progress.Update(ctx, slackGenericErrorText())
		return nil
	}

	outboundText := slackTextFromResponse(outbound)
	if strings.TrimSpace(outboundText) == "" {
		err := fmt.Errorf("empty outbound message")
		b.orchestrator.FinalizeAudit(inbound, err)
		return err
	}
	if strings.EqualFold(string(outbound.Status), "failed") {
		b.logger.Error("agent response error", "request_id", outbound.RequestID, "session_id", outbound.SessionID, "status", outbound.Status, "message", outbound.Message)
	}
	if err := progress.Update(ctx, outboundText); err != nil {
		b.logger.Error("slack final update failed", "error", err)
		if _, sendErr := b.sendMessage(ctx, channelID, outboundText, replyThreadTimestamp); sendErr != nil {
			b.orchestrator.FinalizeAudit(inbound, sendErr)
			return sendErr
		}
	}

	b.orchestrator.FinalizeAudit(inbound, nil)
	return nil
}

func (b *Bot) sendMessage(ctx context.Context, channelID, text, threadTimestamp string) (string, error) {
	options := []slack.MsgOption{slack.MsgOptionText(text, false)}
	if strings.TrimSpace(threadTimestamp) != "" {
		options = append(options, slack.MsgOptionPostMessageParameters(slack.PostMessageParameters{
			ThreadTimestamp: threadTimestamp,
		}))
	}

	_, timestamp, err := b.api.PostMessageContext(ctx, channelID, options...)
	return timestamp, err
}

func (b *Bot) updateMessage(ctx context.Context, channelID, timestamp, text string) error {
	_, _, _, err := b.api.UpdateMessageContext(ctx, channelID, timestamp, slack.MsgOptionText(text, false))
	return err
}

func (b *Bot) isAllowed(channelID, userID string) bool {
	if strings.TrimSpace(userID) != b.ownerUserID {
		return false
	}
	if len(b.allowedChannels) > 0 {
		if _, ok := b.allowedChannels[channelID]; !ok {
			return false
		}
	}
	return true
}

func (b *Bot) inboundMessage(channelID, userID, text, timestamp, threadTimestamp, channelType string) contracts.UserMessage {
	sessionID := fmt.Sprintf("slack_channel_%s", channelID)
	if strings.TrimSpace(threadTimestamp) != "" {
		sessionID = fmt.Sprintf("slack_thread_%s_%s", channelID, threadTimestamp)
	}

	meta := map[string]any{
		"slack_channel_id":   channelID,
		"slack_user_id":      userID,
		"slack_thread_ts":    threadTimestamp,
		"slack_channel_type": channelType,
		"source":             "slack",
	}

	return contracts.UserMessage{
		RequestID: fmt.Sprintf("slack_%s_%s", channelID, normalizeSlackTimestamp(timestamp)),
		SessionID: sessionID,
		Channel:   "slack",
		Text:      strings.TrimSpace(text),
		Locale:    "",
		Metadata:  meta,
		Timestamp: time.Now().UTC(),
	}
}

func makeAllowSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result[trimmed] = struct{}{}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func stripSlackMention(text, botUserID string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.TrimSpace(botUserID) == "" {
		return trimmed
	}

	mention := fmt.Sprintf("<@%s>", botUserID)
	if strings.HasPrefix(trimmed, mention) {
		return strings.TrimSpace(strings.TrimPrefix(trimmed, mention))
	}
	return trimmed
}

func normalizeSlackTimestamp(timestamp string) string {
	trimmed := strings.TrimSpace(timestamp)
	if trimmed == "" {
		return "unknown"
	}
	return strings.ReplaceAll(trimmed, ".", "_")
}

type slackProgressEditor struct {
	bot       *Bot
	channelID string
	timestamp string
	lastText  string
}

func newSlackProgressEditor(bot *Bot, channelID, timestamp string) *slackProgressEditor {
	return &slackProgressEditor{
		bot:       bot,
		channelID: channelID,
		timestamp: timestamp,
		lastText:  "Đang xử lý...",
	}
}

func (e *slackProgressEditor) Update(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" || text == e.lastText {
		return nil
	}
	e.lastText = text
	return e.bot.updateMessage(ctx, e.channelID, e.timestamp, text)
}

func slackProgressText(event agent.ProgressEvent) string {
	switch event.Stage {
	case agent.ProgressStageThinking:
		return "Đang phân tích yêu cầu..."
	case agent.ProgressStageToolStarted:
		switch event.ToolName {
		case "people.searchDirectory":
			return "Đang tìm người trong Workspace..."
		case "chat.listSpaces":
			return "Đang dò Google Chat spaces..."
		case "chat.listMembers":
			return "Đang kiểm tra thành viên trong Chat space..."
		case "chat.findSpacesByMembers":
			return "Đang tìm Chat space phù hợp..."
		case "chat.listMessages":
			return "Đang lấy tin nhắn trong Google Chat..."
		case "gmail.listEmails", "gmail.getEmail", "gmail.listThreads", "gmail.getThread":
			return "Đang đọc Gmail..."
		case "calendar.listEvents":
			return "Đang đọc Google Calendar..."
		default:
			return "Đang chạy tool " + event.ToolName + "..."
		}
	case agent.ProgressStageFinalizing:
		return "Đang tổng hợp câu trả lời..."
	default:
		return ""
	}
}

func slackTextFromResponse(response contracts.AgentResponse) string {
	if strings.EqualFold(string(response.Status), "failed") {
		return slackGenericErrorText()
	}
	if strings.TrimSpace(response.Message) != "" {
		return response.Message
	}
	if response.ApprovalRequest != nil && strings.TrimSpace(response.ApprovalRequest.Summary) != "" {
		return response.ApprovalRequest.Summary
	}
	switch response.Status {
	case contracts.AgentStatusApprovalRequired:
		return "Tôi cần bạn xác nhận trước khi thực hiện hành động này."
	case contracts.AgentStatusCompleted:
		return "Đã hoàn tất."
	case contracts.AgentStatusNeedClarification:
		if response.Data != nil {
			if clarifyQuestion, ok := response.Data["clarifyQuestion"].(string); ok && strings.TrimSpace(clarifyQuestion) != "" {
				return clarifyQuestion
			}
		}
		return "Bạn muốn tôi làm gì cụ thể hơn?"
	default:
		return "Agent chưa có phản hồi."
	}
}

func slackGenericErrorText() string {
	return "Mình chưa thể hoàn tất yêu cầu này. Chi tiết lỗi đã được ghi ở terminal local."
}
