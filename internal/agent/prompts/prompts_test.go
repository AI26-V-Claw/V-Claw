package prompts

import (
	"strings"
	"testing"
)

func TestNewIntentClassifierPrompt(t *testing.T) {
	builder := NewIntentClassifierPrompt()
	
	if builder == nil {
		t.Fatal("Expected non-nil builder")
	}
	
	if builder.basePrompt == "" {
		t.Fatal("Expected non-empty base prompt")
	}
	
	// Check that base prompt contains key sections
	requiredSections := []string{
		"Intent Classification Specialist",
		"GREETING",
		"READ_INFO",
		"DANGEROUS_ACTION",
		"COMPOSITE_ACTION",
		"UNKNOWN",
		"Output Format",
		"Classification Rules",
		"Critical Safety Rules",
	}
	
	for _, section := range requiredSections {
		if !strings.Contains(builder.basePrompt, section) {
			t.Errorf("Base prompt missing required section: %s", section)
		}
	}
}

func TestPromptBuilder_Build(t *testing.T) {
	builder := NewIntentClassifierPrompt()
	prompt := builder.Build()
	
	if prompt == "" {
		t.Fatal("Expected non-empty prompt")
	}
	
	// Should contain base prompt
	if !strings.Contains(prompt, "Intent Classification Specialist") {
		t.Error("Built prompt missing base content")
	}
}

func TestPromptBuilder_WithContext(t *testing.T) {
	builder := NewIntentClassifierPrompt()
	customContext := "Custom context for testing"
	
	prompt := builder.WithContext(customContext).Build()
	
	if !strings.Contains(prompt, customContext) {
		t.Error("Built prompt missing custom context")
	}
	
	if !strings.Contains(prompt, "Additional Context") {
		t.Error("Built prompt missing context header")
	}
}

func TestPromptBuilder_WithToolRegistry(t *testing.T) {
	builder := NewIntentClassifierPrompt()
	
	tools := map[string]interface{}{
		"gmail.listEmails": map[string]string{
			"description": "List Gmail messages",
			"category":    "SAFE_READ",
		},
		"sandbox.runShell": map[string]string{
			"description": "Run shell command in sandbox",
			"category":    "EXECUTION",
		},
	}
	
	prompt := builder.WithToolRegistry(tools).Build()
	
	if !strings.Contains(prompt, "Available Tools") {
		t.Error("Built prompt missing tools section")
	}
	
	if !strings.Contains(prompt, "gmail.listEmails") {
		t.Error("Built prompt missing gmail.listEmails tool")
	}

	if !strings.Contains(prompt, "sandbox.runShell") {
		t.Error("Built prompt missing sandbox.runShell tool")
	}
}

func TestPromptBuilder_WithUserContext(t *testing.T) {
	builder := NewIntentClassifierPrompt()
	
	userID := "user123"
	workingDir := "/home/user/project"
	
	prompt := builder.WithUserContext(userID, workingDir).Build()
	
	if !strings.Contains(prompt, "User Context") {
		t.Error("Built prompt missing user context section")
	}
	
	if !strings.Contains(prompt, userID) {
		t.Error("Built prompt missing user ID")
	}
	
	if !strings.Contains(prompt, workingDir) {
		t.Error("Built prompt missing working directory")
	}
}

func TestPromptBuilder_WithSessionHistory(t *testing.T) {
	builder := NewIntentClassifierPrompt()
	
	history := []string{
		"User: Tìm file config",
		"AI: Tìm thấy /etc/config.json",
		"User: Đọc nội dung file đó",
		"AI: [File content displayed]",
	}
	
	prompt := builder.WithSessionHistory(history, 5).Build()
	
	if !strings.Contains(prompt, "Recent Conversation") {
		t.Error("Built prompt missing conversation history section")
	}
	
	if !strings.Contains(prompt, "WARNING") {
		t.Error("Built prompt missing warning about using history for dangerous actions")
	}
	
	// Should contain all history items
	for _, item := range history {
		if !strings.Contains(prompt, item) {
			t.Errorf("Built prompt missing history item: %s", item)
		}
	}
}

func TestPromptBuilder_WithSessionHistory_Truncation(t *testing.T) {
	builder := NewIntentClassifierPrompt()
	
	// Create 10 history items
	history := make([]string, 10)
	for i := 0; i < 10; i++ {
		history[i] = "Message " + string(rune('0'+i))
	}
	
	// Limit to 3 turns
	prompt := builder.WithSessionHistory(history, 3).Build()
	
	// Should only contain last 3 items
	if !strings.Contains(prompt, "Message 7") {
		t.Error("Built prompt should contain Message 7")
	}
	
	if !strings.Contains(prompt, "Message 8") {
		t.Error("Built prompt should contain Message 8")
	}
	
	if !strings.Contains(prompt, "Message 9") {
		t.Error("Built prompt should contain Message 9")
	}
	
	// Should NOT contain earlier items
	if strings.Contains(prompt, "Message 0") {
		t.Error("Built prompt should not contain Message 0 (should be truncated)")
	}
}

func TestPromptBuilder_BuildWithUserInput(t *testing.T) {
	builder := NewIntentClassifierPrompt()
	userInput := "Xóa file config.json"
	
	prompt := builder.BuildWithUserInput(userInput)
	
	if !strings.Contains(prompt, userInput) {
		t.Error("Built prompt missing user input")
	}
	
	if !strings.Contains(prompt, "User Input to Classify") {
		t.Error("Built prompt missing user input section header")
	}
	
	if !strings.Contains(prompt, "Your Response") {
		t.Error("Built prompt missing response instruction")
	}
	
	if !strings.Contains(prompt, "ONLY valid JSON") {
		t.Error("Built prompt missing JSON-only instruction")
	}
}

func TestPromptBuilder_ChainedCalls(t *testing.T) {
	builder := NewIntentClassifierPrompt()
	
	tools := map[string]interface{}{
		"gmail.listEmails": "List Gmail messages",
	}
	
	history := []string{
		"User: Hello",
		"AI: Hi there",
	}
	
	prompt := builder.
		WithToolRegistry(tools).
		WithUserContext("user123", "/home/user").
		WithSessionHistory(history, 5).
		WithContext("Custom context").
		BuildWithUserInput("Test input")
	
	// Should contain all added context
	requiredParts := []string{
		"Intent Classification Specialist", // Base prompt
		"Available Tools",                  // Tool registry
		"User Context",                     // User context
		"Recent Conversation",              // Session history
		"Custom context",                   // Custom context
		"Test input",                       // User input
	}
	
	for _, part := range requiredParts {
		if !strings.Contains(prompt, part) {
			t.Errorf("Chained prompt missing: %s", part)
		}
	}
}

func TestValidateJSONResponse_Valid(t *testing.T) {
	validJSON := `{
		"intent_type": "READ_INFO",
		"confidence": 0.95,
		"required_params": ["path"],
		"provided_params": {"path": "/etc/config.json"},
		"missing_params": [],
		"tool_calls": [],
		"needs_confirm": false,
		"reasoning": "User wants to read a file"
	}`
	
	err := ValidateJSONResponse(validJSON)
	if err != nil {
		t.Errorf("Expected valid JSON to pass validation, got error: %v", err)
	}
}

func TestValidateJSONResponse_InvalidStart(t *testing.T) {
	invalidJSON := `This is not JSON {
		"intent_type": "READ_INFO"
	}`
	
	err := ValidateJSONResponse(invalidJSON)
	if err == nil {
		t.Error("Expected error for JSON not starting with '{'")
	}
	
	if !strings.Contains(err.Error(), "does not start with '{'") {
		t.Errorf("Expected specific error message, got: %v", err)
	}
}

func TestValidateJSONResponse_InvalidEnd(t *testing.T) {
	invalidJSON := `{
		"intent_type": "READ_INFO"
	} and some extra text`
	
	err := ValidateJSONResponse(invalidJSON)
	if err == nil {
		t.Error("Expected error for JSON not ending with '}'")
	}
	
	if !strings.Contains(err.Error(), "does not end with '}'") {
		t.Errorf("Expected specific error message, got: %v", err)
	}
}

func TestValidateJSONResponse_Markdown(t *testing.T) {
	markdownJSON := "```json\n{\n  \"intent_type\": \"READ_INFO\"\n}\n```"
	
	err := ValidateJSONResponse(markdownJSON)
	if err == nil {
		t.Error("Expected error for JSON wrapped in markdown")
	}
}

func TestValidateJSONResponse_WithWhitespace(t *testing.T) {
	jsonWithWhitespace := `
	
	{
		"intent_type": "READ_INFO"
	}
	
	`
	
	err := ValidateJSONResponse(jsonWithWhitespace)
	if err != nil {
		t.Errorf("Expected valid JSON with whitespace to pass, got error: %v", err)
	}
}

func TestGetSystemPrompt(t *testing.T) {
	builder := NewIntentClassifierPrompt()
	
	systemPrompt := builder.
		WithContext("Test context").
		GetSystemPrompt()
	
	// Should contain base prompt and context
	if !strings.Contains(systemPrompt, "Intent Classification Specialist") {
		t.Error("System prompt missing base content")
	}
	
	if !strings.Contains(systemPrompt, "Test context") {
		t.Error("System prompt missing added context")
	}
	
	// Should NOT contain user input section
	if strings.Contains(systemPrompt, "User Input to Classify") {
		t.Error("System prompt should not contain user input section")
	}
}

func BenchmarkPromptBuilder_Build(b *testing.B) {
	builder := NewIntentClassifierPrompt()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = builder.Build()
	}
}

func BenchmarkPromptBuilder_BuildWithContext(b *testing.B) {
	tools := map[string]interface{}{
		"gmail.listEmails": "List Gmail messages",
		"gmail.sendEmail":  "Send email",
		"sandbox.runShell": "Execute command",
	}
	
	history := []string{
		"User: Hello",
		"AI: Hi",
		"User: Read file",
		"AI: Done",
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		builder := NewIntentClassifierPrompt()
		_ = builder.
			WithToolRegistry(tools).
			WithUserContext("user123", "/home/user").
			WithSessionHistory(history, 5).
			Build()
	}
}

func BenchmarkPromptBuilder_BuildWithUserInput(b *testing.B) {
	builder := NewIntentClassifierPrompt()
	userInput := "Xóa file config.json trong thư mục /etc"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = builder.BuildWithUserInput(userInput)
	}
}

func BenchmarkValidateJSONResponse(b *testing.B) {
	validJSON := `{
		"intent_type": "READ_INFO",
		"confidence": 0.95,
		"required_params": ["path"],
		"provided_params": {"path": "/etc/config.json"},
		"missing_params": [],
		"tool_calls": [],
		"needs_confirm": false,
		"reasoning": "User wants to read a file"
	}`
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateJSONResponse(validJSON)
	}
}
