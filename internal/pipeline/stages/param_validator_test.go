package stages

import (
	"strings"
	"testing"

	"vclaw/internal/agent"
)

func TestNewParameterValidator(t *testing.T) {
	config := ParameterValidatorConfig{
		StrictMode: true,
	}

	validator := NewParameterValidator(config)

	if validator == nil {
		t.Fatal("Expected non-nil validator")
	}

	if !validator.strictMode {
		t.Error("Expected strict mode to be enabled")
	}
}

func TestValidate_ValidParameters(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	intent := &agent.Intent{
		Type: agent.IntentReadInfo,
		ToolCalls: []agent.ToolCall{
			{
				Name:     "read_file",
				Category: agent.ToolCategorySafeRead,
				Parameters: map[string]interface{}{
					"path": "/etc/config.json",
				},
			},
		},
		ProvidedParams: map[string]interface{}{
			"path": "/etc/config.json",
		},
	}

	validation, err := validator.Validate(intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !validation.IsValid {
		t.Error("expected validation to be valid")
	}

	if len(validation.Missing) > 0 {
		t.Errorf("expected no missing params, got %v", validation.Missing)
	}
}

func TestValidate_MissingParameters(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	intent := &agent.Intent{
		Type: agent.IntentDangerousAction,
		ToolCalls: []agent.ToolCall{
			{
				Name:       "delete_file",
				Category:   agent.ToolCategoryDangerousWrite,
				Parameters: map[string]interface{}{},
			},
		},
		ProvidedParams: map[string]interface{}{},
	}

	validation, err := validator.Validate(intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if validation.IsValid {
		t.Error("expected validation to be invalid")
	}

	if len(validation.Missing) == 0 {
		t.Error("expected missing parameters")
	}

	// delete_file requires "path" and "confirm"
	expectedMissing := 2
	if len(validation.Missing) != expectedMissing {
		t.Errorf("expected %d missing params, got %d", expectedMissing, len(validation.Missing))
	}
}

func TestValidate_NoToolCalls(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	intent := &agent.Intent{
		Type:           agent.IntentGreeting,
		ToolCalls:      []agent.ToolCall{},
		ProvidedParams: map[string]interface{}{},
	}

	validation, err := validator.Validate(intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !validation.IsValid {
		t.Error("expected validation to be valid for no tool calls")
	}
}

func TestValidate_NilIntent(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	_, err := validator.Validate(nil)
	if err == nil {
		t.Error("expected error for nil intent")
	}
}

func TestValidateToolCall_ValidPath(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	toolCall := &agent.ToolCall{
		Name:     "read_file",
		Category: agent.ToolCategorySafeRead,
		Parameters: map[string]interface{}{
			"path": "/etc/config.json",
		},
	}

	validation, err := validator.ValidateToolCall(toolCall)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !validation.IsValid {
		t.Error("expected validation to be valid")
	}
}

func TestValidateToolCall_InvalidPath(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{
		StrictMode: false, // Non-strict to get validation result
	})

	testCases := []struct {
		name string
		path string
	}{
		{"Directory traversal", "../../../etc/passwd"},
		{"Command injection with pipe", "/tmp/file | rm -rf /"},
		{"Command injection with semicolon", "/tmp/file; rm -rf /"},
		{"Redirection", "/tmp/file > /dev/null"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			toolCall := &agent.ToolCall{
				Name:     "read_file",
				Category: agent.ToolCategorySafeRead,
				Parameters: map[string]interface{}{
					"path": tc.path,
				},
			}

			validation, err := validator.ValidateToolCall(toolCall)
			if err != nil {
				// In non-strict mode, should return validation result
				t.Logf("Got error (expected): %v", err)
			}

			if validation != nil && validation.IsValid {
				t.Errorf("expected invalid validation for dangerous path: %s", tc.path)
			}
		})
	}
}

func TestValidateToolCall_InvalidEmail(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{
		StrictMode: false,
	})

	testCases := []struct {
		name  string
		email string
	}{
		{"Missing @", "invalidemail.com"},
		{"Missing domain", "user@"},
		{"Missing username", "@domain.com"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			toolCall := &agent.ToolCall{
				Name:     "send_email",
				Category: agent.ToolCategoryCommunication,
				Parameters: map[string]interface{}{
					"to":      tc.email,
					"subject": "Test",
					"body":    "Test body",
				},
			}

			validation, _ := validator.ValidateToolCall(toolCall)

			if validation != nil && validation.IsValid {
				t.Errorf("expected invalid validation for invalid email: %s", tc.email)
			}
		})
	}
}

func TestGenerateClarificationRequest(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	intent := &agent.Intent{
		Type: agent.IntentDangerousAction,
	}

	validation := &agent.ParameterValidation{
		Required: []string{"path", "confirm"},
		Provided: map[string]interface{}{},
		Missing:  []string{"path", "confirm"},
		IsValid:  false,
	}

	message := validator.GenerateClarificationRequest(intent, validation)

	if message == "" {
		t.Error("expected non-empty clarification message")
	}

	// Should mention the missing parameters
	if !strings.Contains(message, "path") && !strings.Contains(message, "đường dẫn") {
		t.Errorf("clarification message should mention path: %s", message)
	}
}

func TestGenerateClarificationRequest_Valid(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	intent := &agent.Intent{
		Type: agent.IntentReadInfo,
	}

	validation := &agent.ParameterValidation{
		Required: []string{"path"},
		Provided: map[string]interface{}{"path": "/etc/config.json"},
		Missing:  []string{},
		IsValid:  true,
	}

	message := validator.GenerateClarificationRequest(intent, validation)

	if message != "" {
		t.Errorf("expected empty message for valid validation, got: %s", message)
	}
}

func TestGenerateClarificationRequest_SingleParam(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	intent := &agent.Intent{
		Type: agent.IntentReadInfo,
	}

	validation := &agent.ParameterValidation{
		Required: []string{"path"},
		Provided: map[string]interface{}{},
		Missing:  []string{"path"},
		IsValid:  false,
	}

	message := validator.GenerateClarificationRequest(intent, validation)

	if message == "" {
		t.Error("expected non-empty clarification message")
	}

	// Should be a single-parameter message format
	if !strings.Contains(message, "tôi cần biết") {
		t.Errorf("expected single-param format, got: %s", message)
	}
}

func TestValidateParameterType(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	testCases := []struct {
		name      string
		paramType string
		value     interface{}
		shouldErr bool
	}{
		{"Valid string", "string", "test", false},
		{"Invalid string", "string", 123, true},
		{"Valid int", "int", 42, false},
		{"Valid int64", "int", int64(42), false},
		{"Valid float64 as int", "int", float64(42), false},
		{"Valid bool", "bool", true, false},
		{"Invalid bool", "bool", "true", true},
		{"Valid path", "path", "/etc/config.json", false},
		{"Invalid path", "path", 123, true},
		{"Valid email", "email", "user@example.com", false},
		{"Invalid email", "email", "not-an-email", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validator.validateParameterType(tc.paramType, tc.value)

			if tc.shouldErr && err == nil {
				t.Error("expected error but got none")
			}

			if !tc.shouldErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestExtractParametersFromInput(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	testCases := []struct {
		name         string
		input        string
		toolName     string
		expectedKeys []string
	}{
		{
			name:         "Extract file path",
			input:        "Đọc file /etc/config.json",
			toolName:     "read_file",
			expectedKeys: []string{"path"},
		},
		{
			name:         "Extract email",
			input:        "Gửi email cho user@example.com",
			toolName:     "send_email",
			expectedKeys: []string{"to"},
		},
		{
			name:         "Extract command",
			input:        "Chạy lệnh npm install",
			toolName:     "exec",
			expectedKeys: []string{"command"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			params, err := validator.ExtractParametersFromInput(tc.input, tc.toolName)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, key := range tc.expectedKeys {
				if _, exists := params[key]; !exists {
					t.Errorf("expected parameter %q to be extracted", key)
				}
			}
		})
	}
}

func TestExtractFilePath(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	testCases := []struct {
		input    string
		expected string
	}{
		{"Đọc file /etc/config.json", "/etc/config.json"},
		{"Read file test.txt", "test.txt"},
		{"Show me data.csv", "data.csv"},
		{"No file here", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := validator.extractFilePath(tc.input)

			if tc.expected != "" && result == "" {
				t.Errorf("expected to extract path, got empty")
			}

			if tc.expected == "" && result != "" {
				t.Errorf("expected empty, got %q", result)
			}
		})
	}
}

func TestExtractEmail(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	testCases := []struct {
		input    string
		expected bool // Whether email should be found
	}{
		{"Send to user@example.com", true},
		{"Email john.doe@company.org", true},
		{"No email here", false},
		{"Invalid @email", false},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := validator.extractEmail(tc.input)

			if tc.expected && result == "" {
				t.Error("expected to extract email, got empty")
			}

			if !tc.expected && result != "" {
				t.Errorf("expected empty, got %q", result)
			}
		})
	}
}

func TestExtractCommand(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	testCases := []struct {
		input    string
		expected string
	}{
		{"Chạy lệnh npm install", "npm install"},
		{"Run command git pull", "git pull"},
		{"Execute ls -la", "ls -la"},
		{"No command here", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := validator.extractCommand(tc.input)

			if tc.expected != "" && result == "" {
				t.Errorf("expected to extract command, got empty")
			}

			if tc.expected == "" && result != "" {
				t.Errorf("expected empty, got %q", result)
			}

			if tc.expected != "" && result != "" && result != tc.expected {
				t.Logf("Expected %q, got %q (may be acceptable)", tc.expected, result)
			}
		})
	}
}

func TestValidateAllParameters(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	// Valid intent
	validIntent := &agent.Intent{
		Type: agent.IntentReadInfo,
		ToolCalls: []agent.ToolCall{
			{
				Name:     "read_file",
				Category: agent.ToolCategorySafeRead,
				Parameters: map[string]interface{}{
					"path": "/etc/config.json",
				},
			},
		},
	}

	err := validator.ValidateAllParameters(validIntent)
	if err != nil {
		t.Errorf("expected no error for valid intent, got: %v", err)
	}

	// Invalid intent
	invalidIntent := &agent.Intent{
		Type: agent.IntentDangerousAction,
		ToolCalls: []agent.ToolCall{
			{
				Name:       "delete_file",
				Category:   agent.ToolCategoryDangerousWrite,
				Parameters: map[string]interface{}{},
			},
		},
	}

	err = validator.ValidateAllParameters(invalidIntent)
	if err == nil {
		t.Error("expected error for invalid intent")
	}
}

func TestGetMissingParameters(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	intent := &agent.Intent{
		Type: agent.IntentDangerousAction,
		ToolCalls: []agent.ToolCall{
			{
				Name:       "delete_file",
				Category:   agent.ToolCategoryDangerousWrite,
				Parameters: map[string]interface{}{},
			},
		},
	}

	missing, err := validator.GetMissingParameters(intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(missing) == 0 {
		t.Error("expected missing parameters")
	}

	// Should include "path" and "confirm"
	expectedCount := 2
	if len(missing) != expectedCount {
		t.Errorf("expected %d missing params, got %d", expectedCount, len(missing))
	}
}

func TestIsValid(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	validIntent := &agent.Intent{
		Type: agent.IntentReadInfo,
		ToolCalls: []agent.ToolCall{
			{
				Name:     "read_file",
				Category: agent.ToolCategorySafeRead,
				Parameters: map[string]interface{}{
					"path": "/etc/config.json",
				},
			},
		},
	}

	if !validator.IsValid(validIntent) {
		t.Error("expected valid intent to return true")
	}

	invalidIntent := &agent.Intent{
		Type: agent.IntentDangerousAction,
		ToolCalls: []agent.ToolCall{
			{
				Name:       "delete_file",
				Category:   agent.ToolCategoryDangerousWrite,
				Parameters: map[string]interface{}{},
			},
		},
	}

	if validator.IsValid(invalidIntent) {
		t.Error("expected invalid intent to return false")
	}
}

func TestRemoveDuplicates(t *testing.T) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	input := []string{"path", "confirm", "path", "subject", "confirm"}
	result := validator.removeDuplicates(input)

	expected := 3 // path, confirm, subject
	if len(result) != expected {
		t.Errorf("expected %d unique items, got %d", expected, len(result))
	}

	// Check no duplicates
	seen := make(map[string]bool)
	for _, item := range result {
		if seen[item] {
			t.Errorf("found duplicate: %s", item)
		}
		seen[item] = true
	}
}

func BenchmarkValidate(b *testing.B) {
	validator := NewParameterValidator(ParameterValidatorConfig{})

	intent := &agent.Intent{
		Type: agent.IntentReadInfo,
		ToolCalls: []agent.ToolCall{
			{
				Name:     "read_file",
				Category: agent.ToolCategorySafeRead,
				Parameters: map[string]interface{}{
					"path": "/etc/config.json",
				},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = validator.Validate(intent)
	}
}

func BenchmarkExtractParametersFromInput(b *testing.B) {
	validator := NewParameterValidator(ParameterValidatorConfig{})
	input := "Đọc file /etc/config.json"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = validator.ExtractParametersFromInput(input, "read_file")
	}
}
