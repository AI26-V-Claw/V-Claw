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

	"vclaw/internal/contracts"
)

type Config struct {
	BotToken          string
	AppToken          string
	AllowedChannelIDs []string
	AllowedUserIDs    []string
}

type Bot struct {
	config          Config
	orchestrator    messageHandler
	logger          *slog.Logger
	api             *slack.Client
	socketClient    *socketmode.Client
	botUserID       string
	allowedChannels map[string]struct{}
	allowedUsers    map[string]struct{}
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

	api := slack.New(cfg.BotToken, slack.OptionAppLevelToken(cfg.AppToken))
	socketClient := socketmode.New(api)

	bot := &Bot{
		config:          cfg,
		orchestrator:    orchestrator,
		logger:          logger,
		api:             api,
		socketClient:    socketClient,
		allowedChannels: makeAllowSet(cfg.AllowedChannelIDs),
		allowedUsers:    makeAllowSet(cfg.AllowedUserIDs),
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
	outbound, err := b.orchestrator.HandleMessage(ctx, inbound)
	if err != nil {
		b.orchestrator.FinalizeAudit(inbound, err)
		return err
	}

	if strings.TrimSpace(outbound.Message) == "" {
		return fmt.Errorf("empty outbound message")
	}
	replyThreadTimestamp := threadTimestamp
	if strings.TrimSpace(replyThreadTimestamp) == "" && channelType != "im" {
		replyThreadTimestamp = timestamp
	}
	if err := b.sendMessage(ctx, channelID, outbound.Message, replyThreadTimestamp); err != nil {
		b.orchestrator.FinalizeAudit(inbound, err)
		return err
	}

	b.orchestrator.FinalizeAudit(inbound, nil)
	return nil
}

func (b *Bot) sendMessage(ctx context.Context, channelID, text, threadTimestamp string) error {
	options := []slack.MsgOption{slack.MsgOptionText(text, false)}
	if strings.TrimSpace(threadTimestamp) != "" {
		options = append(options, slack.MsgOptionPostMessageParameters(slack.PostMessageParameters{
			ThreadTimestamp: threadTimestamp,
		}))
	}

	_, _, err := b.api.PostMessageContext(ctx, channelID, options...)
	return err
}

func (b *Bot) isAllowed(channelID, userID string) bool {
	if len(b.allowedChannels) > 0 {
		if _, ok := b.allowedChannels[channelID]; !ok {
			return false
		}
	}
	if len(b.allowedUsers) > 0 {
		if _, ok := b.allowedUsers[userID]; !ok {
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
