package sessions

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"vclaw/internal/providers"
)

const (
	defaultCompactionThresholdRatio = 0.80
	defaultKeepLastMessages         = 10
	defaultContextWindow            = 128_000
	defaultMaxSummaryTokens         = 2048
)

// CompactorConfig holds tunable parameters for the compactor.
type CompactorConfig struct {
	// SummarizeModel is the LLM model used for generating summaries.
	// Should be a cheaper model than the main agent model (e.g. gpt-4o-mini).
	// Falls back to the provider's default if empty.
	SummarizeModel string

	// ThresholdRatio triggers compaction when estimated tokens exceed
	// ContextWindow * ThresholdRatio. Default: 0.80.
	ThresholdRatio float64

	// KeepLastMessages is the number of recent messages preserved verbatim
	// after truncation. Default: 6 (3 user+assistant turns).
	KeepLastMessages int

	// ContextWindow is the LLM context window size in tokens.
	// Default: 128_000 (safe for gpt-4o / claude-3.5).
	ContextWindow int
}

// CompactorGuard carries runtime callbacks that the compactor checks before
// truncating a session transcript.
// Compaction is skipped entirely if any guard returns true, preventing loss
// of context for in-flight approval or clarification flows.
type CompactorGuard struct {
	// HasPendingApproval returns true when the session has a pending HITL
	// approval that has not yet been resolved. Compaction is skipped so that
	// the tool_call / ACTION_REQUIRES_APPROVAL messages remain in context
	// when the user responds to the approval prompt.
	HasPendingApproval func(sessionID string) bool

	// HasPendingClarification returns true when the session has an active
	// clarification question waiting for a user answer.
	HasPendingClarification func(sessionID string) bool
}

// CompactionResult describes what the compactor did.
type CompactionResult struct {
	// Compacted is true when the transcript was actually summarized and truncated.
	Compacted bool

	// Summary is the LLM-generated summary that replaces the older messages.
	// Only set when Compacted is true.
	Summary string

	// KeptMessages are the verbatim recent messages that survived truncation.
	// Only set when Compacted is true.
	KeptMessages []providers.Message

	// SkipReason explains why compaction was skipped when Compacted is false.
	SkipReason string
}

// Compactor manages LLM-based session summarization and transcript truncation.
// It is safe for concurrent use across sessions; each session has its own mutex
// to prevent double-compaction while an async summarization is in progress.
type Compactor struct {
	provider providers.Provider
	config   CompactorConfig
	mu       sync.Map // map[sessionID string] -> *sync.Mutex
	logger   *slog.Logger
}

// NewCompactor creates a Compactor. provider must not be nil.
func NewCompactor(provider providers.Provider, config CompactorConfig, logger *slog.Logger) *Compactor {
	if config.ThresholdRatio <= 0 {
		config.ThresholdRatio = defaultCompactionThresholdRatio
	}
	if config.KeepLastMessages <= 0 {
		config.KeepLastMessages = defaultKeepLastMessages
	}
	if config.ContextWindow <= 0 {
		config.ContextWindow = defaultContextWindow
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Compactor{
		provider: provider,
		config:   config,
		logger:   logger,
	}
}

// MaybeCompact checks whether the transcript needs compaction and, if so,
// calls the LLM to generate a summary and returns the truncated message list.
//
// The caller is responsible for persisting the result:
//
//	if result.Compacted {
//	    store.SetTranscript(ctx, sessionID, result.KeptMessages)
//	    memory.Summary = result.Summary
//	    store.SaveMemory(ctx, sessionID, memory)
//	}
func (c *Compactor) MaybeCompact(
	ctx context.Context,
	sessionID string,
	transcript []providers.Message,
	memory SessionMemory,
	guard CompactorGuard,
) (CompactionResult, error) {
	// --- Guard: skip when approval or clarification is in flight ---
	if guard.HasPendingApproval != nil && guard.HasPendingApproval(sessionID) {
		c.logger.Debug("compaction skipped: pending approval", "session_id", sessionID)
		return CompactionResult{SkipReason: "pending_approval"}, nil
	}
	if guard.HasPendingClarification != nil && guard.HasPendingClarification(sessionID) {
		c.logger.Debug("compaction skipped: pending clarification", "session_id", sessionID)
		return CompactionResult{SkipReason: "pending_clarification"}, nil
	}

	// --- Check token threshold ---
	threshold := int(float64(c.config.ContextWindow) * c.config.ThresholdRatio)
	estimated := EstimateMessagesTokens(transcript)
	if estimated < threshold {
		return CompactionResult{SkipReason: "below_threshold"}, nil
	}

	// --- Per-session lock: skip if already compacting ---
	mu := c.sessionMutex(sessionID)
	if !mu.TryLock() {
		c.logger.Debug("compaction skipped: already in progress", "session_id", sessionID)
		return CompactionResult{SkipReason: "already_compacting"}, nil
	}
	defer mu.Unlock()

	keepLast := c.config.KeepLastMessages
	if len(transcript) <= keepLast {
		return CompactionResult{SkipReason: "too_few_messages"}, nil
	}

	toSummarize := transcript[:len(transcript)-keepLast]
	kept := cloneMessages(transcript[len(transcript)-keepLast:])

	// --- Build prompt and call LLM ---
	userPrompt := buildCompactionUserPrompt(toSummarize, memory)
	model := strings.TrimSpace(c.config.SummarizeModel)

	resp, err := c.provider.Generate(ctx, &providers.GenerateRequest{
		SystemPrompt: compactionSystemPrompt(),
		UserPrompt:   userPrompt,
		Temperature:  0.3,
		MaxTokens:    defaultMaxSummaryTokens,
		Model:        model,
	})
	if err != nil {
		return CompactionResult{}, fmt.Errorf("compaction llm call: %w", err)
	}

	newSummary := strings.TrimSpace(resp.Text)
	if newSummary == "" {
		return CompactionResult{SkipReason: "empty_summary"}, nil
	}

	// Merge with existing summary (cumulative — new summary appends, not replaces)
	if existing := strings.TrimSpace(memory.Summary); existing != "" {
		newSummary = existing + "\n\n" + newSummary
	}

	c.logger.Info("session compacted",
		"session_id", sessionID,
		"original_messages", len(transcript),
		"kept_messages", len(kept),
		"estimated_tokens_before", estimated,
	)

	return CompactionResult{
		Compacted:    true,
		Summary:      newSummary,
		KeptMessages: kept,
	}, nil
}

// compactionSystemPrompt returns the system instruction for the summarization LLM call.
func compactionSystemPrompt() string {
	return strings.TrimSpace(`Tóm tắt hội thoại này ngắn gọn để AI agent có thể tiếp tục làm việc.

BẮT BUỘC GIỮ LẠI:
- Tác vụ đang thực hiện và trạng thái hiện tại (đang làm gì, đã xong chưa)
- Câu hỏi đang chờ người dùng trả lời (nếu có) — KHÔNG được bỏ qua
- Thông tin quan trọng người dùng đã cung cấp (email, tên, thời gian, địa điểm)
- Yêu cầu cuối cùng của người dùng và trạng thái xử lý
- Quyết định đã thống nhất và lý do

ƯU TIÊN nội dung gần đây hơn nội dung cũ.
Giữ lại mọi email, tên người, ngày giờ cụ thể NGUYÊN VẸN — không viết tắt hay đổi định dạng.
Trả lời bằng tiếng Việt.`)
}

// buildCompactionUserPrompt assembles the conversation text to be summarized,
// including any pending clarification state that must survive truncation.
func buildCompactionUserPrompt(messages []providers.Message, memory SessionMemory) string {
	var b strings.Builder

	if existing := strings.TrimSpace(memory.Summary); existing != "" {
		b.WriteString("Tóm tắt cũ (từ trước):\n")
		b.WriteString(existing)
		b.WriteString("\n\nHội thoại mới cần tóm tắt thêm:\n")
	} else {
		b.WriteString("Hội thoại cần tóm tắt:\n")
	}

	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		switch msg.Role {
		case providers.MessageRoleUser:
			b.WriteString("User: ")
			b.WriteString(content)
			b.WriteString("\n")
		case providers.MessageRoleAssistant:
			if len(msg.ToolCalls) == 0 {
				b.WriteString("Assistant: ")
				b.WriteString(content)
				b.WriteString("\n")
			}
		}
	}

	// Append pending clarification so the summary preserves the waiting state.
	if p := memory.PendingClarification; p != nil {
		if strings.TrimSpace(p.Question) != "" || strings.TrimSpace(p.OriginalRequest) != "" {
			b.WriteString("\n--- TRẠNG THÁI ĐANG CHỜ NGƯỜI DÙNG TRẢ LỜI ---\n")
			if strings.TrimSpace(p.OriginalRequest) != "" {
				b.WriteString("Yêu cầu gốc: ")
				b.WriteString(p.OriginalRequest)
				b.WriteString("\n")
			}
			if strings.TrimSpace(p.Question) != "" {
				b.WriteString("Câu hỏi đang chờ: ")
				b.WriteString(p.Question)
				b.WriteString("\n")
			}
			b.WriteString("LƯU Ý: Bắt buộc giữ thông tin trạng thái này trong tóm tắt.\n")
		}
	}

	return b.String()
}

func (c *Compactor) sessionMutex(sessionID string) *sync.Mutex {
	v, _ := c.mu.LoadOrStore(sessionID, &sync.Mutex{})
	return v.(*sync.Mutex)
}
