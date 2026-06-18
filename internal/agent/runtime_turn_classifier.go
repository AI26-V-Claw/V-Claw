package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"vclaw/internal/providers"
	"vclaw/internal/sessions"
)

type memoryMode string

const (
	memoryModeFresh        memoryMode = "fresh"         // self-contained new request, safe to isolate
	memoryModeNeedsContext memoryMode = "needs_context" // references prior conversation, must keep transcript
)

// heuristicMemoryMode is the keyword-based fallback for memoryMode classification.
func heuristicMemoryMode(text string) memoryMode {
	if isOrdinalActionReference(text) {
		return memoryModeNeedsContext
	}
	if isLikelyContextualFollowUpQuestion(text, nil, sessions.SessionMemory{}) {
		return memoryModeNeedsContext
	}
	return memoryModeFresh
}

// turnAnalysis is the result of a single pre-turn LLM call that classifies
// the semantic meaning of the user's message. Replaces the individual keyword
// heuristics: isLikelyClarificationAnswer, isLikelyResultFollowUpQuestion,
// isLikelyContextualFollowUpQuestion, and classifyMemoryMode.
type turnAnalysis struct {
	// MemoryMode is only meaningful when the input had CheckMemoryMode=true.
	MemoryMode memoryMode

	ShortLabel string
	Category   string

	// IsClarificationAnswer is true when the message answers an active clarification question.
	IsClarificationAnswer bool

	// IsResultFollowUp is true when the message asks about or continues a recent action result.
	IsResultFollowUp bool

	// IsContextualFollowUp is true when the message references or continues prior conversation.
	IsContextualFollowUp bool
}

type turnAnalysisInput struct {
	Text    string
	History []string
	Memory  sessions.SessionMemory

	// ActiveClarificationQuestion is the text of the question the assistant asked and is
	// still waiting to be answered. Empty when there is no active clarification.
	ActiveClarificationQuestion string

	// HasRecentResults is true when there are recent tool action results in the
	// transcript or memory that the user might be referring to.
	HasRecentResults bool

	// CheckMemoryMode is true when the message contains write-action keywords and
	// therefore a memoryMode classification is needed to decide on context isolation.
	CheckMemoryMode bool
}

// analyzeTurn makes a single cheap LLM call that classifies all semantic flags
// for the current user turn in one shot. Guards prevent the call from firing when
// no classification is needed. Heuristics serve as fallback on LLM error.
func (r *Runtime) analyzeTurn(ctx context.Context, input turnAnalysisInput) turnAnalysis {
	// Ordinal references always need context — no LLM call required.
	if isOrdinalActionReference(input.Text) {
		return turnAnalysis{
			MemoryMode:           memoryModeNeedsContext,
			IsContextualFollowUp: true,
		}
	}

	needsClarification := input.ActiveClarificationQuestion != ""
	needsResultFollowUp := input.HasRecentResults
	needsContextual := len(input.History) > 0
	needsMemoryMode := input.CheckMemoryMode

	if !needsClarification && !needsResultFollowUp && !needsContextual && !needsMemoryMode {
		return turnAnalysis{MemoryMode: memoryModeFresh, Category: "chat"}
	}

	if r.provider == nil || r.memoryClassifierModel == "" {
		return r.turnAnalysisHeuristic(input)
	}

	result, err := r.llmAnalyzeTurn(ctx, input, needsClarification, needsResultFollowUp, needsContextual, needsMemoryMode)
	if err != nil {
		r.logger.Debug("turn classifier LLM failed, falling back to heuristics", "error", err)
		return r.turnAnalysisHeuristic(input)
	}
	return result
}

func (r *Runtime) llmAnalyzeTurn(
	ctx context.Context,
	input turnAnalysisInput,
	needsClarification, needsResultFollowUp, needsContextual, needsMemoryMode bool,
) (turnAnalysis, error) {
	historyText := "none"
	if len(input.History) > 0 {
		historyText = strings.Join(input.History, "\n")
	}

	var contextSections []string
	contextSections = append(contextSections, "Recent conversation history:\n"+historyText)

	if needsClarification {
		contextSections = append(contextSections,
			"Pending clarification question (assistant asked this and is waiting for an answer):\n"+
				input.ActiveClarificationQuestion)
	}

	if needsResultFollowUp && len(input.Memory.LastActionResults) > 0 {
		var parts []string
		for _, res := range input.Memory.LastActionResults {
			parts = append(parts, res.ToolName+": "+res.Content)
		}
		contextSections = append(contextSections, "Recent action results:\n"+strings.Join(parts, "\n"))
	}

	contextSections = append(contextSections, "User message:\n"+input.Text)

	var fieldDefs []string
	fieldDefs = append(fieldDefs,
		`"shortLabel": string
  ≤4 Vietnamese words describing the request`,
		`"category": "gmail" | "calendar" | "drive" | "docs" | "chat" | "search"
  Choose the primary request category`)
	if needsMemoryMode {
		fieldDefs = append(fieldDefs,
			`"memoryMode": "fresh" | "needs_context"
  fresh        — message is self-contained; all parameters given explicitly
  needs_context — message references prior conversation (numbered items, pronouns like "nó"/"đó", "cái đó", "gửi nó đi")`)
	}
	if needsClarification {
		fieldDefs = append(fieldDefs,
			`"isClarificationAnswer": true | false
  true  — message directly answers the pending clarification question
  false — new request or unrelated`)
	}
	if needsResultFollowUp {
		fieldDefs = append(fieldDefs,
			`"isResultFollowUp": true | false
  true  — message asks about, references, or continues a recent action result
  false — standalone new request`)
	}
	if needsContextual {
		fieldDefs = append(fieldDefs,
			`"isContextualFollowUp": true | false
  true  — message references or continues prior conversation (pronouns, relative references, "tiếp theo", "còn …")
  false — fully standalone request`)
	}

	systemPrompt := `You are a turn classifier for a personal AI assistant.
Classify the user's message and return a JSON object with exactly these fields:

` + strings.Join(fieldDefs, "\n\n") + `

Rules:
- When in doubt with history present, prefer needs_context / true.
- Return valid JSON only. No explanation, no extra fields.`

	userPrompt := strings.Join(contextSections, "\n\n")

	resp, err := r.provider.Generate(ctx, &providers.GenerateRequest{
		SystemPrompt:   systemPrompt,
		UserPrompt:     userPrompt,
		Temperature:    0.0,
		MaxTokens:      80,
		ResponseFormat: "json",
		Model:          r.memoryClassifierModel,
	})
	if err != nil {
		return turnAnalysis{}, fmt.Errorf("turn analysis LLM: %w", err)
	}

	var wire struct {
		MemoryMode            string `json:"memoryMode"`
		ShortLabel            string `json:"shortLabel"`
		Category              string `json:"category"`
		IsClarificationAnswer bool   `json:"isClarificationAnswer"`
		IsResultFollowUp      bool   `json:"isResultFollowUp"`
		IsContextualFollowUp  bool   `json:"isContextualFollowUp"`
	}
	raw := extractTurnAnalysisJSON(resp.Text)
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		return turnAnalysis{}, fmt.Errorf("parse turn analysis response: %w (raw: %q)", err, resp.Text)
	}

	mode := memoryMode(strings.TrimSpace(strings.ToLower(wire.MemoryMode)))
	if mode != memoryModeFresh && mode != memoryModeNeedsContext {
		mode = memoryModeFresh
	}

	return turnAnalysis{
		MemoryMode:            mode,
		ShortLabel:            normalizeRunShortLabel(wire.ShortLabel),
		Category:              normalizeRunCategory(wire.Category),
		IsClarificationAnswer: wire.IsClarificationAnswer,
		IsResultFollowUp:      wire.IsResultFollowUp,
		IsContextualFollowUp:  wire.IsContextualFollowUp,
	}, nil
}

// turnAnalysisHeuristic is the keyword-based fallback used when the LLM call fails.
func (r *Runtime) turnAnalysisHeuristic(input turnAnalysisInput) turnAnalysis {
	mode := memoryModeFresh
	if input.CheckMemoryMode {
		mode = heuristicMemoryMode(input.Text)
	}
	return turnAnalysis{
		MemoryMode:            mode,
		Category:              "chat",
		IsClarificationAnswer: input.ActiveClarificationQuestion != "" && isLikelyClarificationAnswer(input.Text),
		IsResultFollowUp:      input.HasRecentResults && isLikelyResultFollowUpQuestion(input.Text),
		IsContextualFollowUp:  isLikelyContextualFollowUpQuestion(input.Text, input.History, input.Memory),
	}
}

func normalizeRunCategory(category string) string {
	switch strings.TrimSpace(strings.ToLower(category)) {
	case "gmail", "calendar", "drive", "docs", "chat", "search":
		return strings.TrimSpace(strings.ToLower(category))
	default:
		return "chat"
	}
}

func normalizeRunShortLabel(label string) string {
	words := strings.Fields(strings.TrimSpace(label))
	if len(words) > 4 {
		words = words[:4]
	}
	return strings.Join(words, " ")
}

// lastAssistantText returns the most recent non-empty assistant message text from transcript.
func lastAssistantText(transcript []providers.Message) string {
	for i := len(transcript) - 1; i >= 0; i-- {
		if transcript[i].Role == providers.MessageRoleAssistant && strings.TrimSpace(transcript[i].Content) != "" {
			return transcript[i].Content
		}
	}
	return ""
}

func extractTurnAnalysisJSON(text string) string {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end >= start {
		return trimmed[start : end+1]
	}
	return trimmed
}
