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
	defaultKeepTokenRatio           = 0.20
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

	// KeepTokenRatio is the fraction of ContextWindow to preserve verbatim
	// (walking backwards from the most recent message). Default: 0.20.
	// When set, this takes precedence over KeepLastMessages.
	// Example: 0.20 with a 128k context window keeps the last ~25,600 tokens.
	KeepTokenRatio float64

	// KeepLastMessages is the fallback number of recent messages to preserve
	// verbatim when KeepTokenRatio is 0. Default: 10.
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

	// Stats holds token and message counts for observability. Always populated
	// when Compacted is true — use these to compare token-based vs message-based
	// compaction performance.
	Stats CompactionStats
}

// CompactionStats holds observability data about a compaction run.
type CompactionStats struct {
	// Strategy is "token" or "message" depending on which KeepToken/KeepLastMessages was used.
	Strategy string

	// TokensBefore is the estimated total tokens of the transcript before compaction.
	TokensBefore int

	// KeptTokens is the estimated tokens of the messages preserved verbatim.
	KeptTokens int

	// SummarizedMessages is the number of messages that were fed to the summarizer.
	SummarizedMessages int

	// KeptMessageCount is the number of messages preserved verbatim.
	KeptMessageCount int
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
	if config.KeepTokenRatio <= 0 {
		config.KeepTokenRatio = defaultKeepTokenRatio
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

	// --- Split transcript into toSummarize and kept ---
	splitIdx, strategy := c.splitTranscript(transcript)
	if splitIdx <= 0 {
		return CompactionResult{SkipReason: "too_few_messages"}, nil
	}

	toSummarize := transcript[:splitIdx]
	kept := cloneMessages(transcript[splitIdx:])

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

	stats := CompactionStats{
		Strategy:           strategy,
		TokensBefore:       estimated,
		KeptTokens:         EstimateMessagesTokens(kept),
		SummarizedMessages: len(toSummarize),
		KeptMessageCount:   len(kept),
	}

	c.logger.Info("session compacted",
		"session_id", sessionID,
		"strategy", strategy,
		"original_messages", len(transcript),
		"summarized_messages", stats.SummarizedMessages,
		"kept_messages", stats.KeptMessageCount,
		"tokens_before", stats.TokensBefore,
		"kept_tokens", stats.KeptTokens,
	)

	return CompactionResult{
		Compacted:    true,
		Summary:      newSummary,
		KeptMessages: kept,
		Stats:        stats,
	}, nil
}

// splitTranscript returns the index that divides transcript into
// [toSummarize : kept] and the strategy name used.
//
// When KeepTokenRatio > 0, it walks backwards from the end accumulating
// tokens until the budget (ContextWindow * KeepTokenRatio) is exhausted —
// this is the "token" strategy.
//
// When KeepTokenRatio == 0, it falls back to keeping the last
// KeepLastMessages messages — this is the "message" strategy.
func (c *Compactor) splitTranscript(transcript []providers.Message) (splitIdx int, strategy string) {
	if c.config.KeepTokenRatio > 0 {
		budget := int(float64(c.config.ContextWindow) * c.config.KeepTokenRatio)
		idx := keepByTokenBudget(transcript, budget)
		if idx > 0 {
			return idx, "token"
		}
		// Budget covers the whole transcript — fall through to message fallback.
	}

	keepLast := c.config.KeepLastMessages
	if len(transcript) <= keepLast {
		return 0, "message"
	}
	return len(transcript) - keepLast, "message"
}

// keepByTokenBudget walks backwards through messages, accumulating tokens
// until budget is exhausted, and returns the split index such that
// transcript[splitIdx:] fits within budget.
// Returns 0 if even the whole transcript fits (nothing to summarize).
func keepByTokenBudget(transcript []providers.Message, budget int) int {
	accumulated := 0
	for i := len(transcript) - 1; i >= 0; i-- {
		accumulated += EstimateMessagesTokens(transcript[i : i+1])
		if accumulated > budget {
			// transcript[i+1:] is within budget; transcript[:i+1] gets summarized.
			return i + 1
		}
	}
	return 0
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
