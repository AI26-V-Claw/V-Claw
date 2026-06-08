package slack

import (
	"context"
	"encoding/json"
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
	state           *slackChannelState
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
		state:           newSlackChannelState(),
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
	case socketmode.EventTypeInteractive:
		if event.Request != nil {
			b.socketClient.Ack(*event.Request)
		}
		callback, ok := event.Data.(slack.InteractionCallback)
		if !ok {
			return nil
		}
		return b.handleSlackInteraction(ctx, callback)
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
	if outbound.Status == contracts.AgentStatusApprovalRequired && outbound.ApprovalRequest != nil {
		if err := progress.UpdateApproval(ctx, outboundText, outbound.ApprovalID, inbound.SessionID, outbound.ApprovalRequest.ToolCall.ToolName); err != nil {
			b.logger.Error("slack final approval update failed", "error", err)
			if _, sendErr := b.sendApprovalMessage(ctx, channelID, outboundText, replyThreadTimestamp, outbound.ApprovalID, inbound.SessionID, outbound.ApprovalRequest.ToolCall.ToolName); sendErr != nil {
				b.orchestrator.FinalizeAudit(inbound, sendErr)
				return sendErr
			}
		}
		b.orchestrator.FinalizeAudit(inbound, nil)
		return nil
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

func (b *Bot) handleSlackInteraction(ctx context.Context, callback slack.InteractionCallback) error {
	if callback.Type == slack.InteractionTypeViewSubmission && callback.View.CallbackID == slackApprovalReviseCallbackID {
		return b.handleSlackReviseSubmission(ctx, callback)
	}
	if callback.Type != slack.InteractionTypeBlockActions {
		return nil
	}
	if len(callback.ActionCallback.BlockActions) == 0 {
		return nil
	}
	action := callback.ActionCallback.BlockActions[0]
	approvalAction, approvalID, sessionID, ok := parseSlackApprovalValue(action.Value)
	if !ok {
		return nil
	}
	channelID := callback.Container.ChannelID
	if strings.TrimSpace(channelID) == "" {
		channelID = callback.Channel.ID
	}
	messageTS := callback.Container.MessageTs
	if strings.TrimSpace(messageTS) == "" {
		messageTS = callback.MessageTs
	}
	approvalContext, ok := b.state.lookupApproval(approvalID, sessionID, channelID, messageTS)
	if !ok {
		if b.api != nil {
			_ = b.updateMessageClearBlocks(ctx, channelID, messageTS, "Yêu cầu xác nhận này không còn hợp lệ.")
		}
		return nil
	}
	if !b.isAllowed(channelID, callback.User.ID) {
		b.orchestrator.RecordIgnored(contracts.UserMessage{
			RequestID: "slack_interaction_" + normalizeSlackTimestamp(callback.ActionTs),
			SessionID: approvalContext.SessionID,
			Channel:   "slack",
			Text:      approvalAction,
			Timestamp: time.Now().UTC(),
		}, "ignored_unauthorized_interaction")
		return nil
	}
	if approvalAction == "revise" {
		return b.openSlackReviseModal(ctx, callback.TriggerID, slackApprovalMetadata{
			ApprovalID: approvalContext.ApprovalID,
			SessionID:  approvalContext.SessionID,
			ChannelID:  channelID,
			MessageTS:  messageTS,
		})
	}
	command := approvalAction
	if strings.TrimSpace(approvalContext.ApprovalID) != "" {
		command += " " + approvalContext.ApprovalID
	}
	inbound := contracts.UserMessage{
		RequestID: "slack_interaction_" + normalizeSlackTimestamp(callback.ActionTs),
		SessionID: approvalContext.SessionID,
		Channel:   "slack",
		Text:      command,
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"slack_channel_id": channelID,
			"slack_user_id":    callback.User.ID,
			"slack_action":     approvalAction,
			"approvalId":       approvalContext.ApprovalID,
			"source":           "slack",
		},
	}
	if err := b.updateMessageClearBlocks(ctx, channelID, messageTS, "Đang xử lý quyết định..."); err != nil {
		b.logger.Error("slack approval progress update failed", "error", err)
	}
	outbound, err := b.orchestrator.HandleMessage(ctx, inbound)
	if err != nil {
		b.orchestrator.FinalizeAudit(inbound, err)
		b.logger.Error("agent approval handler failed", "request_id", inbound.RequestID, "session_id", inbound.SessionID, "error", err)
		_ = b.updateMessageClearBlocks(ctx, channelID, messageTS, slackGenericErrorText())
		return nil
	}
	outboundText := slackTextFromResponse(outbound)
	if strings.TrimSpace(outboundText) == "" {
		outboundText = "Đã xử lý quyết định."
	}
	// Continuation after approval may itself require approval (next task in a
	// multi-step request). Show the approval blocks so the user can act on it.
	if outbound.Status == contracts.AgentStatusApprovalRequired && outbound.ApprovalRequest != nil {
		if err := b.updateApprovalMessage(ctx, channelID, messageTS, outboundText, outbound.ApprovalID, inbound.SessionID, outbound.ApprovalRequest.ToolCall.ToolName); err != nil {
			b.logger.Error("slack continuation approval update failed", "error", err)
			if _, sendErr := b.sendApprovalMessage(ctx, channelID, outboundText, "", outbound.ApprovalID, inbound.SessionID, outbound.ApprovalRequest.ToolCall.ToolName); sendErr != nil {
				b.orchestrator.FinalizeAudit(inbound, sendErr)
				return sendErr
			}
		}
		b.orchestrator.FinalizeAudit(inbound, nil)
		return nil
	}
	b.state.deleteApproval(approvalContext.ApprovalID)
	if err := b.updateMessageClearBlocks(ctx, channelID, messageTS, outboundText); err != nil {
		b.orchestrator.FinalizeAudit(inbound, err)
		return err
	}
	b.orchestrator.FinalizeAudit(inbound, nil)
	return nil
}

func (b *Bot) handleSlackReviseSubmission(ctx context.Context, callback slack.InteractionCallback) error {
	meta, err := parseSlackApprovalMetadata(callback.View.PrivateMetadata)
	if err != nil {
		return err
	}
	approvalContext, ok := b.state.lookupApproval(meta.ApprovalID, meta.SessionID, meta.ChannelID, meta.MessageTS)
	if !ok {
		if b.api != nil {
			_ = b.updateMessageClearBlocks(ctx, meta.ChannelID, meta.MessageTS, "Yêu cầu xác nhận này không còn hợp lệ.")
		}
		return nil
	}
	comment := slackReviseComment(callback)
	if strings.TrimSpace(comment) == "" {
		comment = "Tôi muốn chỉnh lại yêu cầu."
	}
	inbound := contracts.UserMessage{
		RequestID: "slack_view_" + normalizeSlackTimestamp(callback.ActionTs),
		SessionID: meta.SessionID,
		Channel:   "slack",
		Text:      "revise " + comment,
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"slack_channel_id": meta.ChannelID,
			"slack_user_id":    callback.User.ID,
			"slack_action":     "revise",
			"approvalId":       approvalContext.ApprovalID,
			"source":           "slack",
		},
	}
	outbound, err := b.orchestrator.HandleMessage(ctx, inbound)
	if err != nil {
		b.orchestrator.FinalizeAudit(inbound, err)
		b.logger.Error("agent revise handler failed", "request_id", inbound.RequestID, "session_id", inbound.SessionID, "error", err)
		_ = b.updateMessageClearBlocks(ctx, meta.ChannelID, meta.MessageTS, slackGenericErrorText())
		return nil
	}
	outboundText := slackTextFromResponse(outbound)
	if strings.TrimSpace(outboundText) == "" {
		outboundText = "Đã ghi nhận phần chỉnh sửa."
	}
	if outbound.Status == contracts.AgentStatusApprovalRequired && outbound.ApprovalRequest != nil {
		if err := b.updateApprovalMessage(ctx, meta.ChannelID, meta.MessageTS, outboundText, outbound.ApprovalID, inbound.SessionID, outbound.ApprovalRequest.ToolCall.ToolName); err != nil {
			b.orchestrator.FinalizeAudit(inbound, err)
			return err
		}
		b.orchestrator.FinalizeAudit(inbound, nil)
		return nil
	}
	b.state.deleteApproval(approvalContext.ApprovalID)
	if err := b.updateMessageClearBlocks(ctx, meta.ChannelID, meta.MessageTS, outboundText); err != nil {
		b.orchestrator.FinalizeAudit(inbound, err)
		return err
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

func (b *Bot) sendApprovalMessage(ctx context.Context, channelID, text, threadTimestamp, approvalID, sessionID, toolName string) (string, error) {
	options := []slack.MsgOption{
		slack.MsgOptionText(text, false),
		slack.MsgOptionBlocks(slackApprovalBlocks(text, approvalID, sessionID)...),
	}
	if strings.TrimSpace(threadTimestamp) != "" {
		options = append(options, slack.MsgOptionPostMessageParameters(slack.PostMessageParameters{
			ThreadTimestamp: threadTimestamp,
		}))
	}
	_, timestamp, err := b.api.PostMessageContext(ctx, channelID, options...)
	if err == nil {
		b.state.registerApproval(slackApprovalContext{
			ApprovalID: approvalID,
			SessionID:  sessionID,
			ChannelID:  channelID,
			MessageTS:  timestamp,
			PromptText: text,
			ToolName:   toolName,
		})
	}
	return timestamp, err
}

func (b *Bot) updateMessage(ctx context.Context, channelID, timestamp, text string) error {
	_, _, _, err := b.api.UpdateMessageContext(ctx, channelID, timestamp, slack.MsgOptionText(text, false))
	return err
}

func (b *Bot) updateApprovalMessage(ctx context.Context, channelID, timestamp, text, approvalID, sessionID, toolName string) error {
	_, _, _, err := b.api.UpdateMessageContext(ctx, channelID, timestamp,
		slack.MsgOptionText(text, false),
		slack.MsgOptionBlocks(slackApprovalBlocks(text, approvalID, sessionID)...),
	)
	if err == nil {
		b.state.registerApproval(slackApprovalContext{
			ApprovalID: approvalID,
			SessionID:  sessionID,
			ChannelID:  channelID,
			MessageTS:  timestamp,
			PromptText: text,
			ToolName:   toolName,
		})
	}
	return err
}

func (b *Bot) updateMessageClearBlocks(ctx context.Context, channelID, timestamp, text string) error {
	_, _, _, err := b.api.UpdateMessageContext(ctx, channelID, timestamp,
		slack.MsgOptionText(text, false),
		slack.MsgOptionBlocks(),
	)
	return err
}

func (b *Bot) openSlackReviseModal(ctx context.Context, triggerID string, metadata slackApprovalMetadata) error {
	encoded, err := metadata.encode()
	if err != nil {
		return err
	}
	approvalContext, _ := b.state.lookupApproval(metadata.ApprovalID, metadata.SessionID, metadata.ChannelID, metadata.MessageTS)
	placeholder := slack.NewTextBlockObject(slack.PlainTextType, "Ví dụ: đổi giờ họp sang 10:00", false, false)
	input := slack.NewPlainTextInputBlockElement(placeholder, slackApprovalReviseInputActionID).WithMultiline(true).WithMaxLength(1000)
	blocks := []slack.Block{}
	if prompt := strings.TrimSpace(slackRevisionPrompt(approvalContext)); prompt != "" {
		blocks = append(blocks, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, slackMrkdwn(prompt), false, false), nil, nil))
	}
	blocks = append(blocks,
		slack.NewInputBlock(
			slackApprovalReviseInputBlockID,
			slack.NewTextBlockObject(slack.PlainTextType, "Bạn muốn chỉnh gì?", false, false),
			nil,
			input,
		),
	)
	view := slack.ModalViewRequest{
		Type:            slack.VTModal,
		Title:           slack.NewTextBlockObject(slack.PlainTextType, "Chỉnh sửa yêu cầu", false, false),
		Close:           slack.NewTextBlockObject(slack.PlainTextType, "Đóng", false, false),
		Submit:          slack.NewTextBlockObject(slack.PlainTextType, "Gửi", false, false),
		CallbackID:      slackApprovalReviseCallbackID,
		PrivateMetadata: encoded,
		Blocks:          slack.Blocks{BlockSet: blocks},
	}
	_, err = b.api.OpenViewContext(ctx, triggerID, view)
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
	if err := e.bot.updateMessage(ctx, e.channelID, e.timestamp, text); err != nil {
		return err
	}
	e.lastText = text
	return nil
}

func (e *slackProgressEditor) UpdateApproval(ctx context.Context, text, approvalID, sessionID, toolName string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if err := e.bot.updateApprovalMessage(ctx, e.channelID, e.timestamp, text, approvalID, sessionID, toolName); err != nil {
		return err
	}
	e.lastText = text
	return nil
}

func slackProgressText(event agent.ProgressEvent) string {
	switch event.Stage {
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
	case agent.ProgressStageToolCompleted, agent.ProgressStageToolFailed:
		return "Đang xử lý..."
	default:
		return ""
	}
}

func slackGenericErrorText() string {
	return "Mình chưa thể hoàn tất yêu cầu này. Chi tiết lỗi đã được ghi ở terminal local."
}

const (
	slackApprovalApproveActionID     = "vclaw_approval_yes"
	slackApprovalRejectActionID      = "vclaw_approval_no"
	slackApprovalReviseActionID      = "vclaw_approval_revise"
	slackApprovalReviseCallbackID    = "vclaw_approval_revise_modal"
	slackApprovalReviseInputBlockID  = "vclaw_approval_comment"
	slackApprovalReviseInputActionID = "comment"
)

type slackApprovalPayload struct {
	Action     string `json:"action"`
	ApprovalID string `json:"approvalId"`
	SessionID  string `json:"sessionId"`
}

type slackApprovalMetadata struct {
	ApprovalID string `json:"approvalId"`
	SessionID  string `json:"sessionId"`
	ChannelID  string `json:"channelId"`
	MessageTS  string `json:"messageTs"`
}

func parseSlackApprovalValue(value string) (action, approvalID, sessionID string, ok bool) {
	var payload slackApprovalPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(value)), &payload); err != nil {
		return "", "", "", false
	}
	action = strings.TrimSpace(payload.Action)
	if action != "approve" && action != "reject" && action != "revise" {
		return "", "", "", false
	}
	return action, strings.TrimSpace(payload.ApprovalID), strings.TrimSpace(payload.SessionID), true
}

func (m slackApprovalMetadata) encode() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func parseSlackApprovalMetadata(value string) (slackApprovalMetadata, error) {
	var meta slackApprovalMetadata
	if err := json.Unmarshal([]byte(strings.TrimSpace(value)), &meta); err != nil {
		return slackApprovalMetadata{}, err
	}
	if strings.TrimSpace(meta.SessionID) == "" || strings.TrimSpace(meta.ChannelID) == "" || strings.TrimSpace(meta.MessageTS) == "" {
		return slackApprovalMetadata{}, fmt.Errorf("missing slack approval metadata")
	}
	return meta, nil
}

func slackReviseComment(callback slack.InteractionCallback) string {
	if callback.View.State == nil {
		return ""
	}
	block, ok := callback.View.State.Values[slackApprovalReviseInputBlockID]
	if !ok {
		return ""
	}
	action, ok := block[slackApprovalReviseInputActionID]
	if !ok {
		return ""
	}
	return strings.TrimSpace(action.Value)
}

func slackMrkdwn(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	if text == "" {
		return "Approval required."
	}
	return text
}
