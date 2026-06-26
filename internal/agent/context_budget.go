package agent

import (
	"encoding/json"
	"strings"

	"vclaw/internal/providers"
	"vclaw/internal/sessions"
)

// ContextBudget caps how many estimated tokens each assembled context section
// may contribute to a provider request. It is derived from the model's context
// window so the assembler never silently overflows the window, regardless of how
// large any single transcript message, memory file, or tool result grows.
//
// The numbers are token ESTIMATES (sessions.EstimateTokens), not exact provider
// tokens. They are intentionally conservative: the goal is to keep the request
// safely under contextWindow - OutputReserve, leaving room for the model's reply
// and tool-continuation rounds.
//
// All values are configurable. Defaults scale from the context window via
// DefaultContextBudget so a 32k model and a 128k model both get a sensible split
// without hard-coding any single model's size.
type ContextBudget struct {
	// ContextWindow is the model's total token window.
	ContextWindow int
	// OutputReserve is held back for the model's response plus tool-continuation
	// rounds; assembled context must fit in ContextWindow - OutputReserve.
	OutputReserve int
	// LongTermMemory caps the long-term memory (USER.md) block.
	LongTermMemory int
	// Summary caps the session summary block.
	Summary int
	// References caps the combined references + linked knowledge blocks.
	References int
	// ActionResults caps the recent action-results block.
	ActionResults int
}

// Budget fractions of the context window used by DefaultContextBudget. Chosen so
// that at 128k they reproduce the figures from the review (≈20k/8k/4k/6k/8k) and
// scale proportionally for other window sizes.
const (
	budgetFracOutputReserve  = 0.156 // ≈20k @ 128k
	budgetFracLongTermMemory = 0.0625
	budgetFracSummary        = 0.03125
	budgetFracReferences     = 0.0469
	budgetFracActionResults  = 0.0625
)

// DefaultContextBudget returns a budget scaled from the given context window.
// A non-positive window falls back to 128k.
func DefaultContextBudget(contextWindow int) ContextBudget {
	if contextWindow <= 0 {
		contextWindow = 128_000
	}
	scale := func(frac float64) int {
		v := int(float64(contextWindow) * frac)
		if v < 1 {
			v = 1
		}
		return v
	}
	return ContextBudget{
		ContextWindow:  contextWindow,
		OutputReserve:  scale(budgetFracOutputReserve),
		LongTermMemory: scale(budgetFracLongTermMemory),
		Summary:        scale(budgetFracSummary),
		References:     scale(budgetFracReferences),
		ActionResults:  scale(budgetFracActionResults),
	}
}

// normalized fills any non-positive field from the scaled defaults so a partially
// configured override is still usable.
func (b ContextBudget) normalized() ContextBudget {
	def := DefaultContextBudget(b.ContextWindow)
	if b.ContextWindow <= 0 {
		b.ContextWindow = def.ContextWindow
	}
	if b.OutputReserve <= 0 {
		b.OutputReserve = def.OutputReserve
	}
	if b.LongTermMemory <= 0 {
		b.LongTermMemory = def.LongTermMemory
	}
	if b.Summary <= 0 {
		b.Summary = def.Summary
	}
	if b.References <= 0 {
		b.References = def.References
	}
	if b.ActionResults <= 0 {
		b.ActionResults = def.ActionResults
	}
	return b
}

// Available returns the total token budget for assembled context: the window
// minus the output reserve. Never negative.
func (b ContextBudget) Available() int {
	avail := b.ContextWindow - b.OutputReserve
	if avail < 0 {
		return 0
	}
	return avail
}

// estimateToolDefinitionsTokens estimates the prompt budget consumed by tool
// schemas. Providers serialize tool definitions differently, but JSON is a
// conservative common shape and keeps the budget aware of schema growth.
func estimateToolDefinitionsTokens(tools []providers.ToolDefinition) int {
	if len(tools) == 0 {
		return 0
	}
	data, err := json.Marshal(tools)
	if err != nil {
		return 0
	}
	return sessions.EstimateTokens(string(data))
}

func estimateProviderRequestTokens(messages []providers.Message, tools []providers.ToolDefinition) int {
	return sessions.EstimateMessagesTokens(messages) + estimateToolDefinitionsTokens(tools)
}

// truncateToTokenBudget trims text so its estimated token count is at most
// maxTokens. It cuts on a rune boundary derived from the token estimator's
// runes-per-token ratio and appends a marker. Empty or already-small text is
// returned unchanged.
func truncateToTokenBudget(text string, maxTokens int) string {
	text = strings.TrimSpace(text)
	if maxTokens <= 0 || text == "" {
		if maxTokens <= 0 {
			return ""
		}
		return text
	}
	if sessions.EstimateTokens(text) <= maxTokens {
		return text
	}
	const marker = "\n...[truncated to fit context budget]"
	markerTokens := sessions.EstimateTokens(marker)
	runes := []rune(text)
	// EstimateTokens uses ~3 runes/token; reserve room for the marker so the
	// final string (content + marker) still fits within maxTokens.
	keep := (maxTokens - markerTokens) * 3
	if keep >= len(runes) {
		keep = len(runes)
	}
	if keep < 0 {
		keep = 0
	}
	return strings.TrimSpace(string(runes[:keep])) + marker
}

// truncateMemoryByTokens trims a long-term memory document to maxTokens while
// preferring (a) markdown headings and (b) lines relevant to the current query.
// This keeps USER.md within budget without dropping the facts most likely needed
// for the current request. When nothing is query-relevant it falls back to a
// plain head truncation so structure is still preserved.
func truncateMemoryByTokens(content string, maxTokens int, query string) string {
	content = strings.TrimSpace(content)
	if maxTokens <= 0 {
		return ""
	}
	if content == "" || sessions.EstimateTokens(content) <= maxTokens {
		return content
	}
	terms := queryTerms(query)
	lines := strings.Split(content, "\n")

	type scored struct {
		idx     int
		line    string
		heading bool
		match   bool
	}
	items := make([]scored, 0, len(lines))
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		items = append(items, scored{
			idx:     i,
			line:    line,
			heading: strings.HasPrefix(trimmed, "#"),
			match:   lineMatchesTerms(trimmed, terms),
		})
	}

	// Select in priority order (headings, query matches, then the rest) but keep
	// original document order in the output for readability.
	selected := make(map[int]bool)
	used := 0
	add := func(it scored) bool {
		if selected[it.idx] {
			return true
		}
		cost := sessions.EstimateTokens(it.line)
		if cost == 0 {
			cost = 1
		}
		if used+cost > maxTokens {
			return false
		}
		selected[it.idx] = true
		used += cost
		return true
	}
	for _, it := range items {
		if it.heading {
			add(it)
		}
	}
	for _, it := range items {
		if it.match {
			add(it)
		}
	}
	for _, it := range items {
		if !add(it) {
			break
		}
	}

	kept := make([]string, 0, len(selected))
	dropped := false
	for _, it := range items {
		if selected[it.idx] {
			kept = append(kept, it.line)
		} else {
			dropped = true
		}
	}
	out := strings.TrimSpace(strings.Join(kept, "\n"))
	if dropped {
		out += "\n...[long-term memory trimmed to fit context budget]"
	}
	return out
}

func queryTerms(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.Trim(f, ".,;:!?\"'()[]{}")
		if len([]rune(f)) >= 3 {
			terms = append(terms, f)
		}
	}
	return terms
}

func lineMatchesTerms(line string, terms []string) bool {
	if len(terms) == 0 {
		return false
	}
	lower := strings.ToLower(line)
	for _, t := range terms {
		if strings.Contains(lower, t) {
			return true
		}
	}
	return false
}

// selectTranscriptWithinBudget keeps recent transcript by atomic conversation
// fit within budgetTokens, walking newest→oldest. Oversized individual messages
// are truncated (user/assistant by token budget, tool messages by the existing
// byte cap) rather than dropped outright. The current (latest) user message is
// always retained, truncated if necessary, so the model never loses the request
// it must answer. Returned messages are in chronological order.
// selectTranscriptWithinBudget keeps recent transcript by atomic conversation
// blocks. Assistant tool-call messages and their immediate tool results are kept
// together so context trimming cannot create invalid provider history.
func selectTranscriptWithinBudget(transcript []providers.Message, budgetTokens int) []providers.Message {
	if len(transcript) == 0 {
		return nil
	}
	if budgetTokens < 0 {
		budgetTokens = 0
	}

	blocks, latestUserBlock := transcriptBlocks(transcript)
	if len(blocks) == 0 {
		return nil
	}
	selected := make([]bool, len(blocks))
	used := 0

	if latestUserBlock >= 0 {
		block, ok := fitTranscriptBlock(blocks[latestUserBlock], budgetTokens)
		if ok {
			blocks[latestUserBlock] = block
			selected[latestUserBlock] = true
			used += messageCost(block.messages)
		}
	}

	for i := len(blocks) - 1; i >= 0; i-- {
		if selected[i] {
			continue
		}
		remaining := budgetTokens - used
		if remaining <= 0 {
			break
		}
		block, ok := fitTranscriptBlock(blocks[i], remaining)
		if !ok {
			continue
		}
		blocks[i] = block
		selected[i] = true
		used += messageCost(block.messages)
	}

	out := make([]providers.Message, 0, len(transcript))
	for i, block := range blocks {
		if !selected[i] {
			continue
		}
		out = append(out, block.messages...)
	}
	return out
}

type transcriptBlock struct {
	messages []providers.Message
}

func transcriptBlocks(transcript []providers.Message) ([]transcriptBlock, int) {
	blocks := make([]transcriptBlock, 0, len(transcript))
	latestUserBlock := -1
	for i := 0; i < len(transcript); {
		message := cloneProviderMessages([]providers.Message{transcript[i]})[0]
		if message.Role == providers.MessageRoleAssistant && len(message.ToolCalls) > 0 {
			block := transcriptBlock{messages: []providers.Message{message}}
			i++
			for i < len(transcript) && transcript[i].Role == providers.MessageRoleTool {
				block.messages = append(block.messages, cloneProviderMessages([]providers.Message{transcript[i]})[0])
				i++
			}
			blocks = append(blocks, block)
			continue
		}
		if message.Role == providers.MessageRoleTool {
			i++
			continue
		}
		block := transcriptBlock{messages: []providers.Message{message}}
		if message.Role == providers.MessageRoleUser {
			latestUserBlock = len(blocks)
		}
		blocks = append(blocks, block)
		i++
	}
	return blocks, latestUserBlock
}

func fitTranscriptBlock(block transcriptBlock, budgetTokens int) (transcriptBlock, bool) {
	if budgetTokens <= 0 || len(block.messages) == 0 {
		return transcriptBlock{}, false
	}
	block.messages = cloneProviderMessages(block.messages)
	normalizeToolResultContent(block.messages)
	if messageCost(block.messages) <= budgetTokens {
		return block, true
	}
	if isPlainTextBlock(block) {
		block.messages[0].Content = truncateToTokenBudget(block.messages[0].Content, budgetTokens)
		return block, strings.TrimSpace(block.messages[0].Content) != ""
	}
	if isToolCallBlock(block) {
		block = truncateToolCallBlock(block, budgetTokens)
		return block, messageCost(block.messages) <= budgetTokens
	}
	return transcriptBlock{}, false
}

func normalizeToolResultContent(messages []providers.Message) {
	for i := range messages {
		if messages[i].Role == providers.MessageRoleTool {
			messages[i].Content = truncateStringBytes(strings.TrimSpace(messages[i].Content), 1600)
		}
	}
}

func truncateToolCallBlock(block transcriptBlock, budgetTokens int) transcriptBlock {
	toolIndexes := make([]int, 0, len(block.messages))
	for i := range block.messages {
		if block.messages[i].Role == providers.MessageRoleTool {
			toolIndexes = append(toolIndexes, i)
		}
	}
	if len(toolIndexes) == 0 {
		return block
	}
	assistantCost := messageCost([]providers.Message{block.messages[0]})
	remaining := budgetTokens - assistantCost
	if remaining < len(toolIndexes) {
		remaining = len(toolIndexes)
	}
	perTool := remaining / len(toolIndexes)
	if perTool < 1 {
		perTool = 1
	}
	for _, idx := range toolIndexes {
		block.messages[idx].Content = truncateToTokenBudget(block.messages[idx].Content, perTool)
		if strings.TrimSpace(block.messages[idx].Content) == "" {
			block.messages[idx].Content = "[tool result omitted to fit context budget]"
		}
	}
	return block
}

func isPlainTextBlock(block transcriptBlock) bool {
	return len(block.messages) == 1 &&
		block.messages[0].Role != providers.MessageRoleTool &&
		len(block.messages[0].ToolCalls) == 0
}

func isToolCallBlock(block transcriptBlock) bool {
	return len(block.messages) > 0 &&
		block.messages[0].Role == providers.MessageRoleAssistant &&
		len(block.messages[0].ToolCalls) > 0
}

func messageCost(messages []providers.Message) int {
	c := sessions.EstimateMessagesTokens(messages)
	if c == 0 && len(messages) > 0 {
		return 1
	}
	return c
}

func selectTranscriptWithinBudgetLegacy(transcript []providers.Message, budgetTokens int) []providers.Message {
	if len(transcript) == 0 {
		return nil
	}
	if budgetTokens < 0 {
		budgetTokens = 0
	}

	// Identify the latest user message — it must survive regardless of budget.
	latestUser := -1
	for i := len(transcript) - 1; i >= 0; i-- {
		if transcript[i].Role == providers.MessageRoleUser {
			latestUser = i
			break
		}
	}

	// resolved holds the (possibly truncated) message chosen for each index.
	resolved := make([]*providers.Message, len(transcript))
	used := 0
	cost := func(m providers.Message) int {
		c := sessions.EstimateMessagesTokens([]providers.Message{m})
		if c == 0 {
			c = 1
		}
		return c
	}

	for i := len(transcript) - 1; i >= 0; i-- {
		m := cloneProviderMessages([]providers.Message{transcript[i]})[0]
		if m.Role == providers.MessageRoleTool {
			m.Content = truncateStringBytes(strings.TrimSpace(m.Content), 1600)
		}
		c := cost(m)
		if used+c <= budgetTokens {
			resolved[i] = &m
			used += c
			continue
		}
		// Doesn't fit whole; try to fit a truncated version for plain text messages.
		remaining := budgetTokens - used
		if remaining > 0 && m.Role != providers.MessageRoleTool && len(m.ToolCalls) == 0 {
			m.Content = truncateToTokenBudget(m.Content, remaining)
			resolved[i] = &m
			used += cost(m)
		}
		if i <= latestUser {
			// Past the current request going backwards; stop scanning older history.
			break
		}
	}

	// Guarantee the current user message is present even if budget was exhausted.
	if latestUser >= 0 && resolved[latestUser] == nil {
		m := cloneProviderMessages([]providers.Message{transcript[latestUser]})[0]
		if budgetTokens > 0 {
			m.Content = truncateToTokenBudget(m.Content, budgetTokens)
		}
		resolved[latestUser] = &m
	}

	out := make([]providers.Message, 0, len(transcript))
	for i := range resolved {
		if resolved[i] == nil {
			continue
		}
		out = append(out, *resolved[i])
	}
	return out
}
