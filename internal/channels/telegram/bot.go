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

	"github.com/nxhai/vclaw/internal/agent"
)

const longPollTimeout = 30

type Bot struct {
	token         string
	allowedUserID int64
	dataDir       string
	offsetPath    string
	client        *http.Client
	orchestrator  *agent.Orchestrator
	logger        *slog.Logger
	apiBase       string
}

func New(token string, allowedUserID int64, dataDir string, orchestrator *agent.Orchestrator, logger *slog.Logger) *Bot {
	return &Bot{
		token:         token,
		allowedUserID: allowedUserID,
		dataDir:       dataDir,
		offsetPath:    filepath.Join(dataDir, "telegram_offset.txt"),
		client: &http.Client{
			Timeout: 65 * time.Second,
		},
		orchestrator: orchestrator,
		logger:       logger,
		apiBase:      "https://api.telegram.org",
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
		UserID:    0,
		Text:      update.Message.Text,
		Source:    "telegram",
		Timestamp: time.Now().UTC(),
	}
	if update.Message.From != nil {
		inbound.UserID = update.Message.From.ID
		inbound.SessionID = fmt.Sprintf("telegram_user_%d", update.Message.From.ID)
	} else if update.Message.Chat.ID != 0 {
		inbound.SessionID = fmt.Sprintf("telegram_chat_%d", update.Message.Chat.ID)
	}

	if strings.TrimSpace(update.Message.Text) == "" {
		b.orchestrator.RecordIgnored(inbound, "ignored_non_text")
		return true, nil
	}
	if update.Message.From == nil || update.Message.From.ID != b.allowedUserID {
		b.orchestrator.RecordIgnored(inbound, "ignored_unauthorized")
		return true, nil
	}

	inbound.UserID = update.Message.From.ID

	outbound, err := b.orchestrator.HandleMessage(ctx, inbound)
	if err != nil {
		b.orchestrator.FinalizeAudit(inbound, err)
		return false, err
	}

	if strings.TrimSpace(outbound.Text) == "" {
		return false, fmt.Errorf("empty outbound message")
	}

	if err := b.sendMessage(ctx, outbound); err != nil {
		b.orchestrator.FinalizeAudit(inbound, err)
		return false, err
	}

	b.orchestrator.FinalizeAudit(inbound, nil)
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

func (b *Bot) sendMessage(ctx context.Context, outbound agent.OutboundMessage) error {
	payload := map[string]any{
		"chat_id": outbound.ChatID,
		"text":    outbound.Text,
	}
	var response struct {
		OK bool `json:"ok"`
	}
	_, err := b.doJSON(ctx, http.MethodPost, "/sendMessage", payload, &response)
	if err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("telegram sendMessage returned not ok")
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
		return nil, err
	}
	defer response.Body.Close()

	responseBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram api status %d: %s", response.StatusCode, strings.TrimSpace(string(responseBytes)))
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

type telegramUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type telegramUpdate struct {
	UpdateID int              `json:"update_id"`
	Message  *telegramMessage `json:"message,omitempty"`
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
