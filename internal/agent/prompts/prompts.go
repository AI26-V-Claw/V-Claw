package prompts

import (
	_ "embed"
	"fmt"
	"strings"
)

// Embed the intent classifier prompt at compile time
//
//go:embed intent_classifier_prompt.md
var IntentClassifierPrompt string

// PromptBuilder helps construct prompts with context
type PromptBuilder struct {
	basePrompt string
	context    []string
}

// NewIntentClassifierPrompt creates a new prompt builder for intent classification
func NewIntentClassifierPrompt() *PromptBuilder {
	return &PromptBuilder{
		basePrompt: IntentClassifierPrompt,
		context:    make([]string, 0),
	}
}

// WithContext adds additional context to the prompt
func (pb *PromptBuilder) WithContext(context string) *PromptBuilder {
	pb.context = append(pb.context, context)
	return pb
}

// WithToolRegistry adds tool registry information to the prompt
func (pb *PromptBuilder) WithToolRegistry(tools map[string]interface{}) *PromptBuilder {
	// Format tool registry as context
	var toolList strings.Builder
	toolList.WriteString("\n## Available Tools in This Session\n\n")
	
	for name, tool := range tools {
		toolList.WriteString(fmt.Sprintf("- `%s`: %v\n", name, tool))
	}
	
	pb.context = append(pb.context, toolList.String())
	return pb
}

// WithUserContext adds user-specific context (e.g., working directory, preferences)
func (pb *PromptBuilder) WithUserContext(userID, workingDir string) *PromptBuilder {
	contextStr := fmt.Sprintf(`
## User Context
- User ID: %s
- Working Directory: %s
`, userID, workingDir)
	
	pb.context = append(pb.context, contextStr)
	return pb
}

// WithSessionHistory adds recent conversation history (for context, not for parameter inference)
func (pb *PromptBuilder) WithSessionHistory(history []string, maxTurns int) *PromptBuilder {
	if len(history) == 0 {
		return pb
	}
	
	// Limit history to maxTurns
	start := 0
	if len(history) > maxTurns {
		start = len(history) - maxTurns
	}
	
	var historyStr strings.Builder
	historyStr.WriteString("\n## Recent Conversation (For Context Only)\n\n")
	historyStr.WriteString("⚠️ **WARNING**: Do NOT use information from this history to fill parameters for DANGEROUS_ACTION.\n\n")
	
	for i := start; i < len(history); i++ {
		historyStr.WriteString(fmt.Sprintf("%d. %s\n", i-start+1, history[i]))
	}
	
	pb.context = append(pb.context, historyStr.String())
	return pb
}

// Build constructs the final prompt
func (pb *PromptBuilder) Build() string {
	var finalPrompt strings.Builder
	
	// Add base prompt
	finalPrompt.WriteString(pb.basePrompt)
	
	// Add all context sections
	if len(pb.context) > 0 {
		finalPrompt.WriteString("\n\n---\n\n# Additional Context\n\n")
		for _, ctx := range pb.context {
			finalPrompt.WriteString(ctx)
			finalPrompt.WriteString("\n")
		}
	}
	
	return finalPrompt.String()
}

// BuildWithUserInput constructs the final prompt with user input
func (pb *PromptBuilder) BuildWithUserInput(userInput string) string {
	basePrompt := pb.Build()
	
	return fmt.Sprintf(`%s

---

# User Input to Classify

%s

---

# Your Response

Respond with ONLY valid JSON (no markdown, no code blocks):`, basePrompt, userInput)
}

// GetSystemPrompt returns just the system prompt without user input
func (pb *PromptBuilder) GetSystemPrompt() string {
	return pb.Build()
}

// ValidateJSONResponse checks if the response is valid JSON
func ValidateJSONResponse(response string) error {
	response = strings.TrimSpace(response)
	
	// Check if response starts with { and ends with }
	if !strings.HasPrefix(response, "{") {
		return fmt.Errorf("response does not start with '{': %s", response[:min(50, len(response))])
	}
	
	if !strings.HasSuffix(response, "}") {
		return fmt.Errorf("response does not end with '}': %s", response[max(0, len(response)-50):])
	}
	
	return nil
}

