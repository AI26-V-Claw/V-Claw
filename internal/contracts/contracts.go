package contracts

import "time"

type AgentStatus string

const (
	AgentStatusCompleted            AgentStatus = "completed"
	AgentStatusApprovalRequired     AgentStatus = "approval_required"
	AgentStatusNeedClarification    AgentStatus = "need_clarification"
	AgentStatusFailed               AgentStatus = "failed"
	AgentStatusBlocked              AgentStatus = "blocked"
	AgentStatusMaxIterationsReached AgentStatus = "max_iterations_reached"
)

type RiskLevel string

const (
	RiskLevelSafeRead      RiskLevel = "safe_read"
	RiskLevelSafeCompute   RiskLevel = "safe_compute"
	RiskLevelSensitiveRead RiskLevel = "sensitive_read"
	RiskLevelExternalWrite RiskLevel = "external_write"
	RiskLevelLocalWrite    RiskLevel = "local_write"
	RiskLevelCodeExecution RiskLevel = "code_execution"
	RiskLevelDestructive   RiskLevel = "destructive"
)

type RiskDecisionStatus string

const (
	RiskDecisionAllow            RiskDecisionStatus = "allow"
	RiskDecisionRequiresApproval RiskDecisionStatus = "requires_approval"
	RiskDecisionBlock            RiskDecisionStatus = "block"
)

type ApprovalStatus string

const (
	ApprovalStatusPending   ApprovalStatus = "pending"
	ApprovalStatusApproved  ApprovalStatus = "approved"
	ApprovalStatusRejected  ApprovalStatus = "rejected"
	ApprovalStatusExpired   ApprovalStatus = "expired"
	ApprovalStatusCancelled ApprovalStatus = "cancelled"
)

type ApprovalDecisionStatus string

const (
	ApprovalDecisionApproved ApprovalDecisionStatus = "approved"
	ApprovalDecisionRejected ApprovalDecisionStatus = "rejected"
)

type ErrorSource string

const (
	ErrorSourceAgent    ErrorSource = "agent"
	ErrorSourceProvider ErrorSource = "provider"
	ErrorSourcePolicy   ErrorSource = "policy"
	ErrorSourceTool     ErrorSource = "tool"
	ErrorSourceSession  ErrorSource = "session"
)

const (
	ErrorInvalidInput           = "INVALID_INPUT"
	ErrorMissingRequiredField   = "MISSING_REQUIRED_FIELD"
	ErrorToolNotFound           = "TOOL_NOT_FOUND"
	ErrorToolInputInvalid       = "TOOL_INPUT_INVALID"
	ErrorProviderError          = "PROVIDER_ERROR"
	ErrorProviderTimeout        = "PROVIDER_TIMEOUT"
	ErrorActionRequiresApproval = "ACTION_REQUIRES_APPROVAL"
	ErrorActionBlockedByPolicy  = "ACTION_BLOCKED_BY_POLICY"
	ErrorInternal               = "INTERNAL_ERROR"
	ErrorMaxIterationsExceeded  = "MAX_ITERATIONS_EXCEEDED"
)

type UserMessage struct {
	RequestID string         `json:"requestId"`
	SessionID string         `json:"sessionId"`
	Channel   string         `json:"channel"`
	Text      string         `json:"text"`
	Locale    string         `json:"locale,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type AgentResponse struct {
	RequestID       string           `json:"requestId"`
	SessionID       string           `json:"sessionId"`
	Status          AgentStatus      `json:"status"`
	Message         string           `json:"message,omitempty"`
	ApprovalID      string           `json:"approvalId,omitempty"`
	ApprovalRequest *ApprovalRequest `json:"approvalRequest,omitempty"`
	Data            map[string]any   `json:"data,omitempty"`
	ToolResults     []ToolResult     `json:"toolResults,omitempty"`
	Error           *ErrorShape      `json:"error,omitempty"`
	Plan            *Plan            `json:"plan,omitempty"`
}

type ToolCall struct {
	ToolCallID string         `json:"toolCallId"`
	RequestID  string         `json:"requestId,omitempty"`
	SessionID  string         `json:"sessionId,omitempty"`
	ToolName   string         `json:"toolName"`
	Input      map[string]any `json:"input,omitempty"`
	Reason     string         `json:"reason,omitempty"`
}

type ToolResult struct {
	ToolCallID string      `json:"toolCallId"`
	ToolName   string      `json:"toolName"`
	Success    bool        `json:"success"`
	Data       any         `json:"data,omitempty"`
	Error      *ErrorShape `json:"error,omitempty"`
}

type RiskDecision struct {
	ToolCallID       string             `json:"toolCallId"`
	ToolName         string             `json:"toolName"`
	RiskLevel        RiskLevel          `json:"riskLevel"`
	Decision         RiskDecisionStatus `json:"decision"`
	RequiresApproval bool               `json:"requiresApproval"`
	Reason           string             `json:"reason,omitempty"`
	CheckedAt        time.Time          `json:"checkedAt"`
}

type ApprovalRequest struct {
	ApprovalID string         `json:"approvalId"`
	RequestID  string         `json:"requestId"`
	SessionID  string         `json:"sessionId"`
	ToolCallID string         `json:"toolCallId"`
	Status     ApprovalStatus `json:"status"`
	RiskLevel  RiskLevel      `json:"riskLevel"`
	Summary    string         `json:"summary"`
	Details    string         `json:"details,omitempty"`
	ToolCall   ToolCall       `json:"toolCall"`
	CreatedAt  time.Time      `json:"createdAt"`
	ExpiresAt  time.Time      `json:"expiresAt"`
}

type ApprovalDecision struct {
	ApprovalID string                 `json:"approvalId"`
	RequestID  string                 `json:"requestId"`
	Decision   ApprovalDecisionStatus `json:"decision"`
	DecidedBy  string                 `json:"decidedBy,omitempty"`
	DecidedAt  time.Time              `json:"decidedAt"`
	Comment    string                 `json:"comment,omitempty"`
}

type ErrorShape struct {
	Code         string         `json:"code"`
	Message      string         `json:"message"`
	Source       ErrorSource    `json:"source,omitempty"`
	Details      map[string]any `json:"details,omitempty"`
	Retryable    bool           `json:"retryable"`
	RetryAfterMs int            `json:"retryAfterMs,omitempty"`
}

type Plan struct {
	Steps []PlanStep `json:"steps,omitempty"`
}

type PlanStep struct {
	ID          string `json:"id,omitempty"`
	Description string `json:"description"`
	Status      string `json:"status"`
}
