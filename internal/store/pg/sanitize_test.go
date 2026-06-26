package pg

import (
	"testing"
	"unicode/utf8"

	"vclaw/internal/agent"
)

func TestSanitizeRunStateTextRemovesInvalidUTF8(t *testing.T) {
	invalid := string([]byte{0xe1, '\n', '.'})
	state := sanitizeRunStateText(agent.RunState{
		SessionID:     invalid,
		RequestID:     invalid,
		OriginalGoal:  invalid,
		ShortLabel:    invalid,
		Category:      invalid,
		ErrorRef:      invalid,
		Model:         invalid,
		PromptVersion: invalid,
	})

	values := map[string]string{
		"SessionID":     state.SessionID,
		"RequestID":     state.RequestID,
		"OriginalGoal":  state.OriginalGoal,
		"ShortLabel":    state.ShortLabel,
		"Category":      state.Category,
		"ErrorRef":      state.ErrorRef,
		"Model":         state.Model,
		"PromptVersion": state.PromptVersion,
	}
	for field, value := range values {
		if !utf8.ValidString(value) {
			t.Fatalf("%s is not valid UTF-8: %q", field, value)
		}
	}
}
