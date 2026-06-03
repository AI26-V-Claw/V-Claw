package intent

import "time"

// IntentType represents the classification of user intent.
type IntentType string

const (
	TypeGreeting        IntentType = "GREETING"
	TypeReadInfo        IntentType = "READ_INFO"
	TypeDangerousAction IntentType = "DANGEROUS_ACTION"
	TypeComposite       IntentType = "COMPOSITE_ACTION"
	TypeUnknown         IntentType = "UNKNOWN"
)

// Result represents the full output of the Intent Classifier.
// This struct maps directly to the JSON returned by the LLM system prompt.
type Result struct {
	Type           IntentType             `json:"intent_type"`
	Confidence     float64                `json:"confidence"`      // 0.0 - 1.0
	RequiredParams []string               `json:"required_params"` // Parameters required by the tool
	ProvidedParams map[string]interface{} `json:"provided_params"` // Parameters extracted from user input
	MissingParams  []string               `json:"missing_params"`  // Required params not found in input
	ToolCalls      []ToolCallInfo         `json:"tool_calls"`      // Tools the LLM wants to invoke
	NeedsConfirm   bool                   `json:"needs_confirm"`   // Requires user confirmation?
	Reasoning      string                 `json:"reasoning"`       // LLM's explanation (Vietnamese)
	Timestamp      time.Time              `json:"timestamp"`
}

// ToolCallInfo represents a single tool the LLM proposes to call.
type ToolCallInfo struct {
	Name       string                 `json:"name"`
	Category   string                 `json:"category"` // SAFE_READ, DANGEROUS_WRITE, EXECUTION, COMMUNICATION
	Parameters map[string]interface{} `json:"parameters"`
	Timeout    int                    `json:"timeout"` // seconds
}

// ClassificationOutput wraps Result with clarification metadata.
type ClassificationOutput struct {
	Intent               *Result              `json:"intent"`
	NeedsClarification   bool                 `json:"needs_clarification"`
	ClarificationMessage string               `json:"clarification_message,omitempty"` // Vietnamese message asking user for more info
	ClarificationOptions *ClarificationChoice `json:"clarification_options,omitempty"`
}

// ClarificationChoice presents multiple-choice options when intent is ambiguous.
type ClarificationChoice struct {
	Question string   `json:"question"`
	Options  []Option `json:"options"`
}

// Option represents one choice in a clarification dialog.
type Option struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	IntentType IntentType `json:"intent_type"`
}
