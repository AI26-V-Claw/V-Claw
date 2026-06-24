package contracts

import (
	"fmt"
	"time"
)

type AgentStatus string

const (
	AgentStatusCompleted                AgentStatus = "completed"
	AgentStatusApprovalRequired         AgentStatus = "approval_required"
	AgentStatusNeedClarification        AgentStatus = "need_clarification"
	AgentStatusFailed                   AgentStatus = "failed"
	AgentStatusBlocked                  AgentStatus = "blocked"
	AgentStatusIterationBudgetExhausted AgentStatus = "iteration_budget_exhausted"
)

type UserOutputKind string

const (
	UserOutputKindMessage  UserOutputKind = "message"
	UserOutputKindSuccess  UserOutputKind = "success"
	UserOutputKindError    UserOutputKind = "error"
	UserOutputKindClarify  UserOutputKind = "clarify"
	UserOutputKindApproval UserOutputKind = "approval"
	UserOutputKindProgress UserOutputKind = "progress"
	UserOutputKindExpired  UserOutputKind = "expired"
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
	ApprovalStatusRevised   ApprovalStatus = "revised"
	ApprovalStatusExpired   ApprovalStatus = "expired"
	ApprovalStatusCancelled ApprovalStatus = "cancelled"
)

type ApprovalDecisionStatus string

const (
	ApprovalDecisionApproved ApprovalDecisionStatus = "approved"
	ApprovalDecisionRejected ApprovalDecisionStatus = "rejected"
	ApprovalDecisionRevised  ApprovalDecisionStatus = "revised"
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
	ErrorInvalidInput             = "INVALID_INPUT"
	ErrorMissingRequiredField     = "MISSING_REQUIRED_FIELD"
	ErrorToolNotFound             = "TOOL_NOT_FOUND"
	ErrorToolInputInvalid         = "TOOL_INPUT_INVALID"
	ErrorProviderError            = "PROVIDER_ERROR"
	ErrorProviderTimeout          = "PROVIDER_TIMEOUT"
	ErrorProviderUnavailable      = "PROVIDER_UNAVAILABLE"
	ErrorActionRequiresApproval   = "ACTION_REQUIRES_APPROVAL"
	ErrorActionBlockedByPolicy    = "ACTION_BLOCKED_BY_POLICY"
	ErrorApprovalNotFound         = "APPROVAL_NOT_FOUND"
	ErrorApprovalExpired          = "APPROVAL_EXPIRED"
	ErrorInternal                 = "INTERNAL_ERROR"
	ErrorIterationBudgetExhausted = "ITERATION_BUDGET_EXHAUSTED"
)

// GovernanceMetadata captures the provenance fields that must be attached to
// every significant runtime record (run, tool call, approval, risk decision,
// audit entry). Consumers such as the N4 monitoring UI use these fields to
// trace which model, prompt version, tool schema version, and policy decision
// produced a given result — without having to join across tables or read
// process memory.
//
// All fields are optional strings so existing records without governance data
// round-trip cleanly (omitempty keeps JSON compact).
type GovernanceMetadata struct {
	// Model is the LLM model ID used for this request
	// (e.g. "claude-opus-4-8", "gemini-1.5-pro").
	Model string `json:"model,omitempty"`
	// PromptVersion is a short content-hash fingerprint of the effective system
	// prompt (runtimeSystemPrompt). Changes automatically when the
	// prompt changes; computed by governance.PromptVersion().
	PromptVersion string `json:"promptVersion,omitempty"`
	// ToolSchemaVersion is a short content-hash fingerprint of the tool's
	// parameter schema at call time. Computed by governance.ToolSchemaVersion().
	ToolSchemaVersion string `json:"toolSchemaVersion,omitempty"`
	// PolicyDecisionRef is a composite reference back to the risk decision that
	// classified this tool call. Format: "policy:<runID>:<toolCallID>:<unixSec>".
	// Computed by governance.PolicyRef().
	PolicyDecisionRef string `json:"policyDecisionRef,omitempty"`
}

type UserMessage struct {
	RequestID string         `json:"requestId"`
	SessionID string         `json:"sessionId"`
	Channel   string         `json:"channel"`
	Text      string         `json:"text"`
	Locale    string         `json:"locale,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type ArtifactRef struct {
	Kind  string         `json:"kind,omitempty"`
	Label string         `json:"label,omitempty"`
	URI   string         `json:"uri,omitempty"`
	ID    string         `json:"id,omitempty"`
	Meta  map[string]any `json:"meta,omitempty"`
}

type UserOutput struct {
	Kind        UserOutputKind `json:"kind"`
	Text        string         `json:"text"`
	ArtifactRef *ArtifactRef   `json:"artifactRef,omitempty"`
	Meta        map[string]any `json:"meta,omitempty"`
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
	// FailureReason is a machine-readable reason for non-completed status. Empty when Status is completed.
	FailureReason string      `json:"failureReason,omitempty"`
	Plan          *Plan       `json:"plan,omitempty"`
	Output        *UserOutput `json:"output,omitempty"`
}

type ToolCall struct {
	ToolCallID string         `json:"toolCallId"`
	RequestID  string         `json:"requestId,omitempty"`
	SessionID  string         `json:"sessionId,omitempty"`
	ToolName   string         `json:"toolName"`
	Input      map[string]any `json:"input,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	// Governance carries provenance metadata for this tool call.
	// Populated by the agent runtime before the call crosses any boundary.
	Governance *GovernanceMetadata `json:"governance,omitempty"`
}

// ToolResult carries the outcome of a single tool execution across the contract
// boundary (e.g. from agent to channel, or into audit/approval flows).
type ToolResult struct {
	ToolCallID string      `json:"toolCallId"`
	ToolName   string      `json:"toolName"`
	Success    bool        `json:"success"`
	Data       any         `json:"data,omitempty"`
	Error      *ErrorShape `json:"error,omitempty"`

	// ArtifactRef references the primary resource this tool accessed or produced.
	// Mirrors tools.ToolArtifactRef but scoped to the shared contracts package.
	ArtifactRef *ArtifactRef `json:"artifactRef,omitempty"`
	// Metadata holds optional structured key-value pairs (e.g. line counts, byte sizes).
	Metadata map[string]any `json:"metadata,omitempty"`
	// Truncated is true when the content payload was cut short due to size limits.
	Truncated bool `json:"truncated,omitempty"`
	// Redacted is true when ContentForLLM was sanitized before inclusion in the LLM context.
	Redacted bool `json:"redacted,omitempty"`
	// Source identifies the origin layer that produced this result, e.g.
	// "tool:gmail", "connector:tavily", "tool:sandbox.python". Populated by the
	// tool layer and consumed by audit/N4 to group records by origin without
	// parsing tool names. Use the prefixes from internal/governance.
	Source string `json:"source,omitempty"`
	// Governance carries the same provenance fields as ToolCall.Governance. The
	// agent runtime copies them through after execution so consumers reading
	// only the result still know which model/prompt/schema/policy produced it.
	Governance *GovernanceMetadata `json:"governance,omitempty"`
}

// ValidateToolResult checks that r satisfies the ToolResult contract invariants:
//   - ToolCallID and ToolName must not be empty.
//   - If Success is false, Error must be non-nil.
//   - If Success is true, Error must be nil.
//
// Returns a descriptive error on violation, nil otherwise.
func ValidateToolResult(r ToolResult) error {
	if r.ToolCallID == "" {
		return fmt.Errorf("ToolResult.ToolCallID must not be empty")
	}
	if r.ToolName == "" {
		return fmt.Errorf("ToolResult.ToolName must not be empty")
	}
	if !r.Success && r.Error == nil {
		return fmt.Errorf("ToolResult.Error must not be nil when Success is false (tool=%s)", r.ToolName)
	}
	if r.Success && r.Error != nil {
		return fmt.Errorf("ToolResult.Error must be nil when Success is true (tool=%s, code=%s)", r.ToolName, r.Error.Code)
	}
	return nil
}

type RiskDecision struct {
	ToolCallID       string             `json:"toolCallId"`
	ToolName         string             `json:"toolName"`
	RiskLevel        RiskLevel          `json:"riskLevel"`
	Decision         RiskDecisionStatus `json:"decision"`
	RequiresApproval bool               `json:"requiresApproval"`
	Reason           string             `json:"reason,omitempty"`
	CheckedAt        time.Time          `json:"checkedAt"`
	// PolicyDecisionRef is a composite reference, identical to the value
	// stored on tool calls / approvals / audit entries that descend from this
	// risk decision. It lets consumers correlate records without joining on
	// the surrogate id. Format: "policy:<runID>:<toolCallId>:<unixSec>".
	PolicyDecisionRef string `json:"policyDecisionRef,omitempty"`
}

type ApprovalRequest struct {
	ApprovalID       string         `json:"approvalId"`
	ParentApprovalID string         `json:"parentApprovalId,omitempty"`
	RequestID        string         `json:"requestId"`
	SessionID        string         `json:"sessionId"`
	ToolCallID       string         `json:"toolCallId"`
	Status           ApprovalStatus `json:"status"`
	RiskLevel        RiskLevel      `json:"riskLevel"`
	Summary          string         `json:"summary"`
	Details          string         `json:"details,omitempty"`
	ToolCall         ToolCall       `json:"toolCall"`
	CreatedAt        time.Time      `json:"createdAt"`
	ExpiresAt        time.Time      `json:"expiresAt"`
	// Governance carries provenance metadata so the approval record is
	// self-contained for audit/trace without joining back to the run or
	// tool_call tables.
	Governance *GovernanceMetadata `json:"governance,omitempty"`
}

type ApprovalDecision struct {
	ApprovalID string                 `json:"approvalId"`
	RequestID  string                 `json:"requestId"`
	Decision   ApprovalDecisionStatus `json:"decision"`
	DecidedBy  string                 `json:"decidedBy,omitempty"`
	Channel    string                 `json:"channel,omitempty"`
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
