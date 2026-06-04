package agent

import (
	"fmt"
	"strings"

	"vclaw/internal/providers"
)

const clarifyToolName = "clarify"

type pendingClarification struct {
	question string
	choices  []string
	reason   string
}

func clarifyToolDefinition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        clarifyToolName,
		Description: "Ask the user one concise clarification question when required information is missing before proceeding. This is not approval and does not execute external side effects.",
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"question": map[string]any{
					"type":        "string",
					"description": "The concise question to ask the user.",
				},
				"choices": map[string]any{
					"type":        "array",
					"description": "Optional answer choices. Use at most 4.",
					"items":       map[string]any{"type": "string"},
					"maxItems":    4,
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Short internal reason for why clarification is needed.",
				},
				"missing_fields": map[string]any{
					"type":        "array",
					"description": "Optional required fields that are missing.",
					"items":       map[string]any{"type": "string"},
				},
			},
			"required": []string{"question"},
		},
	}
}

func isClarifyToolCall(call providers.ToolCall) bool {
	return strings.TrimSpace(call.Name) == clarifyToolName
}

func clarificationFromToolCall(call providers.ToolCall) pendingClarification {
	question := stringArg(call.Arguments, "question")
	if question == "" {
		question = "Bạn cần bổ sung thông tin nào để tôi tiếp tục?"
	}
	choices := stringSliceArg(call.Arguments, "choices")
	reason := stringArg(call.Arguments, "reason")
	if reason == "" {
		fields := stringSliceArg(call.Arguments, "missing_fields")
		if len(fields) > 0 {
			reason = "missing fields: " + strings.Join(fields, ", ")
		}
	}
	return pendingClarification{
		question: question,
		choices:  choices,
		reason:   reason,
	}
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, ok := args[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func stringSliceArg(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	value, ok := args[key]
	if !ok || value == nil {
		return nil
	}
	switch values := value.(type) {
	case []string:
		return cleanChoices(values)
	case []any:
		out := make([]string, 0, len(values))
		for _, item := range values {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				out = append(out, text)
			}
		}
		return cleanChoices(out)
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" {
			return nil
		}
		return []string{text}
	}
}

func cleanChoices(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text != "" {
			out = append(out, text)
		}
		if len(out) >= 4 {
			break
		}
	}
	return out
}
