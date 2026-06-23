package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vclaw/internal/providers"
)

const (
	telegramSessionIndexVersion = 1
	telegramSessionTitleRunes   = 60
)

type telegramSessionIndexStore struct {
	dataDir string
	mu      sync.Mutex
}

type telegramSessionIndex struct {
	Version          int                     `json:"version"`
	ActiveSessionKey string                  `json:"activeSessionKey"`
	Sessions         []telegramSessionRecord `json:"sessions"`
}

type telegramSessionRecord struct {
	Key          string     `json:"key"`
	SessionID    string     `json:"sessionId"`
	Title        string     `json:"title"`
	FirstMessage string     `json:"firstMessage,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	DeletedAt    *time.Time `json:"deletedAt,omitempty"`
}

func newTelegramSessionIndexStore(dataDir string) *telegramSessionIndexStore {
	return &telegramSessionIndexStore{dataDir: dataDir}
}

func (s *telegramSessionIndexStore) Active(ctx context.Context, chatID int64, now time.Time) (telegramSessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index, err := s.loadOrCreateLocked(ctx, chatID, now)
	if err != nil {
		return telegramSessionRecord{}, err
	}
	if record, ok := activeTelegramSession(index); ok {
		return record, nil
	}
	record := newTelegramSessionRecord(chatID, now, len(index.Sessions))
	index.Sessions = append(index.Sessions, record)
	index.ActiveSessionKey = record.Key
	if err := s.saveLocked(chatID, index); err != nil {
		return telegramSessionRecord{}, err
	}
	return record, nil
}

func (s *telegramSessionIndexStore) Create(ctx context.Context, chatID int64, now time.Time) (telegramSessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index, err := s.loadOrCreateLocked(ctx, chatID, now)
	if err != nil {
		return telegramSessionRecord{}, err
	}
	record := newTelegramSessionRecord(chatID, now, len(index.Sessions))
	index.Sessions = append(index.Sessions, record)
	index.ActiveSessionKey = record.Key
	if err := s.saveLocked(chatID, index); err != nil {
		return telegramSessionRecord{}, err
	}
	return record, nil
}

func (s *telegramSessionIndexStore) List(ctx context.Context, chatID int64, now time.Time) (telegramSessionIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index, err := s.loadOrCreateLocked(ctx, chatID, now)
	if err != nil {
		return telegramSessionIndex{}, err
	}
	return sortedTelegramSessionIndex(index), nil
}

func (s *telegramSessionIndexStore) Select(ctx context.Context, chatID int64, key string, now time.Time) (telegramSessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index, err := s.loadOrCreateLocked(ctx, chatID, now)
	if err != nil {
		return telegramSessionRecord{}, err
	}
	for i := range index.Sessions {
		if index.Sessions[i].Key == key && index.Sessions[i].DeletedAt == nil {
			index.Sessions[i].UpdatedAt = now
			index.ActiveSessionKey = key
			if err := s.saveLocked(chatID, index); err != nil {
				return telegramSessionRecord{}, err
			}
			return index.Sessions[i], nil
		}
	}
	return telegramSessionRecord{}, fmt.Errorf("telegram session not found")
}

func (s *telegramSessionIndexStore) Touch(ctx context.Context, chatID int64, sessionID string, text string, now time.Time) error {
	title := telegramSessionTitleFromMessage(text)
	s.mu.Lock()
	defer s.mu.Unlock()

	index, err := s.loadOrCreateLocked(ctx, chatID, now)
	if err != nil {
		return err
	}
	changed := false
	for i := range index.Sessions {
		if index.Sessions[i].SessionID != sessionID || index.Sessions[i].DeletedAt != nil {
			continue
		}
		index.Sessions[i].UpdatedAt = now
		if index.Sessions[i].FirstMessage == "" && title != "" {
			index.Sessions[i].FirstMessage = strings.TrimSpace(text)
			index.Sessions[i].Title = title
		}
		changed = true
		break
	}
	if !changed {
		return nil
	}
	return s.saveLocked(chatID, index)
}

func (s *telegramSessionIndexStore) Delete(ctx context.Context, chatID int64, key string, now time.Time, clear func(string) error) (telegramSessionRecord, telegramSessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index, err := s.loadOrCreateLocked(ctx, chatID, now)
	if err != nil {
		return telegramSessionRecord{}, telegramSessionRecord{}, err
	}
	deleteIndex := -1
	for i := range index.Sessions {
		if index.Sessions[i].Key == key && index.Sessions[i].DeletedAt == nil {
			deleteIndex = i
			break
		}
	}
	if deleteIndex < 0 {
		return telegramSessionRecord{}, telegramSessionRecord{}, fmt.Errorf("telegram session not found")
	}
	deleted := index.Sessions[deleteIndex]
	if clear != nil {
		if err := clear(deleted.SessionID); err != nil {
			return telegramSessionRecord{}, telegramSessionRecord{}, err
		}
	}
	index.Sessions[deleteIndex].DeletedAt = &now
	index.Sessions[deleteIndex].UpdatedAt = now

	var next telegramSessionRecord
	if index.ActiveSessionKey == key {
		if record, ok := newestLiveTelegramSession(index); ok {
			index.ActiveSessionKey = record.Key
			next = record
		} else {
			next = newTelegramSessionRecord(chatID, now, len(index.Sessions))
			index.Sessions = append(index.Sessions, next)
			index.ActiveSessionKey = next.Key
		}
	} else if record, ok := activeTelegramSession(index); ok {
		next = record
	}
	if err := s.saveLocked(chatID, index); err != nil {
		return telegramSessionRecord{}, telegramSessionRecord{}, err
	}
	return deleted, next, nil
}

func (s *telegramSessionIndexStore) loadOrCreateLocked(_ context.Context, chatID int64, now time.Time) (telegramSessionIndex, error) {
	path := s.indexPath(chatID)
	data, err := os.ReadFile(path)
	if err == nil {
		var index telegramSessionIndex
		if err := json.Unmarshal(data, &index); err != nil {
			return telegramSessionIndex{}, err
		}
		if index.Version == 0 {
			index.Version = telegramSessionIndexVersion
		}
		if _, ok := activeTelegramSession(index); ok {
			return index, nil
		}
		if record, ok := newestLiveTelegramSession(index); ok {
			index.ActiveSessionKey = record.Key
			return index, s.saveLocked(chatID, index)
		}
		record := newTelegramSessionRecord(chatID, now, len(index.Sessions))
		index.Sessions = append(index.Sessions, record)
		index.ActiveSessionKey = record.Key
		return index, s.saveLocked(chatID, index)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return telegramSessionIndex{}, err
	}
	index := telegramSessionIndex{
		Version:          telegramSessionIndexVersion,
		ActiveSessionKey: "legacy",
		Sessions: []telegramSessionRecord{
			s.legacySession(chatID, now),
		},
	}
	return index, s.saveLocked(chatID, index)
}

func (s *telegramSessionIndexStore) legacySession(chatID int64, now time.Time) telegramSessionRecord {
	sessionID := telegramLegacySessionID(chatID)
	title, first, updated := s.legacySessionTitle(sessionID)
	if title == "" {
		title = "Phiên cũ"
	}
	if updated.IsZero() {
		updated = now
	}
	return telegramSessionRecord{
		Key:          "legacy",
		SessionID:    sessionID,
		Title:        title,
		FirstMessage: first,
		CreatedAt:    updated,
		UpdatedAt:    updated,
	}
}

func (s *telegramSessionIndexStore) legacySessionTitle(sessionID string) (string, string, time.Time) {
	path := filepath.Join(s.dataDir, "sessions", sessionID, "transcript.json")
	stat, _ := os.Stat(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if stat != nil {
			return "", "", stat.ModTime().UTC()
		}
		return "", "", time.Time{}
	}
	var messages []providers.Message
	if err := json.Unmarshal(data, &messages); err != nil {
		if stat != nil {
			return "", "", stat.ModTime().UTC()
		}
		return "", "", time.Time{}
	}
	for _, message := range messages {
		if message.Role != providers.MessageRoleUser {
			continue
		}
		title := telegramSessionTitleFromMessage(message.Content)
		if title != "" {
			updated := time.Time{}
			if stat != nil {
				updated = stat.ModTime().UTC()
			}
			return title, strings.TrimSpace(message.Content), updated
		}
	}
	updated := time.Time{}
	if stat != nil {
		updated = stat.ModTime().UTC()
	}
	return "", "", updated
}

func (s *telegramSessionIndexStore) saveLocked(chatID int64, index telegramSessionIndex) error {
	index.Version = telegramSessionIndexVersion
	return telegramAtomicWriteJSON(s.indexPath(chatID), index)
}

func (s *telegramSessionIndexStore) indexPath(chatID int64) string {
	return filepath.Join(s.dataDir, "telegram_sessions", strconv.FormatInt(chatID, 10)+".json")
}

func telegramLegacySessionID(chatID int64) string {
	return fmt.Sprintf("telegram_chat_%d", chatID)
}

func newTelegramSessionRecord(chatID int64, now time.Time, ordinal int) telegramSessionRecord {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	key := telegramSessionKey(chatID, now, ordinal)
	return telegramSessionRecord{
		Key:       key,
		SessionID: fmt.Sprintf("telegram_chat_%d_%s_%s", chatID, now.UTC().Format("20060102T150405"), key),
		Title:     "Phiên mới",
		CreatedAt: now.UTC(),
		UpdatedAt: now.UTC(),
	}
}

func telegramSessionKey(chatID int64, now time.Time, ordinal int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%d", chatID, now.UnixNano(), ordinal)))
	return hex.EncodeToString(sum[:5])
}

func activeTelegramSession(index telegramSessionIndex) (telegramSessionRecord, bool) {
	for _, record := range index.Sessions {
		if record.Key == index.ActiveSessionKey && record.DeletedAt == nil {
			return record, true
		}
	}
	return telegramSessionRecord{}, false
}

func newestLiveTelegramSession(index telegramSessionIndex) (telegramSessionRecord, bool) {
	live := liveTelegramSessions(index)
	if len(live) == 0 {
		return telegramSessionRecord{}, false
	}
	sort.Slice(live, func(i, j int) bool {
		return live[i].UpdatedAt.After(live[j].UpdatedAt)
	})
	return live[0], true
}

func sortedTelegramSessionIndex(index telegramSessionIndex) telegramSessionIndex {
	index.Sessions = liveTelegramSessions(index)
	sort.SliceStable(index.Sessions, func(i, j int) bool {
		if index.Sessions[i].Key == index.ActiveSessionKey {
			return true
		}
		if index.Sessions[j].Key == index.ActiveSessionKey {
			return false
		}
		return index.Sessions[i].UpdatedAt.After(index.Sessions[j].UpdatedAt)
	})
	return index
}

func liveTelegramSessions(index telegramSessionIndex) []telegramSessionRecord {
	out := make([]telegramSessionRecord, 0, len(index.Sessions))
	for _, record := range index.Sessions {
		if record.DeletedAt == nil {
			out = append(out, record)
		}
	}
	return out
}

func telegramSessionTitleFromMessage(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "/") {
		return ""
	}
	if strings.EqualFold(text, "User sent an attachment.") {
		return "Tệp đính kèm"
	}
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(strings.ToLower(line), "attachment paths:") {
			continue
		}
		lines = append(lines, line)
	}
	text = strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) > telegramSessionTitleRunes {
		text = strings.TrimSpace(string(runes[:telegramSessionTitleRunes])) + "..."
	}
	return text
}

func telegramAtomicWriteJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func telegramSessionListText(index telegramSessionIndex, now time.Time) string {
	if len(index.Sessions) == 0 {
		return "Chưa có phiên nào."
	}
	var lines []string
	lines = append(lines, "Các phiên hội thoại")
	for i, record := range index.Sessions {
		prefix := fmt.Sprintf("%d.", i+1)
		if record.Key == index.ActiveSessionKey {
			prefix += " Đang dùng:"
		}
		lines = append(lines, fmt.Sprintf("%s %s · %s", prefix, telegramSessionDisplayTitle(record), telegramSessionDisplayTime(record.UpdatedAt, now)))
	}
	return strings.Join(lines, "\n")
}

func telegramSessionKeyboard(index telegramSessionIndex, confirmDeleteKey string) map[string]any {
	var rows [][]map[string]string
	for _, record := range index.Sessions {
		if record.DeletedAt != nil {
			continue
		}
		if confirmDeleteKey == record.Key {
			rows = append(rows, []map[string]string{
				{"text": "Xóa phiên này", "callback_data": telegramSessionCallbackData("confirm_delete", record.Key)},
				{"text": "Hủy", "callback_data": telegramSessionCallbackData("cancel_delete", record.Key)},
			})
			continue
		}
		title := telegramSessionDisplayTitle(record)
		if record.Key == index.ActiveSessionKey {
			title = "✓ " + title
		}
		rows = append(rows, []map[string]string{
			{"text": title, "callback_data": telegramSessionCallbackData("select", record.Key)},
			{"text": "Xóa", "callback_data": telegramSessionCallbackData("delete", record.Key)},
		})
	}
	return map[string]any{"inline_keyboard": rows}
}

func telegramSessionDisplayTitle(record telegramSessionRecord) string {
	title := strings.TrimSpace(record.Title)
	if title == "" {
		title = "Phiên không tên"
	}
	return title
}

func telegramSessionDisplayTime(t time.Time, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	if now.IsZero() {
		now = time.Now()
	}
	local := t.Local()
	today := now.Local()
	if sameTelegramSessionDay(local, today) {
		return "hôm nay " + local.Format("15:04")
	}
	if sameTelegramSessionDay(local, today.AddDate(0, 0, -1)) {
		return "hôm qua " + local.Format("15:04")
	}
	return local.Format("02/01 15:04")
}

func sameTelegramSessionDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func telegramSessionCallbackData(action string, key string) string {
	action = strings.TrimSpace(action)
	key = strings.TrimSpace(key)
	if key == "" {
		return "vclaw:session:" + action
	}
	return "vclaw:session:" + action + ":" + key
}

func parseTelegramSessionCallback(data string) (action string, key string, ok bool) {
	parts := strings.Split(strings.TrimSpace(data), ":")
	if len(parts) < 3 || parts[0] != "vclaw" || parts[1] != "session" {
		return "", "", false
	}
	action = strings.TrimSpace(parts[2])
	if len(parts) > 3 {
		key = strings.TrimSpace(parts[3])
	}
	return action, key, action != ""
}
