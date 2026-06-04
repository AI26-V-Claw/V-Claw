package telegram

import (
	"context"
	"encoding/json"
	"fmt"
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
	inbound := contracts.UserMessage{
		RequestID: fmt.Sprintf("telegram_update_%d", update.UpdateID),
		SessionID: fmt.Sprintf("telegram_chat_%d", update.Message.Chat.ID),
		Channel:   "telegram",
		Text:      update.Message.Text,
		Locale:    "",
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"telegramUpdateId": update.UpdateID,
			"telegramChatId":   update.Message.Chat.ID,
			"source":           "telegram",
		},
	}

	if strings.TrimSpace(update.Message.Text) == "" {
		if b.handler != nil {
			b.handler.RecordIgnored(inbound, "ignored_non_text")
		}
		return true, nil
	}
	if update.Message.From == nil || update.Message.From.ID != b.allowedUserID {
		if b.handler != nil {
			b.handler.RecordIgnored(inbound, "ignored_unauthorized")
		}
		return true, nil
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

	progressCtx := agent.WithProgressSink(ctx, func(progressCtx context.Context, event agent.ProgressEvent) {
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
		if err := progress.UpdateWithReplyMarkup(ctx, text, telegramApprovalKeyboard(outbound.ApprovalID)); err != nil {
			b.logger.Error("telegram final approval edit failed", "error", err)
			if _, sendErr := b.sendMessageWithReplyMarkup(ctx, update.Message.Chat.ID, text, telegramApprovalKeyboard(outbound.ApprovalID)); sendErr != nil {
				b.handler.FinalizeAudit(inbound, sendErr)
				return false, sendErr
			}
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
	if action == "revise" {
		_ = b.answerCallbackQuery(ctx, callback.ID, "Reply with your revision comment.")
		text := "Bạn muốn chỉnh gì? Hãy gửi tin nhắn theo mẫu:\n\nrevise <nội dung muốn chỉnh>"
		if approvalID != "" {
			text += "\n\nApproval ID: " + approvalID
		}
		if err := b.editMessageText(ctx, callback.Message.Chat.ID, callback.Message.MessageID, text); err != nil {
			return false, err
		}
		return true, nil
	}

	command := action
	if approvalID != "" {
		command += " " + approvalID
	}
	inbound := contracts.UserMessage{
		RequestID: fmt.Sprintf("telegram_callback_%s", safeTelegramID(callback.ID)),
		SessionID: fmt.Sprintf("telegram_chat_%d", callback.Message.Chat.ID),
		Channel:   "telegram",
		Text:      command,
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"telegramUpdateId": update.UpdateID,
			"telegramChatId":   callback.Message.Chat.ID,
			"telegramCallback": action,
			"source":           "telegram",
		},
	}
	if b.handler == nil {
		return false, fmt.Errorf("message handler is not configured")
	}
	_ = b.answerCallbackQuery(ctx, callback.ID, "Đang xử lý...")
	progress := newTelegramProgressEditor(b, callback.Message.Chat.ID, callback.Message.MessageID)
	if err := progress.Update(ctx, "Đang xử lý quyết định..."); err != nil {
		b.logger.Error("telegram approval progress edit failed", "error", err)
	}
	outbound, err := b.handler.HandleMessage(ctx, inbound)
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
		"text":    text,
	}
	return b.sendMessagePayload(ctx, payload)
}

func (b *Bot) sendMessageWithReplyMarkup(ctx context.Context, chatID int64, text string, replyMarkup any) (telegramSentMessage, error) {
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
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
		"text":       text,
	}
	return b.editMessageTextPayload(ctx, payload)
}

func (b *Bot) editMessageTextWithReplyMarkup(ctx context.Context, chatID int64, messageID int, text string, replyMarkup any) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	return b.editMessageTextPayload(ctx, payload)
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
	text = strings.TrimSpace(text)
	if text == "" || text == e.lastText {
		return nil
	}
	if err := e.bot.editMessageText(ctx, e.chatID, e.messageID, text); err != nil {
		return err
	}
	e.lastText = text
	return nil
}

func (e *telegramProgressEditor) UpdateWithReplyMarkup(ctx context.Context, text string, replyMarkup any) error {
	text = strings.TrimSpace(text)
	if text == "" {
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
	default:
		return ""
	}
}

func telegramTextFromResponse(response contracts.AgentResponse) string {
	if strings.EqualFold(string(response.Status), "failed") {
		return telegramGenericErrorText()
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
	MessageID int           `json:"message_id"`
	From      *telegramUser `json:"from,omitempty"`
	Chat      telegramChat  `json:"chat"`
	Text      string        `json:"text,omitempty"`
}

type telegramChat struct {
	ID int64 `json:"id"`
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
				{"text": "Yes", "callback_data": telegramApprovalCallbackData("approve", approvalID)},
				{"text": "No", "callback_data": telegramApprovalCallbackData("reject", approvalID)},
				{"text": "Revise", "callback_data": telegramApprovalCallbackData("revise", approvalID)},
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
