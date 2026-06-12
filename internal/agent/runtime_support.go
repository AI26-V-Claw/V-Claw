package agent

import (
	"context"
	"fmt"
	"html"
	"strings"

	"vclaw/internal/agent/reference"
	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

const maxToolContentForLLM = 4000

func executeToolSafely(ctx context.Context, tool tools.Tool, toolCall tools.ToolCall) (result tools.ToolResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = tools.ExecutionErrorResult(toolCall, fmt.Errorf("panic: %v", recovered))
		}
	}()

	return tool.Execute(ctx, toolCall)
}

func truncateToolContentForLLM(content string) string {
	if len(content) <= maxToolContentForLLM {
		return content
	}

	return content[:maxToolContentForLLM] + fmt.Sprintf("\n...[truncated %d bytes]", len(content)-maxToolContentForLLM)
}

func extractPlannerJSONObject(text string) string {
	return extractJSONObject(text)
}

func extractJSONObject(text string) string {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end >= start {
		return trimmed[start : end+1]
	}
	return trimmed
}

func xmlEscape(value string) string {
	return html.EscapeString(strings.TrimSpace(value))
}

func isOrdinalActionReference(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return containsAnyText(lower,
		"số 1", "so 1", "số 2", "so 2", "số 3", "so 3", "số 4", "so 4", "số 5", "so 5",
		"cái 1", "cai 1", "cái 2", "cai 2", "cái 3", "cai 3",
		"cái đầu tiên", "cai dau tien", "cái đầu", "cai dau",
		"cái thứ nhất", "cai thu nhat", "cái thứ hai", "cai thu hai", "cái thứ ba", "cai thu ba",
		"mục 1", "muc 1", "mục 2", "muc 2", "mục 3", "muc 3",
		"#1", "#2", "#3", "#4", "#5",
		"item 1", "item 2", "item 3", "option 1", "option 2",
	)
}

func cloneProviderMessages(messages []providers.Message) []providers.Message {
	cloned := make([]providers.Message, len(messages))
	for i, message := range messages {
		cloned[i] = message
		cloned[i].ToolCalls = cloneProviderToolCalls(message.ToolCalls)
	}
	return cloned
}

func sanitizeProviderTranscriptForToolProtocol(messages []providers.Message) []providers.Message {
	if len(messages) == 0 {
		return nil
	}
	sanitized := make([]providers.Message, 0, len(messages))
	for i := 0; i < len(messages); {
		message := messages[i]
		if message.Role == providers.MessageRoleTool {
			i++
			continue
		}
		if message.Role != providers.MessageRoleAssistant || len(message.ToolCalls) == 0 {
			sanitized = append(sanitized, cloneProviderMessages([]providers.Message{message})[0])
			i++
			continue
		}

		expected := make(map[string]bool, len(message.ToolCalls))
		for _, toolCall := range message.ToolCalls {
			toolCallID := strings.TrimSpace(toolCall.ID)
			if toolCallID != "" {
				expected[toolCallID] = false
			}
		}
		j := i + 1
		toolMessages := make([]providers.Message, 0, len(expected))
		for j < len(messages) && messages[j].Role == providers.MessageRoleTool {
			toolCallID := strings.TrimSpace(messages[j].ToolCallID)
			if _, ok := expected[toolCallID]; ok && !expected[toolCallID] {
				expected[toolCallID] = true
				toolMessages = append(toolMessages, cloneProviderMessages([]providers.Message{messages[j]})[0])
			}
			j++
		}
		allToolCallsAnswered := len(expected) > 0
		for _, answered := range expected {
			if !answered {
				allToolCallsAnswered = false
				break
			}
		}
		if allToolCallsAnswered {
			sanitized = append(sanitized, cloneProviderMessages([]providers.Message{message})[0])
			sanitized = append(sanitized, toolMessages...)
		} else if strings.TrimSpace(message.Content) != "" {
			fallback := message
			fallback.ToolCalls = nil
			fallback.ToolCallID = ""
			sanitized = append(sanitized, cloneProviderMessages([]providers.Message{fallback})[0])
		}
		i = j
	}
	return sanitized
}

func transcriptWithLastUserContent(transcript []providers.Message, content string) []providers.Message {
	cloned := cloneProviderMessages(transcript)
	content = strings.TrimSpace(content)
	if len(cloned) == 0 || content == "" {
		return cloned
	}
	for i := len(cloned) - 1; i >= 0; i-- {
		if cloned[i].Role == providers.MessageRoleUser {
			cloned[i].Content = content
			break
		}
	}
	return cloned
}

func cloneProviderToolCalls(toolCalls []providers.ToolCall) []providers.ToolCall {
	if len(toolCalls) == 0 {
		return nil
	}
	cloned := make([]providers.ToolCall, len(toolCalls))
	for i, toolCall := range toolCalls {
		cloned[i] = toolCall
		cloned[i].Arguments = cloneArguments(toolCall.Arguments)
	}
	return cloned
}

func cloneArguments(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	cloned := make(map[string]any, len(args))
	for key, value := range args {
		cloned[key] = value
	}
	return cloned
}

// heuristicFirstResolver tries the heuristic resolver first. It only trusts the
// heuristic when the result is high-confidence and requires no clarification
// (isUsableReference). When the heuristic is uncertain — e.g. it finds a cue word
// but cannot locate a matching past result, or when the cue is a forward reference
// inside the same request ("sự kiện này" referring to an event being created now) —
// it falls back to the LLM resolver so the LLM can make the correct judgment.
type heuristicFirstResolver struct {
	primary  reference.Resolver
	fallback reference.Resolver
}

func newHeuristicFirstResolver(primary reference.Resolver, fallback reference.Resolver) *heuristicFirstResolver {
	return &heuristicFirstResolver{primary: primary, fallback: fallback}
}

func (r *heuristicFirstResolver) Resolve(ctx context.Context, input reference.Input) (*reference.Resolution, error) {
	result, err := r.primary.Resolve(ctx, input)
	if err == nil && isUsableReference(result) {
		// Heuristic resolved with high confidence and no clarification needed — trust it.
		return result, nil
	}
	// Heuristic is uncertain (low confidence, needs clarification, or no match).
	// Delegate to LLM so it can distinguish forward references (e.g. "sự kiện này"
	// referring to an event being created in the same request) from genuine
	// past-result references.
	return r.fallback.Resolve(ctx, input)
}
