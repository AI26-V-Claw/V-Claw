package agent

import (
	"context"
	"fmt"
	"strings"

	"vclaw/internal/providers"
	"vclaw/internal/sessions"
)

type memoryMode string

const (
	memoryModeFresh       memoryMode = "fresh"         // self-contained new request, safe to isolate
	memoryModeNeedsContext memoryMode = "needs_context" // references prior conversation, must keep transcript
)

// classifyMemoryMode determines whether the incoming message is a self-contained
// request or depends on prior conversation context.
//
// When the message contains write-action keywords (xóa, gửi, etc.) the
// heuristic alone cannot tell whether it is a fresh request ("gửi email cho
// boss@company.com") or a follow-up that references a previous list
// ("Xóa số 1"). The LLM resolves the ambiguity; heuristic is the fallback.
func (r *Runtime) classifyMemoryMode(ctx context.Context, text string, recentHistory []string) memoryMode {
	// Only worth classifying when the heuristic would trigger isolation.
	// For all other messages isolation never fires, so the mode is irrelevant.
	if !isPotentialWriteRequest(text) {
		return memoryModeFresh
	}
	// Short-circuit for explicit ordinal references — always needs context.
	if isOrdinalActionReference(text) {
		return memoryModeNeedsContext
	}
	if r.provider == nil || r.memoryClassifierModel == "" {
		return heuristicMemoryMode(text)
	}
	mode, err := r.llmMemoryMode(ctx, text, recentHistory)
	if err != nil {
		r.logger.Debug("memory mode llm classifier failed, using heuristic fallback", "error", err)
		return heuristicMemoryMode(text)
	}
	return mode
}

func (r *Runtime) llmMemoryMode(ctx context.Context, text string, recentHistory []string) (memoryMode, error) {
	historyText := "none"
	if len(recentHistory) > 0 {
		historyText = strings.Join(recentHistory, "\n")
	}

	systemPrompt := `You are a memory-mode classifier for a personal AI assistant.
Decide whether the user's message is a FRESH standalone request or NEEDS_CONTEXT from prior conversation.

Reply with exactly one word: "fresh" or "needs_context".

Rules:
- "needs_context": message references something from history — a numbered item (số 1, số 2, #1, cái đầu tiên, cái thứ hai, mục 1…), a pronoun ("nó", "đó", "cái đó", "this one"), or a follow-up continuation ("gửi nó đi", "xóa cái đó").
- "fresh": message is self-contained — all parameters are given explicitly (recipient, title, time, content…) without depending on prior context.
- When in doubt and history is present, prefer "needs_context".`

	userPrompt := fmt.Sprintf("History:\n%s\n\nUser message: %s", historyText, text)

	resp, err := r.provider.Generate(ctx, &providers.GenerateRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		Temperature:  0.0,
		MaxTokens:    5,
		Model:        r.memoryClassifierModel,
	})
	if err != nil {
		return "", err
	}

	result := strings.TrimSpace(strings.ToLower(resp.Text))
	if strings.Contains(result, "needs_context") || strings.Contains(result, "needs context") {
		return memoryModeNeedsContext, nil
	}
	if strings.Contains(result, "fresh") {
		return memoryModeFresh, nil
	}
	return "", fmt.Errorf("unexpected memory mode response: %q", resp.Text)
}

// heuristicMemoryMode is the keyword-based fallback used when the LLM call fails.
func heuristicMemoryMode(text string) memoryMode {
	if isOrdinalActionReference(text) {
		return memoryModeNeedsContext
	}
	if isLikelyContextualFollowUpQuestion(text, nil, sessions.SessionMemory{}) {
		return memoryModeNeedsContext
	}
	return memoryModeFresh
}
