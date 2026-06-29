package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vclaw/internal/agent"
	"vclaw/internal/channels/formatting"
	"vclaw/internal/contracts"
	"vclaw/internal/filesafety"
	"vclaw/internal/monitoring"
	"vclaw/internal/policies"
	sandboxtool "vclaw/internal/tools/system/sandbox"
)

const longPollTimeout = 30

var (
	queryLatestTelegramRun  = monitoring.QueryLatestRunForSession
	queryRecentTelegramRuns = monitoring.QueryRecentRunsForSession
	queryTelegramLogs       = monitoring.QueryLogs
)

type Bot struct {
	token         string
	allowedUserID int64
	dataDir       string
	offsetPath    string
	client        *http.Client
	handler       messageHandler
	logger        *slog.Logger
	apiBase       string
	policyStore   *policies.UserPolicyStore
	policyMu      sync.Mutex
	policyDrafts  map[int64]map[contracts.RiskLevel]policies.PolicyGroup
	sessionIndex  *telegramSessionIndexStore
	state         *telegramChannelState
	workCh        chan telegramUpdate
	busySessions  sync.Map // chat ID (int64) → struct{} while a message is being processed
	pendingMu     sync.Mutex
	pendingByChat map[int64][]telegramUpdate
}

const maxPendingTelegramUpdatesPerChat = 8

type messageHandler interface {
	HandleMessage(ctx context.Context, msg contracts.UserMessage) (contracts.AgentResponse, error)
	ResetSession(ctx context.Context, sessionID string) error
	FinalizeAudit(msg contracts.UserMessage, err error)
	RecordIgnored(msg contracts.UserMessage, actionTaken string)
}

func New(token string, allowedUserID int64, dataDir string, args ...any) *Bot {
	var policyStore *policies.UserPolicyStore
	var handler messageHandler
	var logger *slog.Logger
	switch len(args) {
	case 2:
		handler, _ = args[0].(messageHandler)
		logger, _ = args[1].(*slog.Logger)
	case 3:
		policyStore, _ = args[0].(*policies.UserPolicyStore)
		handler, _ = args[1].(messageHandler)
		logger, _ = args[2].(*slog.Logger)
	default:
		panic("telegram.New expects handler/logger or policyStore/handler/logger")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Bot{
		token:         token,
		allowedUserID: allowedUserID,
		dataDir:       dataDir,
		offsetPath:    filepath.Join(dataDir, "telegram_offset.txt"),
		workCh:        make(chan telegramUpdate, 64),
		client: &http.Client{
			Timeout: 65 * time.Second,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				IdleConnTimeout:       60 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
		handler:       handler,
		logger:        logger,
		apiBase:       "https://api.telegram.org",
		policyStore:   policyStore,
		policyDrafts:  make(map[int64]map[contracts.RiskLevel]policies.PolicyGroup),
		sessionIndex:  newTelegramSessionIndexStore(dataDir),
		state:         newTelegramChannelState(),
		pendingByChat: make(map[int64][]telegramUpdate),
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
	if err := b.setMyCommands(ctx); err != nil {
		b.logger.Warn("failed to register telegram commands", "error", err)
	}

	// A single worker goroutine processes updates sequentially. This ensures
	// session state is never read/written concurrently while keeping the
	// polling loop non-blocking even when the LLM is slow.
	go b.processWorker(ctx)

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
			// Advance offset before dispatching so that a slow worker does not
			// cause the same update to be re-delivered on the next poll.
			offset = int64(update.UpdateID) + 1
			if err := b.writeOffset(offset); err != nil {
				b.logger.Error("failed to persist offset", "offset", offset, "error", err)
			}

			// /cancel is dispatched immediately in the polling loop (not via
			// workCh) so it can interrupt a run blocking the worker goroutine.
			if update.Message != nil &&
				update.Message.From != nil &&
				update.Message.From.ID == b.allowedUserID {
				msgText := strings.TrimSpace(update.Message.Text)
				if msgText == "" {
					msgText = strings.TrimSpace(update.Message.Caption)
				}
				if isTelegramCancelCommand(msgText) {
					go func(u telegramUpdate) {
						if _, err := b.processUpdate(ctx, u); err != nil {
							b.logger.Error("telegram cancel update failed", "update_id", u.UpdateID, "error", err)
						}
					}(update)
					continue
				}
			}

			// Callback queries (approval buttons) always go through.
			// For regular messages, queue follow-ups if the session is already busy.
			if update.Message != nil {
				chatID := update.Message.Chat.ID
				msgText := strings.TrimSpace(update.Message.Text)
				if msgText == "" {
					msgText = strings.TrimSpace(update.Message.Caption)
				}
				if !isTelegramCancelCommand(msgText) {
					if _, alreadyBusy := b.busySessions.LoadOrStore(chatID, struct{}{}); alreadyBusy {
						queued := b.enqueuePendingUpdate(update)
						go func(cid int64, queued bool) {
							text := "Mình đã nhận tin nhắn này, sẽ xử lý ngay sau lượt hiện tại."
							if !queued {
								text = "Mình đang có quá nhiều tin nhắn đang chờ, vui lòng đợi phản hồi hiện tại trước."
							}
							if _, err := b.sendMessage(ctx, cid, text); err != nil {
								b.logger.Error("telegram busy reply failed", "chat_id", cid, "error", err)
							}
						}(chatID, queued)
						continue
					}
				}
			}

			select {
			case b.workCh <- update:
			case <-ctx.Done():
				if update.Message != nil {
					b.busySessions.Delete(update.Message.Chat.ID)
				}
				return ctx.Err()
			}
		}
	}
}

func (b *Bot) processWorker(ctx context.Context) {
	for {
		select {
		case u := <-b.workCh:
			b.processUpdateWithPendingDrain(ctx, u)
		case <-ctx.Done():
			return
		}
	}
}

func (b *Bot) processUpdateWithPendingDrain(ctx context.Context, update telegramUpdate) {
	current := update
	for {
		if _, err := b.processUpdate(ctx, current); err != nil {
			b.logger.Error("telegram update failed", "update_id", current.UpdateID, "error", err)
		}
		if current.Message == nil {
			return
		}
		chatID := current.Message.Chat.ID
		next, ok := b.dequeuePendingUpdate(chatID)
		if !ok {
			b.busySessions.Delete(chatID)
			return
		}
		b.logger.Info("telegram draining pending update", "chat_id", chatID, "update_id", next.UpdateID)
		current = next
		if ctx.Err() != nil {
			b.busySessions.Delete(chatID)
			return
		}
	}
}

func (b *Bot) enqueuePendingUpdate(update telegramUpdate) bool {
	if update.Message == nil {
		return false
	}
	chatID := update.Message.Chat.ID
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()
	queue := b.pendingByChat[chatID]
	if len(queue) >= maxPendingTelegramUpdatesPerChat {
		b.logger.Warn("telegram pending queue full", "chat_id", chatID, "update_id", update.UpdateID, "pending_count", len(queue))
		return false
	}
	b.pendingByChat[chatID] = append(queue, update)
	b.logger.Info("telegram queued pending update", "chat_id", chatID, "update_id", update.UpdateID, "pending_count", len(queue)+1)
	return true
}

func (b *Bot) dequeuePendingUpdate(chatID int64) (telegramUpdate, bool) {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()
	queue := b.pendingByChat[chatID]
	if len(queue) == 0 {
		return telegramUpdate{}, false
	}
	next := queue[0]
	if len(queue) == 1 {
		delete(b.pendingByChat, chatID)
	} else {
		b.pendingByChat[chatID] = queue[1:]
	}
	return next, true
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
		SessionID: telegramLegacySessionID(update.Message.Chat.ID),
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
	if isTelegramPolicyCommand(messageText) {
		if err := b.sendPolicySettingsMenu(ctx, update.Message.Chat.ID); err != nil {
			b.logger.Error("telegram policy settings menu failed", "error", err)
			return false, err
		}
		return true, nil
	}
	if isTelegramNewCommand(messageText) {
		if _, ok := b.state.approvalForChat(update.Message.Chat.ID); ok {
			if _, sendErr := b.sendMessage(ctx, update.Message.Chat.ID, "Phiên này đang có yêu cầu xác nhận. Hãy approve, reject hoặc revise trước khi tạo phiên mới."); sendErr != nil {
				return false, sendErr
			}
			return true, nil
		}
		record, err := b.sessionIndex.Create(ctx, update.Message.Chat.ID, time.Now().UTC())
		if err != nil {
			b.logger.Error("telegram session create failed", "error", err)
			if _, sendErr := b.sendMessage(ctx, update.Message.Chat.ID, "Không thể tạo phiên mới lúc này. Vui lòng thử lại sau."); sendErr != nil {
				return false, sendErr
			}
			return true, nil
		}
		if _, err := b.sendMessage(ctx, update.Message.Chat.ID, fmt.Sprintf("Đã tạo phiên mới: %s", telegramSessionDisplayTitle(record))); err != nil {
			return false, err
		}
		return true, nil
	}
	if isTelegramSessionsCommand(messageText) {
		if err := b.sendTelegramSessionsMenu(ctx, update.Message.Chat.ID, ""); err != nil {
			b.logger.Error("telegram sessions menu failed", "error", err)
			if _, sendErr := b.sendMessage(ctx, update.Message.Chat.ID, "Không thể mở danh sách phiên lúc này. Vui lòng thử lại sau."); sendErr != nil {
				return false, sendErr
			}
		}
		return true, nil
	}
	if isTelegramStatusCommand(messageText) {
		if err := b.sendStatusSummary(ctx, update.Message.Chat.ID); err != nil {
			b.logger.Error("telegram status summary failed", "error", err)
			if _, sendErr := b.sendMessage(ctx, update.Message.Chat.ID, "Không kiểm tra được trạng thái lúc này. Vui lòng thử lại sau."); sendErr != nil {
				return false, sendErr
			}
		}
		return true, nil
	}
	if isTelegramHistoryCommand(messageText) {
		if err := b.sendHistorySummary(ctx, update.Message.Chat.ID, messageText); err != nil {
			b.logger.Error("telegram history summary failed", "error", err)
			if _, sendErr := b.sendMessage(ctx, update.Message.Chat.ID, "Không kiểm tra được trạng thái lúc này. Vui lòng thử lại sau."); sendErr != nil {
				return false, sendErr
			}
		}
		return true, nil
	}
	if isTelegramCancelCommand(messageText) {
		sessionID, err := b.activeTelegramSessionID(ctx, update.Message.Chat.ID)
		if err != nil {
			sessionID = fmt.Sprintf("telegram_chat_%d", update.Message.Chat.ID)
		}
		if canceller, ok := b.handler.(interface{ CancelSession(string) bool }); ok && canceller.CancelSession(sessionID) {
			if _, err := b.sendMessage(ctx, update.Message.Chat.ID, "Đã hủy lệnh đang chạy."); err != nil {
				return false, err
			}
		} else {
			if _, err := b.sendMessage(ctx, update.Message.Chat.ID, "Không có lệnh nào đang chạy."); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	activeSession, err := b.sessionIndex.Active(ctx, update.Message.Chat.ID, time.Now().UTC())
	if err != nil {
		b.logger.Error("telegram active session resolve failed", "error", err)
		if _, sendErr := b.sendMessage(ctx, update.Message.Chat.ID, "Không thể mở phiên hội thoại lúc này. Vui lòng thử lại sau."); sendErr != nil {
			return false, sendErr
		}
		return true, nil
	}
	inbound.SessionID = activeSession.SessionID
	attachments, err := b.downloadMessageAttachments(ctx, update.Message)
	if err != nil {
		if b.handler != nil {
			b.handler.FinalizeAudit(inbound, err)
		}
		if strings.Contains(err.Error(), "file safety gate") {
			if _, sendErr := b.sendMessage(ctx, update.Message.Chat.ID, "Tệp đính kèm bị chặn bởi lớp kiểm tra an toàn file: "+strings.TrimPrefix(err.Error(), "telegram attachment blocked by file safety gate: ")); sendErr != nil {
				return false, sendErr
			}
			return true, nil
		}
		return false, err
	}
	if len(attachments) > 0 {
		paths := make([]string, 0, len(attachments))
		metadata := make([]map[string]any, 0, len(attachments))
		for _, attachment := range attachments {
			paths = append(paths, attachment.Path)
			metadata = append(metadata, map[string]any{
				"path":       attachment.Path,
				"filename":   attachment.Filename,
				"mimeType":   attachment.MimeType,
				"source":     "telegram",
				"fileSafety": attachment.Safety,
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
	touchSession := !isTelegramApprovalTextCommand(inbound.Text)
	if approvalContext, ok := b.state.approvalForChat(update.Message.Chat.ID); ok {
		action := telegramApprovalTextAction(inbound.Text)
		switch action {
		case "approve", "reject":
			touchSession = false
			if err := b.dismissApprovalKeyboard(ctx, approvalContext); err != nil {
				b.logger.Error("telegram approval keyboard dismiss failed", "chat_id", approvalContext.ChatID, "message_id", approvalContext.MessageID, "error", err)
			}
			b.state.deleteApproval(approvalContext.ApprovalID)
			inbound.SessionID = approvalContext.SessionID
			inbound.Metadata["approvalId"] = approvalContext.ApprovalID
			inbound.Text = action + " " + approvalContext.ApprovalID
			inbound.Metadata["telegramCallback"] = action
		case "revise":
			touchSession = false
			if err := b.dismissApprovalKeyboard(ctx, approvalContext); err != nil {
				b.logger.Error("telegram approval keyboard dismiss failed", "chat_id", approvalContext.ChatID, "message_id", approvalContext.MessageID, "error", err)
			}
			b.state.deleteApproval(approvalContext.ApprovalID)
			inbound.SessionID = approvalContext.SessionID
			inbound.Metadata["approvalId"] = approvalContext.ApprovalID
			inbound.Text = "revise " + strings.TrimSpace(inbound.Text)
			inbound.Metadata["telegramCallback"] = "revise"
		default:
			if err := b.rejectPendingApprovalForNewMessage(ctx, update.UpdateID, approvalContext); err != nil {
				if b.handler != nil {
					b.handler.FinalizeAudit(inbound, err)
				}
				return false, err
			}
			if err := b.dismissApprovalKeyboard(ctx, approvalContext); err != nil {
				b.logger.Error("telegram approval keyboard dismiss failed", "chat_id", approvalContext.ChatID, "message_id", approvalContext.MessageID, "error", err)
			}
			b.state.deleteApproval(approvalContext.ApprovalID)
			inbound.SessionID = approvalContext.SessionID
		}
	} else if revision, ok := b.state.consumeRevision(update.Message.Chat.ID); ok {
		touchSession = false
		inbound.SessionID = revision.SessionID
		inbound.Text = "revise " + strings.TrimSpace(inbound.Text)
		inbound.Metadata["telegramCallback"] = "revise"
		inbound.Metadata["approvalId"] = revision.ApprovalID
	}
	if b.handler == nil {
		return false, fmt.Errorf("message handler is not configured")
	}
	if touchSession {
		if err := b.sessionIndex.Touch(ctx, update.Message.Chat.ID, inbound.SessionID, inbound.Text, time.Now().UTC()); err != nil {
			b.logger.Warn("telegram session touch failed", "session_id", inbound.SessionID, "error", err)
		}
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
		b.logger.Info("approval request sent to channel",
			"request_id", outbound.RequestID,
			"session_id", outbound.SessionID,
			"approval_id", outbound.ApprovalRequest.ApprovalID,
			"tool_call_id", outbound.ApprovalRequest.ToolCallID,
			"telegram_chat_id", update.Message.Chat.ID,
		)
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

func isTelegramPolicyCommand(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || !strings.HasPrefix(text, "/") {
		return false
	}
	command := strings.TrimPrefix(text, "/")
	if index := strings.IndexAny(command, " \t\n@"); index >= 0 {
		command = command[:index]
	}
	return strings.EqualFold(command, "policy")
}

func isTelegramStatusCommand(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || !strings.HasPrefix(text, "/") {
		return false
	}
	command := strings.TrimPrefix(text, "/")
	if index := strings.IndexAny(command, " \t\n@"); index >= 0 {
		command = command[:index]
	}
	return strings.EqualFold(command, "status")
}

func isTelegramHistoryCommand(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || !strings.HasPrefix(text, "/") {
		return false
	}
	command := strings.TrimPrefix(text, "/")
	if index := strings.IndexAny(command, " \t\n@"); index >= 0 {
		command = command[:index]
	}
	return strings.EqualFold(command, "history")
}

func isTelegramCancelCommand(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || !strings.HasPrefix(text, "/") {
		return false
	}
	command := strings.TrimPrefix(text, "/")
	if index := strings.IndexAny(command, " \t\n@"); index >= 0 {
		command = command[:index]
	}
	return strings.EqualFold(command, "cancel")
}

func isTelegramNewCommand(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || !strings.HasPrefix(text, "/") {
		return false
	}
	command := strings.TrimPrefix(text, "/")
	if index := strings.IndexAny(command, " \t\n@"); index >= 0 {
		command = command[:index]
	}
	return strings.EqualFold(command, "new")
}

func isTelegramSessionsCommand(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || !strings.HasPrefix(text, "/") {
		return false
	}
	command := strings.TrimPrefix(text, "/")
	if index := strings.IndexAny(command, " \t\n@"); index >= 0 {
		command = command[:index]
	}
	return strings.EqualFold(command, "sessions")
}

func isTelegramApprovalTextCommand(text string) bool {
	action := telegramApprovalTextAction(text)
	return action == "approve" || action == "reject" || action == "revise"
}

func (b *Bot) sendTelegramSessionsMenu(ctx context.Context, chatID int64, confirmDeleteKey string) error {
	index, err := b.sessionIndex.List(ctx, chatID, time.Now().UTC())
	if err != nil {
		return err
	}
	_, err = b.sendMessageWithReplyMarkup(ctx, chatID, telegramSessionListText(index, time.Now()), telegramSessionKeyboard(index, confirmDeleteKey))
	return err
}

func (b *Bot) activeTelegramSessionID(ctx context.Context, chatID int64) (string, error) {
	record, err := b.sessionIndex.Active(ctx, chatID, time.Now().UTC())
	if err != nil {
		return "", err
	}
	return record.SessionID, nil
}

func (b *Bot) sendStatusSummary(ctx context.Context, chatID int64) error {
	sessionID, err := b.activeTelegramSessionID(ctx, chatID)
	if err != nil {
		return err
	}
	run, err := queryLatestTelegramRun(ctx, strings.TrimSpace(os.Getenv("DATABASE_URL")), sessionID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(run.RunID) == "" {
		_, err = b.sendMarkdownMessage(ctx, chatID, FormatStatus(nil))
		return err
	}
	runState, err := queryTelegramRunByID(ctx, strings.TrimSpace(os.Getenv("DATABASE_URL")), run.RunID)
	if err != nil {
		return err
	}
	_, err = b.sendMarkdownMessage(ctx, chatID, FormatStatus(runState))
	return err
}

func (b *Bot) sendHistorySummary(ctx context.Context, chatID int64, messageText string) error {
	sessionID, err := b.activeTelegramSessionID(ctx, chatID)
	if err != nil {
		return err
	}
	runs, err := queryRecentTelegramRuns(ctx, strings.TrimSpace(os.Getenv("DATABASE_URL")), sessionID, 10)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		_, err = b.sendMarkdownMessage(ctx, chatID, FormatHistory(nil, time.Now()))
		return err
	}

	fields := strings.Fields(strings.TrimSpace(messageText))
	if len(fields) > 1 {
		index, parseErr := strconv.Atoi(strings.TrimSpace(fields[1]))
		if parseErr != nil || index <= 0 || index > len(runs) {
			_, sendErr := b.sendMarkdownMessage(ctx, chatID, "Số thứ tự không hợp lệ. Hãy dùng /history <số>.")
			return sendErr
		}
		runState, err := queryTelegramRunByID(ctx, strings.TrimSpace(os.Getenv("DATABASE_URL")), runs[index-1].RunID)
		if err != nil {
			return err
		}
		_, err = b.sendMarkdownMessage(ctx, chatID, FormatStatus(runState))
		return err
	}

	states := make([]*agent.RunState, 0, len(runs))
	for _, run := range runs {
		runState, err := queryTelegramRunByID(ctx, strings.TrimSpace(os.Getenv("DATABASE_URL")), run.RunID)
		if err != nil {
			return err
		}
		if runState != nil {
			states = append(states, runState)
		}
	}
	_, err = b.sendMarkdownMessage(ctx, chatID, FormatHistory(states, time.Now()))
	return err
}

func (b *Bot) rejectPendingApprovalForNewMessage(ctx context.Context, updateID int, approvalContext telegramApprovalContext) error {
	if b.handler == nil {
		return fmt.Errorf("message handler is not configured")
	}
	rejectMsg := contracts.UserMessage{
		RequestID: fmt.Sprintf("telegram_update_%d_auto_reject", updateID),
		SessionID: approvalContext.SessionID,
		Channel:   "telegram",
		Text:      "reject " + approvalContext.ApprovalID,
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"telegramUpdateId":            updateID,
			"telegramChatId":              approvalContext.ChatID,
			"source":                      "telegram",
			"approvalId":                  approvalContext.ApprovalID,
			"autoRejectedPendingApproval": true,
		},
	}
	_, err := b.handler.HandleMessage(ctx, rejectMsg)
	b.handler.FinalizeAudit(rejectMsg, err)
	if err != nil {
		b.logger.Error("telegram auto-reject failed", "approval_id", approvalContext.ApprovalID, "session_id", approvalContext.SessionID, "error", err)
	}
	return err
}

func (b *Bot) processCallbackQuery(ctx context.Context, update telegramUpdate) (bool, error) {
	callback := update.CallbackQuery
	userID := int64(0)
	if callback.From != nil {
		userID = callback.From.ID
	}
	b.logger.Info("telegram callback received",
		"callback_id", callback.ID,
		"user_id", userID,
	)
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
	if action, chatID, riskLevel, ok := parseTelegramPolicySettingsCallback(callback.Data); ok {
		targetChatID := callback.Message.Chat.ID
		if chatID != 0 {
			targetChatID = chatID
		}
		switch action {
		case telegramPolicySettingsCycleAction:
			if err := b.cycleTelegramPolicySetting(ctx, targetChatID, callback.Message.MessageID, riskLevel); err != nil {
				b.logger.Error("telegram policy cycle failed", "error", err)
				_ = b.answerCallbackQuery(ctx, callback.ID, "")
				return true, nil
			}
			_ = b.answerCallbackQuery(ctx, callback.ID, "")
			return true, nil
		case telegramPolicySettingsSaveAction:
			if err := b.saveTelegramPolicySettings(ctx, targetChatID, callback.Message.MessageID); err != nil {
				b.logger.Error("telegram policy save failed", "error", err)
				_ = b.answerCallbackQuery(ctx, callback.ID, "")
				return true, nil
			}
			_ = b.answerCallbackQuery(ctx, callback.ID, "")
			return true, nil
		default:
			_ = b.answerCallbackQuery(ctx, callback.ID, "Unknown action.")
			return true, nil
		}
	}
	if action, key, ok := parseTelegramSessionCallback(callback.Data); ok {
		return b.processTelegramSessionCallback(ctx, update, action, key)
	}
	action, approvalID, ok := parseTelegramApprovalCallback(callback.Data)
	if !ok {
		_ = b.answerCallbackQuery(ctx, callback.ID, "Unknown action.")
		return true, nil
	}
	b.logger.Info("approval decision received and parsed",
		"request_id", fmt.Sprintf("telegram_callback_%s", safeTelegramID(callback.ID)),
		"session_id", fmt.Sprintf("telegram_chat_%d", callback.Message.Chat.ID),
		"approval_id", approvalID,
		"decision", action,
		"telegram_user_id", callback.From.ID,
	)
	approvalContext, ok := b.state.lookupApproval(approvalID, callback.Message.Chat.ID, callback.Message.MessageID)
	if !ok {
		if err := b.editMessageReplyMarkup(ctx, callback.Message.Chat.ID, callback.Message.MessageID, map[string]any{
			"inline_keyboard": [][]map[string]string{},
		}); err != nil {
			b.logger.Error("telegram stale approval keyboard dismiss failed", "chat_id", callback.Message.Chat.ID, "message_id", callback.Message.MessageID, "error", err)
		}
		if action == "revise" || b.state.hasApproval(approvalID) {
			_ = b.answerCallbackQuery(ctx, callback.ID, "Yêu cầu xác nhận này không còn hợp lệ.")
			return true, nil
		}
		approvalContext = telegramApprovalContext{
			ApprovalID: approvalID,
			SessionID:  fmt.Sprintf("telegram_chat_%d", callback.Message.Chat.ID),
			ChatID:     callback.Message.Chat.ID,
		}
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
		b.logger.Info("approval request sent to channel",
			"request_id", outbound.RequestID,
			"session_id", outbound.SessionID,
			"approval_id", outbound.ApprovalRequest.ApprovalID,
			"tool_call_id", outbound.ApprovalRequest.ToolCallID,
			"telegram_chat_id", callback.Message.Chat.ID,
		)
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

func (b *Bot) processTelegramSessionCallback(ctx context.Context, update telegramUpdate, action string, key string) (bool, error) {
	callback := update.CallbackQuery
	chatID := callback.Message.Chat.ID
	if action != "cancel_delete" {
		if _, ok := b.state.approvalForChat(chatID); ok {
			_ = b.answerCallbackQuery(ctx, callback.ID, "Phiên này đang có yêu cầu xác nhận. Hãy approve, reject hoặc revise trước.")
			return true, nil
		}
	}

	switch action {
	case "select":
		record, err := b.sessionIndex.Select(ctx, chatID, key, time.Now().UTC())
		if err != nil {
			_ = b.answerCallbackQuery(ctx, callback.ID, "Phiên không còn tồn tại.")
			return true, nil
		}
		index, err := b.sessionIndex.List(ctx, chatID, time.Now().UTC())
		if err != nil {
			return false, err
		}
		if err := b.editMessageTextWithReplyMarkup(ctx, chatID, callback.Message.MessageID, telegramSessionListText(index, time.Now()), telegramSessionKeyboard(index, "")); err != nil {
			return false, err
		}
		_ = b.answerCallbackQuery(ctx, callback.ID, "Đã chuyển sang: "+telegramSessionDisplayTitle(record))
		return true, nil
	case "delete":
		index, err := b.sessionIndex.List(ctx, chatID, time.Now().UTC())
		if err != nil {
			return false, err
		}
		if err := b.editMessageTextWithReplyMarkup(ctx, chatID, callback.Message.MessageID, telegramSessionListText(index, time.Now()), telegramSessionKeyboard(index, key)); err != nil {
			return false, err
		}
		_ = b.answerCallbackQuery(ctx, callback.ID, "Xác nhận trước khi xóa.")
		return true, nil
	case "confirm_delete":
		if b.handler == nil {
			return false, fmt.Errorf("message handler is not configured")
		}
		deleted, _, err := b.sessionIndex.Delete(ctx, chatID, key, time.Now().UTC(), func(sessionID string) error {
			return b.handler.ResetSession(ctx, sessionID)
		})
		if err != nil {
			_ = b.answerCallbackQuery(ctx, callback.ID, "Không thể xóa phiên này.")
			return true, nil
		}
		index, err := b.sessionIndex.List(ctx, chatID, time.Now().UTC())
		if err != nil {
			return false, err
		}
		if err := b.editMessageTextWithReplyMarkup(ctx, chatID, callback.Message.MessageID, telegramSessionListText(index, time.Now()), telegramSessionKeyboard(index, "")); err != nil {
			return false, err
		}
		_ = b.answerCallbackQuery(ctx, callback.ID, "Đã xóa: "+telegramSessionDisplayTitle(deleted))
		return true, nil
	case "cancel_delete":
		index, err := b.sessionIndex.List(ctx, chatID, time.Now().UTC())
		if err != nil {
			return false, err
		}
		if err := b.editMessageTextWithReplyMarkup(ctx, chatID, callback.Message.MessageID, telegramSessionListText(index, time.Now()), telegramSessionKeyboard(index, "")); err != nil {
			return false, err
		}
		_ = b.answerCallbackQuery(ctx, callback.ID, "Đã hủy.")
		return true, nil
	default:
		_ = b.answerCallbackQuery(ctx, callback.ID, "Unknown action.")
		return true, nil
	}
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

func (b *Bot) setMyCommands(ctx context.Context) error {
	payload := map[string]any{
		"commands": []map[string]string{
			{"command": "new", "description": "Bắt đầu phiên mới"},
			{"command": "sessions", "description": "Chọn hoặc xóa phiên"},
			{"command": "status", "description": "Xem trạng thái lệnh gần nhất"},
			{"command": "history", "description": "Xem lịch sử gần đây"},
			{"command": "cancel", "description": "Hủy lệnh đang chạy"},
			{"command": "policy", "description": "Mở menu chính sách"},
		},
	}
	var response struct {
		OK bool `json:"ok"`
	}
	_, err := b.doJSON(ctx, http.MethodPost, "/setMyCommands", payload, &response)
	if err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("telegram setMyCommands returned not ok")
	}
	return nil
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

func (b *Bot) sendMarkdownMessage(ctx context.Context, chatID int64, text string) (telegramSentMessage, error) {
	payload := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
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
	disableTelegramLinkPreview(payload)
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

func disableTelegramLinkPreview(payload map[string]any) {
	if payload == nil {
		return
	}
	payload["disable_web_page_preview"] = true
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
		builder.WriteString(telegramRenderInlineMarkdownHTML(telegramNormalizeMarkdownListLine(line)))
	}
	return builder.String()
}

func telegramNormalizeMarkdownListLine(line string) string {
	left := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(left, "- ") {
		return line
	}
	indent := line[:len(line)-len(left)]
	return indent + "• " + strings.TrimSpace(strings.TrimPrefix(left, "- "))
}

func telegramRenderInlineMarkdownHTML(text string) string {
	var builder strings.Builder
	for len(text) > 0 {
		if strings.HasPrefix(text, "`") {
			if end := strings.Index(text[1:], "`"); end >= 0 {
				code := text[1 : 1+end]
				builder.WriteString("<code>")
				builder.WriteString(telegramEscapeHTMLPreservingSpaces(code))
				builder.WriteString("</code>")
				text = text[1+end+1:]
				continue
			}
		}
		if strings.HasPrefix(text, "**") {
			if end := strings.Index(text[2:], "**"); end >= 0 {
				bold := text[2 : 2+end]
				if strings.TrimSpace(bold) != "" {
					builder.WriteString("<b>")
					builder.WriteString(telegramEscapeHTMLPreservingSpaces(bold))
					builder.WriteString("</b>")
					text = text[2+end+2:]
					continue
				}
			}
		}
		if strings.HasPrefix(text, "[") {
			if label, href, rest, ok := consumeMarkdownLink(text); ok {
				builder.WriteString(`<a href="`)
				builder.WriteString(html.EscapeString(href))
				builder.WriteString(`">`)
				builder.WriteString(telegramEscapeHTMLPreservingSpaces(label))
				builder.WriteString("</a>")
				text = rest
				continue
			}
		}

		next := nextMarkdownTokenIndex(text[1:])
		if next < 0 {
			builder.WriteString(telegramEscapeHTMLPreservingSpaces(text))
			break
		}
		next++
		builder.WriteString(telegramEscapeHTMLPreservingSpaces(text[:next]))
		text = text[next:]
	}
	return builder.String()
}

func consumeMarkdownLink(text string) (label string, href string, rest string, ok bool) {
	closeLabel := strings.Index(text, "](")
	if closeLabel <= 0 {
		return "", "", "", false
	}
	closeURL := strings.Index(text[closeLabel+2:], ")")
	if closeURL < 0 {
		return "", "", "", false
	}
	label = text[1:closeLabel]
	href = strings.TrimSpace(text[closeLabel+2 : closeLabel+2+closeURL])
	if strings.TrimSpace(label) == "" || !telegramSafeLinkURL(href) {
		return "", "", "", false
	}
	rest = text[closeLabel+2+closeURL+1:]
	return label, href, rest, true
}

func telegramSafeLinkURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return parsed.Host != ""
	default:
		return false
	}
}

func nextMarkdownTokenIndex(text string) int {
	indices := []int{}
	for _, token := range []string{"`", "**", "["} {
		if index := strings.Index(text, token); index >= 0 {
			indices = append(indices, index)
		}
	}
	if len(indices) == 0 {
		return -1
	}
	sort.Ints(indices)
	return indices[0]
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
	disableTelegramLinkPreview(payload)
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
	Safety   map[string]any
}

func (b *Bot) downloadMessageAttachments(ctx context.Context, message *telegramMessage) ([]downloadedTelegramAttachment, error) {
	if message == nil {
		return nil, nil
	}
	candidates := telegramAttachmentCandidates(message)
	if len(candidates) == 0 {
		return nil, nil
	}
	outputDir := filepath.Join(telegramAttachmentWorkspaceRoot(), "data", "telegram_attachments", safeTelegramID(strconv.FormatInt(message.Chat.ID, 10)), strconv.Itoa(message.MessageID))
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
		quarantinePath := filepath.Join(filesafety.QuarantineDir(outputDir), filename)
		if err := os.MkdirAll(filepath.Dir(quarantinePath), 0o700); err != nil {
			return nil, err
		}
		if err := b.downloadTelegramFile(ctx, filePath, quarantinePath); err != nil {
			return nil, err
		}
		decision, err := filesafety.ScanPath(quarantinePath, filesafety.Input{
			Filename:             filename,
			ClaimedMIME:          candidate.MimeType,
			Origin:               "telegram_attachment",
			SourceTool:           "telegram.downloadAttachment",
			MaxSizeBytes:         filesafety.DefaultMaxSizeBytes,
			AllowInertExecutable: true,
		})
		if err != nil {
			_ = os.Remove(quarantinePath)
			return nil, err
		}
		if !decision.Allowed() {
			_ = os.Remove(quarantinePath)
			return nil, fmt.Errorf("telegram attachment blocked by file safety gate: %s", decision.ReasonUser)
		}
		localPath := filepath.Join(outputDir, filename)
		if err := filesafety.Promote(filesafety.QuarantinedFile{Path: quarantinePath, Dir: filepath.Dir(quarantinePath), Filename: filename}, localPath, decision); err != nil {
			return nil, err
		}
		downloaded = append(downloaded, downloadedTelegramAttachment{
			Path:     localPath,
			Filename: filename,
			MimeType: candidate.MimeType,
			Safety:   decision.Metadata(),
		})
	}
	return downloaded, nil
}

func telegramAttachmentWorkspaceRoot() string {
	workspaceRoot := strings.TrimSpace(os.Getenv("VCLAW_SANDBOX_WORKSPACE_DIR"))
	if workspaceRoot == "" {
		workspaceRoot = ".sandbox-workspace"
	}
	if !filepath.IsAbs(workspaceRoot) {
		if abs, err := filepath.Abs(workspaceRoot); err == nil {
			workspaceRoot = abs
		}
	}
	return filepath.Join(workspaceRoot, sandboxtool.DefaultSessionID, "workspace")
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

const (
	doJSONMaxRetries = 2
	doJSONRetryDelay = 500 * time.Millisecond
)

func (b *Bot) doJSON(ctx context.Context, method, path string, body any, out any) ([]byte, error) {
	var jsonBytes []byte
	if body != nil {
		var err error
		jsonBytes, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	var lastErr error
	for attempt := 0; attempt <= doJSONMaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(doJSONRetryDelay):
			}
			b.logger.Debug("retrying telegram api call after network error",
				"path", path, "attempt", attempt, "error", lastErr)
		}

		var reader io.Reader
		if jsonBytes != nil {
			reader = strings.NewReader(string(jsonBytes))
		}
		request, err := http.NewRequestWithContext(ctx, method, b.apiURL(path), reader)
		if err != nil {
			return nil, err
		}
		if jsonBytes != nil {
			request.Header.Set("Content-Type", "application/json")
		}

		response, err := b.client.Do(request)
		if err != nil {
			if isTelegramNetworkError(err) {
				lastErr = err
				continue
			}
			return nil, fmt.Errorf("telegram api request failed: %s", redactTelegramToken(err.Error(), b.token))
		}

		responseBytes, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			if isTelegramNetworkError(err) {
				lastErr = err
				continue
			}
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

	return nil, fmt.Errorf("telegram api request failed: %s", redactTelegramToken(lastErr.Error(), b.token))
}

func isTelegramNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "forcibly closed") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "eof")
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
	return "Mình không thể hoàn tất bước này vì có lỗi tạm thời. Bạn thử lại giúp mình nhé?"
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

const (
	telegramPolicySettingsCycleAction = "cycle"
	telegramPolicySettingsSaveAction  = "save"
)

func (b *Bot) sendPolicySettingsMenu(ctx context.Context, chatID int64) error {
	assignments := b.currentTelegramPolicyAssignments(chatID)
	b.setTelegramPolicyDraft(chatID, assignments)
	text := telegramPolicySettingsMenuText()
	_, err := b.sendMessageWithReplyMarkup(ctx, chatID, text, telegramPolicySettingsKeyboard(chatID, assignments))
	return err
}

func (b *Bot) currentTelegramPolicyConfig(chatID int64) policies.UserPolicyConfig {
	return policies.EffectivePolicyConfig(b.currentTelegramPolicyConfigLocked(chatID))
}

func (b *Bot) currentTelegramPolicyConfigLocked(chatID int64) policies.UserPolicyConfig {
	b.policyMu.Lock()
	defer b.policyMu.Unlock()
	if draft, ok := b.policyDrafts[chatID]; ok {
		cfg, err := policies.PolicyConfigFromAssignments(draft)
		if err == nil {
			return cfg
		}
	}
	if b.policyStore == nil {
		return policies.UserPolicyConfig{}
	}
	return b.policyStore.Snapshot()
}

func (b *Bot) currentTelegramPolicyAssignments(chatID int64) map[contracts.RiskLevel]policies.PolicyGroup {
	b.policyMu.Lock()
	defer b.policyMu.Unlock()
	if draft, ok := b.policyDrafts[chatID]; ok {
		return cloneTelegramPolicyAssignments(draft)
	}
	if b.policyStore != nil {
		cfg := b.policyStore.Snapshot()
		assignments := policies.EffectivePolicyAssignments(cfg)
		b.policyDrafts[chatID] = cloneTelegramPolicyAssignments(assignments)
		return cloneTelegramPolicyAssignments(assignments)
	}
	assignments := policies.EffectivePolicyAssignments(policies.UserPolicyConfig{})
	b.policyDrafts[chatID] = cloneTelegramPolicyAssignments(assignments)
	return cloneTelegramPolicyAssignments(assignments)
}

func (b *Bot) setTelegramPolicyDraft(chatID int64, assignments map[contracts.RiskLevel]policies.PolicyGroup) {
	b.policyMu.Lock()
	defer b.policyMu.Unlock()
	b.policyDrafts[chatID] = cloneTelegramPolicyAssignments(assignments)
}

func (b *Bot) cycleTelegramPolicySetting(ctx context.Context, chatID int64, messageID int, level contracts.RiskLevel) error {
	assignments := b.currentTelegramPolicyAssignments(chatID)
	current := assignments[level]
	next := policies.PolicyGroupNext(current)
	assignments[level] = next
	b.setTelegramPolicyDraft(chatID, assignments)
	return b.editMessageTextWithReplyMarkup(ctx, chatID, messageID, telegramPolicySettingsMenuText(), telegramPolicySettingsKeyboard(chatID, assignments))
}

func (b *Bot) saveTelegramPolicySettings(ctx context.Context, chatID int64, messageID int) error {
	assignments := b.currentTelegramPolicyAssignments(chatID)
	updated, err := policies.PolicyConfigFromAssignments(assignments)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "destructive cannot be auto_allowed") {
			return b.editPolicySettingsError(ctx, chatID, messageID, "🚫 Xóa dữ liệu không thể để Tự động cho phép.")
		}
		return b.editPolicySettingsError(ctx, chatID, messageID, "⚠️ Cấu hình policy không hợp lệ.")
	}
	if b.policyStore == nil {
		return b.editPolicySettingsError(ctx, chatID, messageID, "⚠️ Chức năng lưu policy chưa được cấu hình.")
	}
	if err := policies.SaveUserPolicyConfig(b.policyStore.Path(), updated); err != nil {
		return b.editPolicySettingsError(ctx, chatID, messageID, "⚠️ Mình chưa lưu được cài đặt policy.")
	}
	if _, err := b.policyStore.Reload(); err != nil {
		return b.editPolicySettingsError(ctx, chatID, messageID, "⚠️ Mình đã lưu được file policy nhưng chưa nạp lại được cấu hình.")
	}
	b.policyMu.Lock()
	delete(b.policyDrafts, chatID)
	b.policyMu.Unlock()
	return b.editMessageTextWithReplyMarkup(ctx, chatID, messageID, telegramPolicySettingsSavedText(), telegramEmptyInlineKeyboard())
}

func (b *Bot) editPolicySettingsError(ctx context.Context, chatID int64, messageID int, text string) error {
	assignments := b.currentTelegramPolicyAssignments(chatID)
	return b.editMessageTextWithReplyMarkup(ctx, chatID, messageID, text, telegramPolicySettingsKeyboard(chatID, assignments))
}

func telegramPolicySettingsMenuText() string {
	return "Chạm vào một nút để đổi nhóm cho từng mức rủi ro."
}

func telegramPolicySettingsSavedText() string {
	return "✅ Đã lưu cài đặt."
}

func telegramPolicySettingsKeyboard(chatID int64, assignments map[contracts.RiskLevel]policies.PolicyGroup) map[string]any {
	rows := make([][]map[string]string, 0, len(policies.RiskLevelOrder())+1)
	for _, level := range policies.RiskLevelOrder() {
		rows = append(rows, []map[string]string{
			{
				"text":          telegramPolicySettingsButtonText(level, assignments[level]),
				"callback_data": telegramPolicySettingsCallbackData(telegramPolicySettingsCycleAction, chatID, level),
			},
		})
	}
	rows = append(rows, []map[string]string{{
		"text":          "Lưu",
		"callback_data": telegramPolicySettingsCallbackData(telegramPolicySettingsSaveAction, chatID, ""),
	}})
	return map[string]any{"inline_keyboard": rows}
}

func telegramPolicySettingsButtonText(level contracts.RiskLevel, group policies.PolicyGroup) string {
	return fmt.Sprintf("%s\n%s %s", telegramPolicyRiskLevelLabel(level), telegramPolicyGroupEmoji(group), policies.PolicyGroupLabel(group))
}

func telegramPolicyRiskLevelLabel(level contracts.RiskLevel) string {
	switch level {
	case contracts.RiskLevelSafeRead:
		return "Xem danh sách & thông tin tổng quan"
	case contracts.RiskLevelSafeCompute:
		return "Tóm tắt, phân tích nội dung"
	case contracts.RiskLevelSensitiveRead:
		return "Đọc nội dung riêng tư"
	case contracts.RiskLevelExternalWrite:
		return "Tạo, chỉnh sửa & gửi đi"
	case contracts.RiskLevelLocalWrite:
		return "Tải & lưu file về máy"
	case contracts.RiskLevelCodeExecution:
		return "Chạy lệnh hệ thống"
	case contracts.RiskLevelDestructive:
		return "Xóa dữ liệu"
	default:
		return policies.RiskLevelLabel(level)
	}
}

func telegramPolicyGroupEmoji(group policies.PolicyGroup) string {
	switch group {
	case policies.PolicyGroupAutoAllow:
		return "✅"
	case policies.PolicyGroupRequireApprove:
		return "👤"
	case policies.PolicyGroupAlwaysBlock:
		return "🚫"
	default:
		return "•"
	}
}

func telegramEmptyInlineKeyboard() map[string]any {
	return map[string]any{"inline_keyboard": [][]map[string]string{}}
}

func telegramPolicySettingsCallbackData(action string, chatID int64, riskLevel contracts.RiskLevel) string {
	parts := []string{"vclaw", "policy", strings.TrimSpace(action), strconv.FormatInt(chatID, 10)}
	if strings.TrimSpace(string(riskLevel)) != "" {
		parts = append(parts, string(riskLevel))
	}
	return strings.Join(parts, ":")
}

func parseTelegramPolicySettingsCallback(data string) (action string, chatID int64, riskLevel contracts.RiskLevel, ok bool) {
	parts := strings.Split(strings.TrimSpace(data), ":")
	if len(parts) < 4 || parts[0] != "vclaw" || parts[1] != "policy" {
		return "", 0, "", false
	}
	action = strings.TrimSpace(parts[2])
	switch action {
	case telegramPolicySettingsCycleAction:
		if len(parts) != 5 {
			return "", 0, "", false
		}
		id, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return "", 0, "", false
		}
		return action, id, contracts.RiskLevel(strings.TrimSpace(parts[4])), true
	case telegramPolicySettingsSaveAction:
		if len(parts) != 4 {
			return "", 0, "", false
		}
		id, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return "", 0, "", false
		}
		return action, id, "", true
	default:
		return "", 0, "", false
	}
}

func cloneTelegramPolicyAssignments(assignments map[contracts.RiskLevel]policies.PolicyGroup) map[contracts.RiskLevel]policies.PolicyGroup {
	if len(assignments) == 0 {
		return nil
	}
	clone := make(map[contracts.RiskLevel]policies.PolicyGroup, len(assignments))
	for level, group := range assignments {
		clone[level] = group
	}
	return clone
}
