package stages

import (
	"fmt"
	"strings"

	"vclaw/internal/agent"
)

// ParameterValidator validates parameters for tool calls
type ParameterValidator struct {
	strictMode bool // If true, fail on any validation error
}

// ParameterValidatorConfig contains configuration for the validator
type ParameterValidatorConfig struct {
	StrictMode bool
}

// NewParameterValidator creates a new parameter validator
func NewParameterValidator(config ParameterValidatorConfig) *ParameterValidator {
	return &ParameterValidator{
		strictMode: config.StrictMode,
	}
}

// Validate validates parameters for an intent
func (pv *ParameterValidator) Validate(intent *agent.Intent) (*agent.ParameterValidation, error) {
	if intent == nil {
		return nil, fmt.Errorf("intent cannot be nil")
	}

	// If no tool calls, nothing to validate
	if len(intent.ToolCalls) == 0 {
		return &agent.ParameterValidation{
			Required: []string{},
			Provided: intent.ProvidedParams,
			Missing:  []string{},
			IsValid:  true,
		}, nil
	}

	// Validate each tool call
	var allRequired []string
	var allMissing []string

	for _, toolCall := range intent.ToolCalls {
		validation, err := pv.ValidateToolCall(&toolCall)
		if err != nil {
			if pv.strictMode {
				return nil, fmt.Errorf("validation failed for tool %s: %w", toolCall.Name, err)
			}
			// In non-strict mode, continue with other tools
			continue
		}

		allRequired = append(allRequired, validation.Required...)
		allMissing = append(allMissing, validation.Missing...)
	}

	// Remove duplicates
	allRequired = pv.removeDuplicates(allRequired)
	allMissing = pv.removeDuplicates(allMissing)

	validation := &agent.ParameterValidation{
		Required: allRequired,
		Provided: intent.ProvidedParams,
		Missing:  allMissing,
		IsValid:  len(allMissing) == 0,
	}

	return validation, nil
}

// ValidateToolCall validates parameters for a single tool call
func (pv *ParameterValidator) ValidateToolCall(toolCall *agent.ToolCall) (*agent.ParameterValidation, error) {
	// Get tool definition
	tool, err := agent.GetTool(toolCall.Name)
	if err != nil {
		return nil, fmt.Errorf("tool not found: %w", err)
	}

	validation := &agent.ParameterValidation{
		Required: []string{},
		Provided: toolCall.Parameters,
		Missing:  []string{},
		IsValid:  true,
	}

	// Check each required parameter
	for _, paramDef := range tool.Parameters {
		if !paramDef.Required {
			continue
		}

		validation.Required = append(validation.Required, paramDef.Name)

		// Check if parameter is provided
		value, exists := toolCall.Parameters[paramDef.Name]
		if !exists {
			validation.Missing = append(validation.Missing, paramDef.Name)
			validation.IsValid = false
			continue
		}

		// Validate parameter value
		if paramDef.Validator != nil {
			if err := paramDef.Validator(value); err != nil {
				if pv.strictMode {
					return nil, fmt.Errorf("parameter %s validation failed: %w", paramDef.Name, err)
				}
				validation.Missing = append(validation.Missing, paramDef.Name)
				validation.IsValid = false
			}
		}

		// Type-specific validation
		if err := pv.validateParameterType(paramDef.Type, value); err != nil {
			if pv.strictMode {
				return nil, fmt.Errorf("parameter %s type validation failed: %w", paramDef.Name, err)
			}
			validation.Missing = append(validation.Missing, paramDef.Name)
			validation.IsValid = false
		}
	}

	return validation, nil
}

// validateParameterType validates a parameter value against its type
func (pv *ParameterValidator) validateParameterType(paramType string, value interface{}) error {
	switch paramType {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
	case "int":
		switch value.(type) {
		case int, int32, int64, float64:
			// Accept numeric types
		default:
			return fmt.Errorf("expected int, got %T", value)
		}
	case "bool":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected bool, got %T", value)
		}
	case "path":
		pathStr, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string for path, got %T", value)
		}
		return agent.ValidatePath(pathStr)
	case "email":
		return agent.ValidateEmail(value)
	}

	return nil
}

// GenerateClarificationRequest generates a clarification request for missing parameters
func (pv *ParameterValidator) GenerateClarificationRequest(
	intent *agent.Intent,
	validation *agent.ParameterValidation,
) string {
	if validation.IsValid {
		return ""
	}

	// Build clarification message
	var message strings.Builder

	// Determine the action
	action := pv.getActionDescription(intent.Type)

	if len(validation.Missing) == 1 {
		// Single missing parameter
		param := validation.Missing[0]
		message.WriteString(fmt.Sprintf("Để %s, tôi cần biết %s. ",
			action, pv.getParameterDescription(param)))
		message.WriteString(fmt.Sprintf("Bạn có thể cung cấp %s không?", param))
	} else {
		// Multiple missing parameters
		message.WriteString(fmt.Sprintf("Để %s, tôi cần các thông tin sau:\n", action))
		for i, param := range validation.Missing {
			message.WriteString(fmt.Sprintf("%d. %s\n", i+1, pv.getParameterDescription(param)))
		}
		message.WriteString("\nBạn có thể cung cấp các thông tin này không?")
	}

	return message.String()
}

// getActionDescription returns a description of the action based on intent type
func (pv *ParameterValidator) getActionDescription(intentType agent.IntentType) string {
	descriptions := map[agent.IntentType]string{
		agent.IntentReadInfo:        "đọc thông tin",
		agent.IntentDangerousAction: "thực hiện hành động này",
		agent.IntentComposite:       "thực hiện các bước",
	}

	if desc, exists := descriptions[intentType]; exists {
		return desc
	}

	return "hoàn thành yêu cầu"
}

// getParameterDescription returns a user-friendly description of a parameter
func (pv *ParameterValidator) getParameterDescription(paramName string) string {
	descriptions := map[string]string{
		"path":    "đường dẫn file",
		"to":      "địa chỉ email người nhận",
		"subject": "tiêu đề email",
		"body":    "nội dung email",
		"command": "lệnh cần chạy",
		"query":   "từ khóa tìm kiếm",
		"confirm": "xác nhận thực hiện",
	}

	if desc, exists := descriptions[paramName]; exists {
		return desc
	}

	return paramName
}

// ExtractParametersFromInput attempts to extract parameters from user input
// This is a heuristic approach and may not always be accurate
func (pv *ParameterValidator) ExtractParametersFromInput(
	input string,
	toolName string,
) (map[string]interface{}, error) {
	tool, err := agent.GetTool(toolName)
	if err != nil {
		return nil, err
	}

	params := make(map[string]interface{})

	// Extract parameters based on tool type
	switch toolName {
	case "read_file", "delete_file", "write_file":
		// Try to extract file path
		if path := pv.extractFilePath(input); path != "" {
			params["path"] = path
		}

	case "send_email":
		// Try to extract email components
		if to := pv.extractEmail(input); to != "" {
			params["to"] = to
		}
		// Extract subject and body would require more sophisticated parsing

	case "exec":
		// Try to extract command
		if cmd := pv.extractCommand(input); cmd != "" {
			params["command"] = cmd
		}

	case "web_search":
		// The entire input might be the query
		params["query"] = input
	}

	// Validate extracted parameters
	for _, paramDef := range tool.Parameters {
		if paramDef.Required {
			if _, exists := params[paramDef.Name]; !exists {
				// Parameter not extracted
				continue
			}

			// Validate if validator exists
			if paramDef.Validator != nil {
				if err := paramDef.Validator(params[paramDef.Name]); err != nil {
					delete(params, paramDef.Name) // Remove invalid parameter
				}
			}
		}
	}

	return params, nil
}

// extractFilePath attempts to extract a file path from input
func (pv *ParameterValidator) extractFilePath(input string) string {
	// Look for common path patterns
	// This is a simple heuristic and may need improvement
	words := strings.Fields(input)
	for _, word := range words {
		// Check if word looks like a path
		if strings.Contains(word, "/") || strings.Contains(word, "\\") {
			return word
		}
		// Check for file extensions
		if strings.Contains(word, ".") {
			parts := strings.Split(word, ".")
			if len(parts) > 1 {
				// Has extension, likely a filename
				return word
			}
		}
	}
	return ""
}

// extractEmail attempts to extract an email address from input
func (pv *ParameterValidator) extractEmail(input string) string {
	words := strings.Fields(input)
	for _, word := range words {
		if strings.Contains(word, "@") {
			// Validate it's a proper email
			if err := agent.ValidateEmail(word); err == nil {
				return word
			}
		}
	}
	return ""
}

// extractCommand attempts to extract a command from input
func (pv *ParameterValidator) extractCommand(input string) string {
	// Look for common command patterns
	inputLower := strings.ToLower(input)

	// Helper: find keyword in lower, extract suffix from original input
	extractAfter := func(keyword string) string {
		idx := strings.Index(inputLower, keyword)
		if idx < 0 {
			return ""
		}
		suffix := strings.TrimSpace(input[idx+len(keyword):])
		return suffix
	}

	// Vietnamese patterns
	if strings.Contains(inputLower, "chạy lệnh") {
		if cmd := extractAfter("chạy lệnh"); cmd != "" {
			return cmd
		}
	}

	// English patterns
	if strings.Contains(inputLower, "run command") {
		if cmd := extractAfter("run command"); cmd != "" {
			return cmd
		}
	}

	if strings.Contains(inputLower, "execute") {
		if cmd := extractAfter("execute"); cmd != "" {
			return cmd
		}
	}

	return ""
}

// removeDuplicates removes duplicate strings from a slice
func (pv *ParameterValidator) removeDuplicates(slice []string) []string {
	seen := make(map[string]bool)
	result := []string{}

	for _, item := range slice {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}

// ValidateAllParameters validates all parameters in the intent
func (pv *ParameterValidator) ValidateAllParameters(intent *agent.Intent) error {
	validation, err := pv.Validate(intent)
	if err != nil {
		return err
	}

	if !validation.IsValid {
		return fmt.Errorf("validation failed: missing parameters: %v", validation.Missing)
	}

	return nil
}

// GetMissingParameters returns a list of missing required parameters
func (pv *ParameterValidator) GetMissingParameters(intent *agent.Intent) ([]string, error) {
	validation, err := pv.Validate(intent)
	if err != nil {
		return nil, err
	}

	return validation.Missing, nil
}

// IsValid checks if all required parameters are present and valid
func (pv *ParameterValidator) IsValid(intent *agent.Intent) bool {
	validation, err := pv.Validate(intent)
	if err != nil {
		return false
	}

	return validation.IsValid
}
