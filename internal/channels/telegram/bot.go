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
	HandleMessage(ctx context.Context, msg agent.InboundMessage) (agent.OutboundMessage, error)
	FinalizeAudit(msg agent.InboundMessage, err error)
	RecordIgnored(msg agent.InboundMessage, actionTaken string)
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
	if update.Message == nil {
		return true, nil
	}
	inbound := agent.InboundMessage{
		RequestID: fmt.Sprintf("telegram_update_%d", update.UpdateID),
		SessionID: "",
		Channel:   "telegram",
		UpdateID:  int64(update.UpdateID),
		ChatID:    update.Message.Chat.ID,
		Text:      update.Message.Text,
		Source:    "telegram",
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"telegramUpdateId": update.UpdateID,
			"telegramChatId":   update.Message.Chat.ID,
			"source":           "telegram",
		},
	}
	if update.Message.Chat.ID != 0 {
		inbound.SessionID = fmt.Sprintf("telegram_chat_%d", update.Message.Chat.ID)
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

	text := telegramTextFromOutbound(outbound)
	if strings.TrimSpace(text) == "" {
		err := fmt.Errorf("empty outbound message")
		b.handler.FinalizeAudit(inbound, err)
		return false, err
	}
	if strings.EqualFold(outbound.Status, "failed") {
		b.logger.Error("agent response error", "request_id", outbound.RequestID, "session_id", outbound.SessionID, "status", outbound.Status, "message", outbound.Message)
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
	e.lastText = text
	return e.bot.editMessageText(ctx, e.chatID, e.messageID, text)
}

func telegramProgressText(event agent.ProgressEvent) string {
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

func telegramTextFromOutbound(outbound agent.OutboundMessage) string {
	if strings.EqualFold(outbound.Status, "failed") {
		return telegramGenericErrorText()
	}
	if strings.TrimSpace(outbound.Text) != "" {
		return outbound.Text
	}
	if strings.TrimSpace(outbound.Message) != "" {
		return outbound.Message
	}
	switch outbound.Status {
	case "approval_required":
		return "Tôi cần bạn xác nhận trước khi thực hiện hành động này."
	case "completed":
		return "Đã hoàn tất."
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
	UpdateID int              `json:"update_id"`
	Message  *telegramMessage `json:"message,omitempty"`
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
