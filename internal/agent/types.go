package agent

import "time"

// IntentType represents the classification of user intent
type IntentType string

const (
	IntentGreeting        IntentType = "GREETING"
	IntentReadInfo        IntentType = "READ_INFO"
	IntentDangerousAction IntentType = "DANGEROUS_ACTION"
	IntentComposite       IntentType = "COMPOSITE_ACTION"
	IntentUnknown         IntentType = "UNKNOWN"
)

// Intent represents the result of intent classification
type Intent struct {
	Type           IntentType             `json:"intent_type"`
	Confidence     float64                `json:"confidence"`      // 0.0 - 1.0
	RequiredParams []string               `json:"required_params"` // Required parameters
	ProvidedParams map[string]interface{} `json:"provided_params"` // Parameters provided by user
	MissingParams  []string               `json:"missing_params"`  // Missing required parameters
	ToolCalls      []ToolCall             `json:"tool_calls"`      // List of tools to call
	NeedsConfirm   bool                   `json:"needs_confirm"`   // Requires user confirmation?
	Reasoning      string                 `json:"reasoning"`       // Reason for classification
	Timestamp      time.Time              `json:"timestamp"`
}

// ToolCall represents a single tool invocation
type ToolCall struct {
	Name       string                 `json:"name"`
	Category   ToolCategory           `json:"category"` // SAFE_READ, DANGEROUS_WRITE, etc.
	Parameters map[string]interface{} `json:"parameters"`
	Timeout    int                    `json:"timeout"` // seconds
}

// ToolCategory classifies tools by their risk level
type ToolCategory string

const (
	ToolCategorySafeRead       ToolCategory = "SAFE_READ"
	ToolCategoryDangerousWrite ToolCategory = "DANGEROUS_WRITE"
	ToolCategoryExecution      ToolCategory = "EXECUTION"
	ToolCategoryCommunication  ToolCategory = "COMMUNICATION"
)

// ToolDefinition defines the schema and metadata for a tool
type ToolDefinition struct {
	Name            string
	Category        ToolCategory
	Description     string
	Parameters      []ParameterDef
	Dangerous       bool
	RequiresConfirm bool
	Timeout         int // seconds
}

// ParameterDef defines a single parameter for a tool
type ParameterDef struct {
	Name        string
	Type        string // "string", "int", "bool", "path", "email"
	Required    bool
	Description string
	Validator   func(interface{}) error
}

// ParameterValidation represents the result of parameter validation
type ParameterValidation struct {
	Required []string               // Required parameters
	Provided map[string]interface{} // Parameters provided by user
	Missing  []string               // Missing required parameters
	IsValid  bool                   // Validation result
}

// ClarificationOptions represents multiple choice options for ambiguous intents
type ClarificationOptions struct {
	Question string
	Options  []Option
}

// Option represents a single choice in clarification
type Option struct {
	ID         string
	Label      string
	IntentType IntentType
	Confidence float64
}

// ClassificationResult contains the full result of intent classification
type ClassificationResult struct {
	Intent               *Intent
	NeedsClarification   bool
	ClarificationOptions *ClarificationOptions
	Error                error
}
