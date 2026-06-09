package sessions

import (
	"fmt"

	"vclaw/internal/providers"
)

// EstimateTokens ước lượng số token của một string bằng heuristic.
// Dùng 3 rune/token — an toàn cho văn bản tiếng Việt+English lẫn lộn.
// Sai số ~20%, đủ để làm compaction threshold.
func EstimateTokens(text string) int {
	runes := len([]rune(text))
	if runes == 0 {
		return 0
	}
	estimate := runes / 3
	if estimate == 0 {
		return 1
	}
	return estimate
}

// EstimateMessagesTokens ước lượng tổng token của một slice messages,
// bao gồm cả content và arguments của tool calls.
func EstimateMessagesTokens(messages []providers.Message) int {
	total := 0
	for _, msg := range messages {
		total += EstimateTokens(msg.Content)
		for _, tc := range msg.ToolCalls {
			total += EstimateTokens(tc.Name)
			for _, v := range tc.Arguments {
				total += EstimateTokens(fmt.Sprint(v))
			}
		}
	}
	return total
}
