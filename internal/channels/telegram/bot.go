package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"vclaw/internal/agent"
	"vclaw/internal/channels/formatting"
	"vclaw/internal/contracts"
)

const longPollTimeout = 30

type Bot struct {
	token         string
	allowedUserID int64
	dataDir       string
	offsetPath    string
	client        *http.Client
	handler       messageHandler
	logger        *slog.Logger
	apiBase       string
	state         *telegramChannelState
}

type messageHandler interface {
	HandleMessage(ctx context.Context, msg contracts.UserMessage) (contracts.AgentResponse, error)
	FinalizeAudit(msg contracts.UserMessage, err error)
	RecordIgnored(msg contracts.UserMessage, actionTaken string)
}

func New(token string, allowedUserID int64, dataDir string, handler messageHandler, logger *slog.Logger) *Bot {
	if logger == nil {
		logger = slog.Default()
	}
	return &Bot{
		token:         token,
		allowedUserID: allowedUserID,
		dataDir:       dataDir,
		offsetPath:    filepath.Join(dataDir, "telegram_offset.txt"),
		client: &http.Client{
			Timeout: 65 * time.Second,
		},
		handler: handler,
		logger:  logger,
		apiBase: "https://api.telegram.org",
		state:   newTelegramChannelState(),
	}
}

func (b *Bot) Run(ctx context.Context) error {
	if err := os.MkdirAll(b.dataDir, 0o755); err != nil {
		return err
	}

	if err := b.deleteWebhook(ctx); err != nil {
		return err
	}

	me, err := b.getMe(ctx)
	if err != nil {
		return err
	}
	b.logger.Info("telegram bot ready", "username", me.Username, "bot_id", me.ID)

	offset := b.readOffset()
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		updates, err := b.getUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			b.logger.Error("telegram polling failed", "error", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, update := range updates {
			processed, err := b.processUpdate(ctx, update)
			if err != nil {
				b.logger.Error("telegram update failed", "update_id", update.UpdateID, "error", err)
				continue
			}
			if processed {
				offset = int64(update.UpdateID) + 1
				if err := b.writeOffset(offset); err != nil {
					b.logger.Error("failed to persist offset", "offset", offset, "error", err)
				}
			}
		}
	}
}

func (b *Bot) processUpdate(ctx context.Context, update telegramUpdate) (bool, error) {
	if update.CallbackQuery != nil {
		return b.processCallbackQuery(ctx, update)
	}
	if update.Message == nil {
		return true, nil
	}
	messageText := strings.TrimSpace(update.Message.Text)
	if messageText == "" {
		messageText = strings.TrimSpace(update.Message.Caption)
	}
	inbound := contracts.UserMessage{
		RequestID: fmt.Sprintf("telegram_update_%d", update.UpdateID),
		SessionID: fmt.Sprintf("telegram_chat_%d", update.Message.Chat.ID),
		Channel:   "telegram",
		Text:      messageText,
		Locale:    "",
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"telegramUpdateId": update.UpdateID,
			"telegramChatId":   update.Message.Chat.ID,
			"source":           "telegram",
		},
	}

	if update.Message.From == nil || update.Message.From.ID != b.allowedUserID {
		if b.handler != nil {
			b.handler.RecordIgnored(inbound, "ignored_unauthorized")
		}
		return true, nil
	}
	attachments, err := b.downloadMessageAttachments(ctx, update.Message)
	if err != nil {
		if b.handler != nil {
			b.handler.FinalizeAudit(inbound, err)
		}
		return false, err
	}
	if len(attachments) > 0 {
		paths := make([]string, 0, len(attachments))
		metadata := make([]map[string]any, 0, len(attachments))
		for _, attachment := range attachments {
			paths = append(paths, attachment.Path)
			metadata = append(metadata, map[string]any{
				"path":     attachment.Path,
				"filename": attachment.Filename,
				"mimeType": attachment.MimeType,
				"source":   "telegram",
			})
		}
		inbound.Metadata["attachmentPaths"] = paths
		inbound.Metadata["attachments"] = metadata
		if strings.TrimSpace(inbound.Text) == "" {
			inbound.Text = "User sent an attachment."
		}
	}
	if strings.TrimSpace(inbound.Text) == "" {
		if b.handler != nil {
			b.handler.RecordIgnored(inbound, "ignored_non_text")
		}
		return true, nil
	}
	if approvalContext, ok := b.state.approvalForChat(update.Message.Chat.ID); ok {
		if err := b.dismissApprovalKeyboard(ctx, approvalContext); err != nil {
			b.logger.Error("telegram approval keyboard dismiss failed", "chat_id", approvalContext.ChatID, "message_id", approvalContext.MessageID, "error", err)
		}
		b.state.deleteApproval(approvalContext.ApprovalID)
		inbound.SessionID = approvalContext.SessionID
		inbound.Metadata["approvalId"] = approvalContext.ApprovalID
		action := telegramApprovalTextAction(inbound.Text)
		switch action {
		case "approve", "reject":
			inbound.Text = action + " " + approvalContext.ApprovalID
			inbound.Metadata["telegramCallback"] = action
		default:
			inbound.Text = "revise " + strings.TrimSpace(inbound.Text)
			inbound.Metadata["telegramCallback"] = "revise"
		}
	} else if revision, ok := b.state.consumeRevision(update.Message.Chat.ID); ok {
		inbound.SessionID = revision.SessionID
		inbound.Text = "revise " + strings.TrimSpace(inbound.Text)
		inbound.Metadata["telegramCallback"] = "revise"
		inbound.Metadata["approvalId"] = revision.ApprovalID
	}
	if b.handler == nil {
		return false, fmt.Errorf("message handler is not configured")
	}

	processingMessage, err := b.sendMessage(ctx, update.Message.Chat.ID, "Đang xử lý...")
	if err != nil {
		b.handler.FinalizeAudit(inbound, err)
		return false, err
	}
	progress := newTelegramProgressEditor(b, update.Message.Chat.ID, processingMessage.MessageID)

	followUpCtx := agent.WithFollowUpSink(ctx, func(followCtx context.Context, message agent.FollowUpMessage) {
		if strings.TrimSpace(message.Text) == "" {
			return
		}
		if _, err := b.sendMessage(followCtx, update.Message.Chat.ID, message.Text); err != nil {
			b.logger.Error("telegram follow-up send failed", "error", err)
		}
	})
	progressCtx := agent.WithProgressSink(followUpCtx, func(progressCtx context.Context, event agent.ProgressEvent) {
		text := telegramProgressText(event)
		if strings.TrimSpace(text) == "" {
			return
		}
		if err := progress.Update(progressCtx, text); err != nil {
			b.logger.Error("telegram progress edit failed", "error", err)
		}
	})

	outbound, err := b.handler.HandleMessage(progressCtx, inbound)
	if err != nil {
		b.handler.FinalizeAudit(inbound, err)
		b.logger.Error("agent handler failed", "request_id", inbound.RequestID, "session_id", inbound.SessionID, "error", err)
		_ = progress.Update(ctx, telegramGenericErrorText())
		return true, nil
	}

	text := telegramTextFromResponse(outbound)
	if strings.TrimSpace(text) == "" {
		err := fmt.Errorf("empty outbound message")
		b.handler.FinalizeAudit(inbound, err)
		return false, err
	}
	if strings.EqualFold(string(outbound.Status), "failed") {
		b.logger.Error("agent response error", "request_id", outbound.RequestID, "session_id", outbound.SessionID, "status", outbound.Status, "message", outbound.Message)
	}

	if outbound.Status == contracts.AgentStatusApprovalRequired && outbound.ApprovalRequest != nil {
		b.state.registerApproval(telegramApprovalContext{
			ApprovalID: outbound.ApprovalID,
			SessionID:  outbound.SessionID,
			ChatID:     update.Message.Chat.ID,
			MessageID:  processingMessage.MessageID,
			PromptText: text,
			ToolName:   outbound.ApprovalRequest.ToolCall.ToolName,
		})
		if err := progress.UpdateWithReplyMarkup(ctx, text, telegramApprovalKeyboard(outbound.ApprovalID)); err != nil {
			b.logger.Error("telegram final approval edit failed", "error", err)
			sentMessage, sendErr := b.sendMessageWithReplyMarkup(ctx, update.Message.Chat.ID, text, telegramApprovalKeyboard(outbound.ApprovalID))
			if sendErr != nil {
				b.handler.FinalizeAudit(inbound, sendErr)
				return false, sendErr
			}
			b.state.registerApproval(telegramApprovalContext{
				ApprovalID: outbound.ApprovalID,
				SessionID:  outbound.SessionID,
				ChatID:     update.Message.Chat.ID,
				MessageID:  sentMessage.MessageID,
				PromptText: text,
				ToolName:   outbound.ApprovalRequest.ToolCall.ToolName,
			})
		}
		b.handler.FinalizeAudit(inbound, nil)
		return true, nil
	}

	if err := progress.Update(ctx, text); err != nil {
		b.logger.Error("telegram final edit failed", "error", err)
		if _, sendErr := b.sendMessage(ctx, update.Message.Chat.ID, text); sendErr != nil {
			b.handler.FinalizeAudit(inbound, sendErr)
			return false, sendErr
		}
	}

	b.handler.FinalizeAudit(inbound, nil)
	return true, nil
}

func (b *Bot) processCallbackQuery(ctx context.Context, update telegramUpdate) (bool, error) {
	callback := update.CallbackQuery
	if callback.From == nil || callback.From.ID != b.allowedUserID {
		if b.handler != nil {
			b.handler.RecordIgnored(contracts.UserMessage{
				RequestID: fmt.Sprintf("telegram_callback_%s", safeTelegramID(callback.ID)),
				SessionID: "telegram_callback",
				Channel:   "telegram",
				Text:      callback.Data,
				Timestamp: time.Now().UTC(),
			}, "ignored_unauthorized_callback")
		}
		_ = b.answerCallbackQuery(ctx, callback.ID, "Not allowed.")
		return true, nil
	}
	if callback.Message == nil {
		_ = b.answerCallbackQuery(ctx, callback.ID, "Missing message context.")
		return true, nil
	}
	action, approvalID, ok := parseTelegramApprovalCallback(callback.Data)
	if !ok {
		_ = b.answerCallbackQuery(ctx, callback.ID, "Unknown action.")
		return true, nil
	}
	approvalContext, ok := b.state.lookupApproval(approvalID, callback.Message.Chat.ID, callback.Message.MessageID)
	if !ok {
		if err := b.editMessageReplyMarkup(ctx, callback.Message.Chat.ID, callback.Message.MessageID, map[string]any{
			"inline_keyboard": [][]map[string]string{},
		}); err != nil {
			b.logger.Error("telegram stale approval keyboard dismiss failed", "chat_id", callback.Message.Chat.ID, "message_id", callback.Message.MessageID, "error", err)
		}
		_ = b.answerCallbackQuery(ctx, callback.ID, "Yêu cầu xác nhận này không còn hợp lệ.")
		return true, nil
	}
	if err := b.dismissApprovalKeyboard(ctx, approvalContext); err != nil {
		b.logger.Error("telegram approval keyboard dismiss failed", "chat_id", approvalContext.ChatID, "message_id", approvalContext.MessageID, "error", err)
	}
	if action == "revise" {
		b.state.rememberRevision(approvalContext)
		_ = b.answerCallbackQuery(ctx, callback.ID, "Hãy gửi nội dung bạn muốn chỉnh.")
		text := telegramRevisionPrompt(approvalContext)
		if _, err := b.sendMessage(ctx, callback.Message.Chat.ID, text); err != nil {
			return false, err
		}
		return true, nil
	}

	command := action
	if approvalContext.ApprovalID != "" {
		command += " " + approvalContext.ApprovalID
	}
	inbound := contracts.UserMessage{
		RequestID: fmt.Sprintf("telegram_callback_%s", safeTelegramID(callback.ID)),
		SessionID: approvalContext.SessionID,
		Channel:   "telegram",
		Text:      command,
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"telegramUpdateId": update.UpdateID,
			"telegramChatId":   callback.Message.Chat.ID,
			"telegramCallback": action,
			"approvalId":       approvalContext.ApprovalID,
			"source":           "telegram",
		},
	}
	if b.handler == nil {
		return false, fmt.Errorf("message handler is not configured")
	}
	b.state.deleteApproval(approvalContext.ApprovalID)
	_ = b.answerCallbackQuery(ctx, callback.ID, "Đang xử lý...")
	processingMessage, sendErr := b.sendMessage(ctx, callback.Message.Chat.ID, "Đang xử lý...")
	if sendErr != nil {
		b.handler.FinalizeAudit(inbound, sendErr)
		return false, sendErr
	}
	progress := newTelegramProgressEditor(b, callback.Message.Chat.ID, processingMessage.MessageID)
	followUpCtx := agent.WithFollowUpSink(ctx, func(followCtx context.Context, message agent.FollowUpMessage) {
		if strings.TrimSpace(message.Text) == "" {
			return
		}
		if _, err := b.sendMessage(followCtx, callback.Message.Chat.ID, message.Text); err != nil {
			b.logger.Error("telegram follow-up send failed", "error", err)
		}
	})
	outbound, err := b.handler.HandleMessage(followUpCtx, inbound)
	if err != nil {
		b.handler.FinalizeAudit(inbound, err)
		b.logger.Error("agent approval handler failed", "request_id", inbound.RequestID, "session_id", inbound.SessionID, "error", err)
		_ = progress.Update(ctx, telegramGenericErrorText())
		return true, nil
	}
	text := telegramTextFromResponse(outbound)
	if strings.TrimSpace(text) == "" {
		text = "Đã xử lý quyết định."
	}
	// Continuation after approval may itself require approval (e.g. next task in a
	// multi-step request). Show the inline keyboard so the user can act on it.
	if outbound.Status == contracts.AgentStatusApprovalRequired && outbound.ApprovalRequest != nil {
		b.state.registerApproval(telegramApprovalContext{
			ApprovalID: outbound.ApprovalID,
			SessionID:  outbound.SessionID,
			ChatID:     callback.Message.Chat.ID,
			MessageID:  processingMessage.MessageID,
			PromptText: text,
			ToolName:   outbound.ApprovalRequest.ToolCall.ToolName,
		})
		if err := progress.UpdateWithReplyMarkup(ctx, text, telegramApprovalKeyboard(outbound.ApprovalID)); err != nil {
			b.logger.Error("telegram continuation approval edit failed", "error", err)
			sentMessage, sendErr := b.sendMessageWithReplyMarkup(ctx, callback.Message.Chat.ID, text, telegramApprovalKeyboard(outbound.ApprovalID))
			if sendErr != nil {
				b.handler.FinalizeAudit(inbound, sendErr)
				return false, sendErr
			}
			b.state.registerApproval(telegramApprovalContext{
				ApprovalID: outbound.ApprovalID,
				SessionID:  outbound.SessionID,
				ChatID:     callback.Message.Chat.ID,
				MessageID:  sentMessage.MessageID,
				PromptText: text,
				ToolName:   outbound.ApprovalRequest.ToolCall.ToolName,
			})
		}
		b.handler.FinalizeAudit(inbound, nil)
		return true, nil
	}
	b.state.deleteApproval(approvalContext.ApprovalID)
	if err := progress.Update(ctx, text); err != nil {
		b.handler.FinalizeAudit(inbound, err)
		return false, err
	}
	b.handler.FinalizeAudit(inbound, nil)
	return true, nil
}

func (b *Bot) deleteWebhook(ctx context.Context) error {
	var response struct {
		OK bool `json:"ok"`
	}
	_, err := b.doJSON(ctx, http.MethodPost, "/deleteWebhook", nil, &response)
	if err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("telegram deleteWebhook returned not ok")
	}
	return nil
}

func (b *Bot) getMe(ctx context.Context) (telegramUser, error) {
	var response struct {
		OK     bool         `json:"ok"`
		Result telegramUser `json:"result"`
	}
	_, err := b.doJSON(ctx, http.MethodGet, "/getMe", nil, &response)
	if err != nil {
		return telegramUser{}, err
	}
	if !response.OK {
		return telegramUser{}, fmt.Errorf("telegram getMe returned not ok")
	}
	return response.Result, nil
}

func (b *Bot) getUpdates(ctx context.Context, offset int64) ([]telegramUpdate, error) {
	query := url.Values{}
	if offset > 0 {
		query.Set("offset", strconv.FormatInt(offset, 10))
	}
	query.Set("timeout", strconv.Itoa(longPollTimeout))

	var response struct {
		OK     bool             `json:"ok"`
		Result []telegramUpdate `json:"result"`
	}
	_, err := b.doJSON(ctx, http.MethodGet, "/getUpdates?"+query.Encode(), nil, &response)
	if err != nil {
		return nil, err
	}
	if !response.OK {
		return nil, fmt.Errorf("telegram getUpdates returned not ok")
	}
	return response.Result, nil
}

func (b *Bot) sendMessage(ctx context.Context, chatID int64, text string) (telegramSentMessage, error) {
	payload := map[string]any{
		"chat_id": chatID,
	}
	applyTelegramFormattedText(payload, text)
	return b.sendMessagePayload(ctx, payload)
}

func (b *Bot) sendMessageWithReplyMarkup(ctx context.Context, chatID int64, text string, replyMarkup any) (telegramSentMessage, error) {
	payload := map[string]any{
		"chat_id": chatID,
	}
	applyTelegramFormattedText(payload, text)
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	return b.sendMessagePayload(ctx, payload)
}

func (b *Bot) sendMessagePayload(ctx context.Context, payload map[string]any) (telegramSentMessage, error) {
	var response struct {
		OK     bool                `json:"ok"`
		Result telegramSentMessage `json:"result"`
	}
	_, err := b.doJSON(ctx, http.MethodPost, "/sendMessage", payload, &response)
	if err != nil {
		return telegramSentMessage{}, err
	}
	if !response.OK {
		return telegramSentMessage{}, fmt.Errorf("telegram sendMessage returned not ok")
	}
	return response.Result, nil
}

func (b *Bot) editMessageText(ctx context.Context, chatID int64, messageID int, text string) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
	}
	applyTelegramFormattedText(payload, text)
	return b.editMessageTextPayload(ctx, payload)
}

func (b *Bot) editMessageTextWithReplyMarkup(ctx context.Context, chatID int64, messageID int, text string, replyMarkup any) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
	}
	applyTelegramFormattedText(payload, text)
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	return b.editMessageTextPayload(ctx, payload)
}

func (b *Bot) editMessageReplyMarkup(ctx context.Context, chatID int64, messageID int, replyMarkup any) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
	}
	payload["reply_markup"] = replyMarkup
	var response struct {
		OK bool `json:"ok"`
	}
	_, err := b.doJSON(ctx, http.MethodPost, "/editMessageReplyMarkup", payload, &response)
	if err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("telegram editMessageReplyMarkup returned not ok")
	}
	return nil
}

func (b *Bot) dismissApprovalKeyboard(ctx context.Context, approvalContext telegramApprovalContext) error {
	if approvalContext.ChatID == 0 || approvalContext.MessageID == 0 {
		return nil
	}
	return b.editMessageReplyMarkup(ctx, approvalContext.ChatID, approvalContext.MessageID, map[string]any{
		"inline_keyboard": [][]map[string]string{},
	})
}

func applyTelegramFormattedText(payload map[string]any, text string) {
	payload["text"] = telegramRenderHTML(text)
	payload["parse_mode"] = "HTML"
}

func telegramRenderHTML(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	text = formatting.NormalizeLineEndings(text)
	text = formatting.ReplaceFencedCodeBlocks(text, telegramCodeBlock)

	var builder strings.Builder
	for {
		codeStart := strings.Index(text, telegramCodeBlockOpen)
		preStart := strings.Index(text, telegramPreBlockOpen)
		fieldStart := strings.Index(text, telegramFieldOpen)
		textFieldStart := strings.Index(text, telegramTextFieldOpen)
		start := -1
		blockType := ""
		switch {
		case codeStart >= 0 &&
			(preStart < 0 || codeStart < preStart) &&
			(fieldStart < 0 || codeStart < fieldStart) &&
			(textFieldStart < 0 || codeStart < textFieldStart):
			start = codeStart
			blockType = "code"
		case preStart >= 0 &&
			(fieldStart < 0 || preStart < fieldStart) &&
			(textFieldStart < 0 || preStart < textFieldStart):
			start = preStart
			blockType = "pre"
		case fieldStart >= 0 && (textFieldStart < 0 || fieldStart < textFieldStart):
			start = fieldStart
			blockType = "field"
		case textFieldStart >= 0:
			start = textFieldStart
			blockType = "textField"
		}
		if start < 0 {
			builder.WriteString(telegramRenderPlainHTML(text))
			break
		}
		builder.WriteString(telegramRenderPlainHTML(text[:start]))
		if blockType == "pre" {
			remaining := text[start+len(telegramPreBlockOpen):]
			end := strings.Index(remaining, telegramPreBlockClose)
			if end < 0 {
				builder.WriteString(telegramRenderPlainHTML(text[start:]))
				break
			}
			builder.WriteString("<blockquote>")
			builder.WriteString(telegramRenderPlainHTML(remaining[:end]))
			builder.WriteString("</blockquote>")
			text = remaining[end+len(telegramPreBlockClose):]
			continue
		}
		if blockType == "field" {
			remaining := text[start+len(telegramFieldOpen):]
			end := strings.Index(remaining, telegramFieldClose)
			if end < 0 {
				builder.WriteString(telegramRenderPlainHTML(text[start:]))
				break
			}
			field := remaining[:end]
			label := field
			value := ""
			if separator := strings.Index(field, telegramFieldSeparator); separator >= 0 {
				label = strings.TrimSpace(field[:separator])
				value = strings.TrimSpace(field[separator+len(telegramFieldSeparator):])
			}
			builder.WriteString("<b>")
			builder.WriteString(html.EscapeString(label))
			builder.WriteString("</b>")
			if value != "" {
				builder.WriteString(" <code>")
				builder.WriteString(html.EscapeString(value))
				builder.WriteString("</code>")
			}
			text = remaining[end+len(telegramFieldClose):]
			continue
		}
		if blockType == "textField" {
			remaining := text[start+len(telegramTextFieldOpen):]
			end := strings.Index(remaining, telegramTextFieldClose)
			if end < 0 {
				builder.WriteString(telegramRenderPlainHTML(text[start:]))
				break
			}
			field := remaining[:end]
			label := field
			value := ""
			if separator := strings.Index(field, telegramFieldSeparator); separator >= 0 {
				label = strings.TrimSpace(field[:separator])
				value = strings.TrimSpace(field[separator+len(telegramFieldSeparator):])
			}
			builder.WriteString("<b>")
			builder.WriteString(html.EscapeString(label))
			builder.WriteString("</b>")
			if value != "" {
				builder.WriteString(" ")
				builder.WriteString(html.EscapeString(value))
			}
			text = remaining[end+len(telegramTextFieldClose):]
			continue
		}
		remaining := text[start+len(telegramCodeBlockOpen):]
		end := strings.Index(remaining, telegramCodeBlockClose)
		if end < 0 {
			builder.WriteString(telegramRenderPlainHTML(text[start:]))
			break
		}
		block := remaining[:end]
		language := ""
		code := block
		if newline := strings.Index(block, "\n"); newline >= 0 {
			language = strings.TrimSpace(block[:newline])
			code = block[newline+1:]
		}
		builder.WriteString("<pre><code")
		if language != "" {
			builder.WriteString(` class="language-`)
			builder.WriteString(html.EscapeString(language))
			builder.WriteString(`"`)
		}
		builder.WriteString(">")
		builder.WriteString(html.EscapeString(code))
		builder.WriteString("</code></pre>")
		text = remaining[end+len(telegramCodeBlockClose):]
	}
	return builder.String()
}

func telegramRenderPlainHTML(text string) string {
	if text == "" {
		return ""
	}

	lines := strings.Split(text, "\n")
	var builder strings.Builder
	for index, line := range lines {
		if index > 0 {
			builder.WriteString("\n")
		}
		if _, title, ok := formatting.ParseMarkdownHeading(line); ok {
			builder.WriteString("<b>")
			builder.WriteString(html.EscapeString(strings.ToUpper(title)))
			builder.WriteString("</b>")
			continue
		}
		builder.WriteString(telegramEscapeHTMLPreservingSpaces(line))
	}
	return builder.String()
}

func telegramEscapeHTMLPreservingSpaces(text string) string {
	if text == "" {
		return ""
	}
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "\u00a0", " ")

	var builder strings.Builder
	previousSpace := false
	for position, r := range text {
		switch r {
		case ' ':
			if position == 0 || previousSpace {
				builder.WriteRune('\u00a0')
			} else {
				builder.WriteByte(' ')
			}
			previousSpace = true
		case '\t':
			builder.WriteString("\u00a0\u00a0\u00a0\u00a0")
			previousSpace = true
		default:
			builder.WriteString(html.EscapeString(string(r)))
			previousSpace = false
		}
	}
	return builder.String()
}

func (b *Bot) editMessageTextPayload(ctx context.Context, payload map[string]any) error {
	var response struct {
		OK bool `json:"ok"`
	}
	_, err := b.doJSON(ctx, http.MethodPost, "/editMessageText", payload, &response)
	if err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("telegram editMessageText returned not ok")
	}
	return nil
}

func (b *Bot) answerCallbackQuery(ctx context.Context, callbackID string, text string) error {
	payload := map[string]any{
		"callback_query_id": callbackID,
	}
	if strings.TrimSpace(text) != "" {
		payload["text"] = text
	}
	var response struct {
		OK bool `json:"ok"`
	}
	_, err := b.doJSON(ctx, http.MethodPost, "/answerCallbackQuery", payload, &response)
	if err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("telegram answerCallbackQuery returned not ok")
	}
	return nil
}

type downloadedTelegramAttachment struct {
	Path     string
	Filename string
	MimeType string
}

func (b *Bot) downloadMessageAttachments(ctx context.Context, message *telegramMessage) ([]downloadedTelegramAttachment, error) {
	if message == nil {
		return nil, nil
	}
	candidates := telegramAttachmentCandidates(message)
	if len(candidates) == 0 {
		return nil, nil
	}
	outputDir := filepath.Join(b.dataDir, "telegram_attachments", safeTelegramID(strconv.FormatInt(message.Chat.ID, 10)), strconv.Itoa(message.MessageID))
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, err
	}
	downloaded := make([]downloadedTelegramAttachment, 0, len(candidates))
	for index, candidate := range candidates {
		filePath, err := b.getFilePath(ctx, candidate.FileID)
		if err != nil {
			return nil, err
		}
		filename := safeLocalFilename(candidate.Filename)
		if filename == "" {
			filename = fmt.Sprintf("attachment_%d%s", index+1, candidate.Extension)
		}
		if filepath.Ext(filename) == "" && candidate.Extension != "" {
			filename += candidate.Extension
		}
		localPath := filepath.Join(outputDir, filename)
		if err := b.downloadTelegramFile(ctx, filePath, localPath); err != nil {
			return nil, err
		}
		downloaded = append(downloaded, downloadedTelegramAttachment{
			Path:     localPath,
			Filename: filename,
			MimeType: candidate.MimeType,
		})
	}
	return downloaded, nil
}

type telegramAttachmentCandidate struct {
	FileID    string
	Filename  string
	MimeType  string
	Extension string
}

func telegramAttachmentCandidates(message *telegramMessage) []telegramAttachmentCandidate {
	candidates := []telegramAttachmentCandidate{}
	if message.Document != nil && strings.TrimSpace(message.Document.FileID) != "" {
		candidates = append(candidates, telegramAttachmentCandidate{
			FileID:    message.Document.FileID,
			Filename:  message.Document.FileName,
			MimeType:  message.Document.MimeType,
			Extension: filepath.Ext(message.Document.FileName),
		})
	}
	if len(message.Photo) > 0 {
		photo := largestTelegramPhoto(message.Photo)
		if strings.TrimSpace(photo.FileID) != "" {
			candidates = append(candidates, telegramAttachmentCandidate{
				FileID:    photo.FileID,
				Filename:  "photo_" + safeTelegramID(photo.FileUniqueID) + ".jpg",
				MimeType:  "image/jpeg",
				Extension: ".jpg",
			})
		}
	}
	return candidates
}

func largestTelegramPhoto(photos []telegramPhotoSize) telegramPhotoSize {
	best := telegramPhotoSize{}
	bestArea := 0
	for _, photo := range photos {
		area := photo.Width * photo.Height
		if area > bestArea {
			best = photo
			bestArea = area
		}
	}
	return best
}

func (b *Bot) getFilePath(ctx context.Context, fileID string) (string, error) {
	var response struct {
		OK     bool `json:"ok"`
		Result struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
	}
	payload := map[string]any{"file_id": fileID}
	_, err := b.doJSON(ctx, http.MethodPost, "/getFile", payload, &response)
	if err != nil {
		return "", err
	}
	if !response.OK || strings.TrimSpace(response.Result.FilePath) == "" {
		return "", fmt.Errorf("telegram getFile returned no file path")
	}
	return response.Result.FilePath, nil
}

func (b *Bot) downloadTelegramFile(ctx context.Context, filePath string, outputPath string) error {
	fileURL := fmt.Sprintf("%s/file/bot%s/%s", b.apiBase, b.token, strings.TrimLeft(filePath, "/"))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return err
	}
	response, err := b.client.Do(request)
	if err != nil {
		return fmt.Errorf("telegram file download failed: %s", redactTelegramToken(err.Error(), b.token))
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("telegram file download status %d: %s", response.StatusCode, strings.TrimSpace(redactTelegramToken(string(body), b.token)))
	}
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.Copy(file, response.Body); err != nil {
		return err
	}
	return nil
}

func safeLocalFilename(value string) string {
	filename := filepath.Base(strings.TrimSpace(value))
	if filename == "." || filename == string(filepath.Separator) || filename == "" {
		return ""
	}
	filename = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, filename)
	if strings.Trim(filename, "._ ") == "" {
		return ""
	}
	return filename
}

func (b *Bot) readOffset() int64 {
	bytes, err := os.ReadFile(b.offsetPath)
	if err != nil {
		return 0
	}
	offset, err := strconv.ParseInt(strings.TrimSpace(string(bytes)), 10, 64)
	if err != nil {
		return 0
	}
	return offset
}

func (b *Bot) writeOffset(offset int64) error {
	return os.WriteFile(b.offsetPath, []byte(strconv.FormatInt(offset, 10)), 0o644)
}

func (b *Bot) doJSON(ctx context.Context, method, path string, body any, out any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = strings.NewReader(string(jsonBytes))
	}

	request, err := http.NewRequestWithContext(ctx, method, b.apiURL(path), reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := b.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("telegram api request failed: %s", redactTelegramToken(err.Error(), b.token))
	}
	defer response.Body.Close()

	responseBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram api status %d: %s", response.StatusCode, strings.TrimSpace(redactTelegramToken(string(responseBytes), b.token)))
	}
	if out != nil {
		if err := json.Unmarshal(responseBytes, out); err != nil {
			return nil, err
		}
	}
	return responseBytes, nil
}

func (b *Bot) apiURL(path string) string {
	return fmt.Sprintf("%s/bot%s%s", b.apiBase, b.token, path)
}

type telegramProgressEditor struct {
	bot       *Bot
	chatID    int64
	messageID int
	lastText  string
}

func newTelegramProgressEditor(bot *Bot, chatID int64, messageID int) *telegramProgressEditor {
	return &telegramProgressEditor{
		bot:       bot,
		chatID:    chatID,
		messageID: messageID,
		lastText:  "Đang xử lý...",
	}
}

func (e *telegramProgressEditor) Update(ctx context.Context, text string) error {
	if strings.TrimSpace(text) == "" || text == e.lastText {
		return nil
	}
	if err := e.bot.editMessageText(ctx, e.chatID, e.messageID, text); err != nil {
		return err
	}
	e.lastText = text
	return nil
}

func (e *telegramProgressEditor) UpdateWithReplyMarkup(ctx context.Context, text string, replyMarkup any) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if err := e.bot.editMessageTextWithReplyMarkup(ctx, e.chatID, e.messageID, text, replyMarkup); err != nil {
		return err
	}
	e.lastText = text
	return nil
}

func telegramProgressText(event agent.ProgressEvent) string {
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

func telegramGenericErrorText() string {
	return "Mình chưa thể hoàn tất yêu cầu này. Chi tiết lỗi đã được ghi ở terminal local."
}

func redactTelegramToken(text string, token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return text
	}
	return strings.ReplaceAll(text, token, "<telegram-token>")
}

type telegramUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type telegramUpdate struct {
	UpdateID      int                    `json:"update_id"`
	Message       *telegramMessage       `json:"message,omitempty"`
	CallbackQuery *telegramCallbackQuery `json:"callback_query,omitempty"`
}

type telegramSentMessage struct {
	MessageID int `json:"message_id"`
}

type telegramMessage struct {
	MessageID int                 `json:"message_id"`
	From      *telegramUser       `json:"from,omitempty"`
	Chat      telegramChat        `json:"chat"`
	Text      string              `json:"text,omitempty"`
	Caption   string              `json:"caption,omitempty"`
	Document  *telegramDocument   `json:"document,omitempty"`
	Photo     []telegramPhotoSize `json:"photo,omitempty"`
}

type telegramChat struct {
	ID int64 `json:"id"`
}

type telegramDocument struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id,omitempty"`
	FileName     string `json:"file_name,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
}

type telegramPhotoSize struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id,omitempty"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
}

type telegramCallbackQuery struct {
	ID      string           `json:"id"`
	From    *telegramUser    `json:"from,omitempty"`
	Message *telegramMessage `json:"message,omitempty"`
	Data    string           `json:"data,omitempty"`
}

func telegramApprovalKeyboard(approvalID string) map[string]any {
	return map[string]any{
		"inline_keyboard": [][]map[string]string{
			{
				{"text": "Xác nhận", "callback_data": telegramApprovalCallbackData("approve", approvalID)},
				{"text": "Hủy", "callback_data": telegramApprovalCallbackData("reject", approvalID)},
			},
		},
	}
}

func telegramApprovalCallbackData(action string, approvalID string) string {
	action = strings.TrimSpace(action)
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return "vclaw:approval:" + action
	}
	return "vclaw:approval:" + action + ":" + approvalID
}

func parseTelegramApprovalCallback(data string) (action string, approvalID string, ok bool) {
	parts := strings.Split(strings.TrimSpace(data), ":")
	if len(parts) < 3 || parts[0] != "vclaw" || parts[1] != "approval" {
		return "", "", false
	}
	action = strings.TrimSpace(parts[2])
	if action != "approve" && action != "reject" && action != "revise" {
		return "", "", false
	}
	if len(parts) > 3 {
		approvalID = strings.TrimSpace(strings.Join(parts[3:], ":"))
	}
	return action, approvalID, true
}

func safeTelegramID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return strings.NewReplacer(" ", "_", ".", "_", ":", "_", "/", "_", "\\", "_").Replace(value)
}
