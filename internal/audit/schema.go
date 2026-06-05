// Package audit defines the V-Claw audit log schema and Logger interface.
//
// Every action in the sandbox pipeline produces one or more AuditEvents:
//
//	Tool Request
//	    │  EventToolRequest
//	    ▼
//	Policy Decision
//	    │  EventPolicyDecision
//	    ├─ allow ──────────────────────────────────────────────────┐
//	    └─ requires_approval → EventHITLProposal                   │
//	                            │                                   │
//	                 ┌──────────┴──────────┐                       │
//	         EventHITLApproved    EventHITLRejected                │
//	                 │                    │                        │
//	                 ▼                    ▼                        │
//	         EventExecutionStart   (log & stop)                   │
//	                 │                                             │
//	         EventExecutionResult ◄──────────────────────────────-┘
//	         (executed / failed / timeout)
//
// Lifecycle states for audit queries:
//
//	proposed → approved/rejected → (approved only) executing → executed/failed/timeout
//	any stage → blocked (if policy blocks)
package audit

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// ─── Event Type ───────────────────────────────────────────────────────────────

// EventType identifies what happened at each step of the pipeline.
type EventType string

const (
	// EventToolRequest is logged when the Agent Planner submits a tool request.
	EventToolRequest EventType = "tool_request"

	// EventPolicyDecision is logged when the PolicyChecker classifies the request.
	EventPolicyDecision EventType = "policy_decision"

	// EventHITLProposal is logged when a requires_approval request is held and
	// a HITL proposal is sent to the user.
	EventHITLProposal EventType = "hitl_proposal"

	// EventHITLApproved is logged when the user approves a HITL proposal.
	EventHITLApproved EventType = "hitl_approved"

	// EventHITLRejected is logged when the user rejects a HITL proposal.
	EventHITLRejected EventType = "hitl_rejected"

	// EventExecutionStart is logged immediately before the sandbox container
	// is dispatched.
	EventExecutionStart EventType = "execution_start"

	// EventExecutionResult is logged when a job completes (success, failed,
	// timeout, or context-cancelled).
	EventExecutionResult EventType = "execution_result"

	// EventBlocked is logged when the PolicyChecker blocks a request outright.
	// The request is never dispatched to the sandbox.
	EventBlocked EventType = "blocked"
)

// ─── Lifecycle Status ─────────────────────────────────────────────────────────

// LifecycleStatus is the current state of a tool request in the pipeline.
// It is stored alongside each AuditEvent so queries can filter by stage.
type LifecycleStatus string

const (
	// StatusProposed — HITL proposal sent, waiting for user action.
	StatusProposed LifecycleStatus = "proposed"

	// StatusApproved — user approved the HITL proposal.
	StatusApproved LifecycleStatus = "approved"

	// StatusRejected — user rejected the HITL proposal.
	StatusRejected LifecycleStatus = "rejected"

	// StatusBlocked — request blocked by policy; never executed.
	StatusBlocked LifecycleStatus = "blocked"

	// StatusExecuting — sandbox job is currently running.
	StatusExecuting LifecycleStatus = "executing"

	// StatusExecuted — job completed with exit code 0.
	StatusExecuted LifecycleStatus = "executed"

	// StatusFailed — job completed with non-zero exit code or runner error.
	StatusFailed LifecycleStatus = "failed"

	// StatusTimeout — job was killed because it exceeded the time limit.
	StatusTimeout LifecycleStatus = "timeout"
)

// ─── Action Type ──────────────────────────────────────────────────────────────

// ActionType classifies the kind of operation the request is attempting.
// It is a human-readable label used in HITL proposals and audit reports.
type ActionType string

const (
	ActionRunPython      ActionType = "run_python"
	ActionRunShell       ActionType = "run_shell"
	ActionFileRead       ActionType = "file_read"
	ActionFileWrite      ActionType = "file_write"
	ActionFileDelete     ActionType = "file_delete"
	ActionFileMove       ActionType = "file_move"
	ActionFileCopy       ActionType = "file_copy"
	ActionFileList       ActionType = "file_list"
	ActionSystemShutdown ActionType = "system_shutdown"
	ActionServiceControl ActionType = "service_control"
	ActionRegistryAccess ActionType = "registry_access"
	ActionNetworkAccess  ActionType = "network_access"
	ActionCredentialRead ActionType = "credential_read"
)

// ─── AuditEvent ───────────────────────────────────────────────────────────────

// AuditEvent is the canonical record written to the audit log for every
// significant step in the sandbox pipeline.
//
// JSON field names match the V-Claw API contract (section 11 of the
// sandbox-scrum-plan.md) so events can be forwarded directly to external
// log aggregators.
type AuditEvent struct {
	// ── Identity ───────────────────────────────────────────────────────────

	// EventID is a unique identifier for this log entry.
	EventID string `json:"event_id"`

	// EventType classifies what happened (tool_request, policy_decision, …).
	EventType EventType `json:"event_type"`

	// Timestamp is the UTC time this event was recorded.
	Timestamp time.Time `json:"timestamp"`

	// ── Session / Request ──────────────────────────────────────────────────

	// SessionID ties the event to the active user session.
	SessionID string `json:"session_id"`

	// UserID identifies the authenticated user who triggered the request.
	UserID string `json:"user_id"`

	// RequestID correlates all events belonging to the same tool invocation.
	RequestID string `json:"request_id"`

	// JobID is the sandbox execution ID, set after the runner dispatches the job.
	// Empty for events that occur before execution.
	JobID string `json:"job_id,omitempty"`

	// ── Tool / Action ──────────────────────────────────────────────────────

	// Tool is the name of the tool being invoked (sandbox.runPython, sandbox.runShell, …).
	Tool string `json:"tool"`

	// ActionType classifies the operation at a semantic level.
	ActionType ActionType `json:"action_type"`

	// ── Policy / Risk ──────────────────────────────────────────────────────

	// RiskLevel is the risk classification assigned by the PolicyChecker.
	RiskLevel string `json:"risk_level"`

	// PolicyDecision is the PolicyChecker outcome: allow, requires_approval, block.
	PolicyDecision string `json:"policy_decision"`

	// PolicyReasons is the list of Vietnamese explanations from the checker.
	PolicyReasons []string `json:"policy_reasons,omitempty"`

	// ── Lifecycle ──────────────────────────────────────────────────────────

	// Status is the current lifecycle state of the request.
	Status LifecycleStatus `json:"status"`

	// ── Command / Code ─────────────────────────────────────────────────────

	// CommandHash is the SHA-256 digest of the raw command or code.
	// Allows deduplication and tamper detection in downstream log systems.
	CommandHash string `json:"command_hash"`

	// CommandPreview is a truncated, safe-to-display version of the command
	// or code. Never contains the full content if it is long.
	CommandPreview string `json:"command_preview"`

	// AffectedPaths lists file or resource paths that will be modified or
	// accessed by the action. Extracted from the command/code by the policy
	// layer. May be empty.
	AffectedPaths []string `json:"affected_paths,omitempty"`

	// ── HITL ───────────────────────────────────────────────────────────────

	// HITLApprovalID is the unique ID of the HITL proposal, set when
	// EventType is EventHITLProposal, EventHITLApproved, or EventHITLRejected.
	HITLApprovalID string `json:"hitl_approval_id,omitempty"`

	// HITLSummaryVI is the Vietnamese summary shown to the user in the
	// HITL proposal.
	HITLSummaryVI string `json:"hitl_summary_vi,omitempty"`

	// HITLReasonVI is the Vietnamese explanation of why the action requires
	// approval.
	HITLReasonVI string `json:"hitl_reason_vi,omitempty"`

	// ── Execution Result ───────────────────────────────────────────────────

	// ResultStatus mirrors JobStatus from the sandbox runner
	// (success, failed, timeout, blocked, rejected).
	ResultStatus string `json:"result_status,omitempty"`

	// ExitCode is the process exit code from the sandbox container.
	ExitCode int `json:"exit_code,omitempty"`

	// DurationMs is the wall-clock execution time in milliseconds.
	DurationMs int64 `json:"duration_ms,omitempty"`

	// OutputSummary is a truncated summary of stdout/stderr output.
	// Full output is not stored in the audit log for brevity.
	OutputSummary string `json:"output_summary,omitempty"`

	// OutputTruncated is true when the sandbox output exceeded the size limit.
	OutputTruncated bool `json:"output_truncated,omitempty"`

	// ── Error ──────────────────────────────────────────────────────────────

	// ErrorMessage contains the error description if the runner or policy
	// layer returned an error (as opposed to the user command failing).
	ErrorMessage string `json:"error_message,omitempty"`
}

// ─── Constructor helpers ──────────────────────────────────────────────────────

// NewToolRequestEvent creates an AuditEvent for the initial tool request.
func NewToolRequestEvent(requestID, sessionID, userID, tool string, action ActionType, command string) AuditEvent {
	return AuditEvent{
		EventID:        newEventID(),
		EventType:      EventToolRequest,
		Timestamp:      time.Now().UTC(),
		SessionID:      sessionID,
		UserID:         userID,
		RequestID:      requestID,
		Tool:           tool,
		ActionType:     action,
		Status:         StatusProposed,
		CommandHash:    HashCommand(command),
		CommandPreview: PreviewCommand(command, 120),
	}
}

// NewPolicyEvent creates an AuditEvent for a PolicyChecker decision.
func NewPolicyEvent(base AuditEvent, riskLevel, decision string, reasons []string) AuditEvent {
	ev := base
	ev.EventID = newEventID()
	ev.EventType = EventPolicyDecision
	ev.Timestamp = time.Now().UTC()
	ev.RiskLevel = riskLevel
	ev.PolicyDecision = decision
	ev.PolicyReasons = reasons

	switch decision {
	case "block":
		ev.Status = StatusBlocked
	case "requires_approval":
		ev.Status = StatusProposed
	default:
		ev.Status = StatusApproved
	}
	return ev
}

// NewHITLProposalEvent creates an AuditEvent when a HITL proposal is sent.
func NewHITLProposalEvent(base AuditEvent, approvalID, summaryVI, reasonVI string, paths []string) AuditEvent {
	ev := base
	ev.EventID = newEventID()
	ev.EventType = EventHITLProposal
	ev.Timestamp = time.Now().UTC()
	ev.Status = StatusProposed
	ev.HITLApprovalID = approvalID
	ev.HITLSummaryVI = summaryVI
	ev.HITLReasonVI = reasonVI
	ev.AffectedPaths = paths
	return ev
}

// NewHITLApprovedEvent creates an AuditEvent when the user approves a proposal.
func NewHITLApprovedEvent(base AuditEvent, approvalID string) AuditEvent {
	ev := base
	ev.EventID = newEventID()
	ev.EventType = EventHITLApproved
	ev.Timestamp = time.Now().UTC()
	ev.Status = StatusApproved
	ev.HITLApprovalID = approvalID
	return ev
}

// NewHITLRejectedEvent creates an AuditEvent when the user rejects a proposal.
func NewHITLRejectedEvent(base AuditEvent, approvalID string) AuditEvent {
	ev := base
	ev.EventID = newEventID()
	ev.EventType = EventHITLRejected
	ev.Timestamp = time.Now().UTC()
	ev.Status = StatusRejected
	ev.HITLApprovalID = approvalID
	return ev
}

// NewBlockedEvent creates an AuditEvent when a request is blocked by policy.
func NewBlockedEvent(base AuditEvent, riskLevel string, reasons []string) AuditEvent {
	ev := base
	ev.EventID = newEventID()
	ev.EventType = EventBlocked
	ev.Timestamp = time.Now().UTC()
	ev.Status = StatusBlocked
	ev.RiskLevel = riskLevel
	ev.PolicyDecision = "block"
	ev.PolicyReasons = reasons
	return ev
}

// NewExecutionStartEvent creates an AuditEvent when execution begins.
func NewExecutionStartEvent(base AuditEvent, jobID string) AuditEvent {
	ev := base
	ev.EventID = newEventID()
	ev.EventType = EventExecutionStart
	ev.Timestamp = time.Now().UTC()
	ev.Status = StatusExecuting
	ev.JobID = jobID
	return ev
}

// NewExecutionResultEvent creates an AuditEvent for the execution outcome.
func NewExecutionResultEvent(base AuditEvent, jobID, resultStatus string, exitCode int, durationMs int64, outputSummary string, truncated bool) AuditEvent {
	ev := base
	ev.EventID = newEventID()
	ev.EventType = EventExecutionResult
	ev.Timestamp = time.Now().UTC()
	ev.JobID = jobID
	ev.ResultStatus = resultStatus
	ev.ExitCode = exitCode
	ev.DurationMs = durationMs
	ev.OutputSummary = outputSummary
	ev.OutputTruncated = truncated

	switch resultStatus {
	case "success":
		ev.Status = StatusExecuted
	case "timeout":
		ev.Status = StatusTimeout
	default:
		ev.Status = StatusFailed
	}
	return ev
}

// ─── Utility functions ────────────────────────────────────────────────────────

// HashCommand returns a "sha256:<hex>" fingerprint of the command or code.
// Used for deduplication and tamper detection.
func HashCommand(command string) string {
	sum := sha256.Sum256([]byte(command))
	return fmt.Sprintf("sha256:%x", sum)
}

// PreviewCommand returns a safe, truncated view of a command or code string.
// The preview is suitable for display in HITL proposals and log entries.
func PreviewCommand(command string, maxLen int) string {
	s := strings.TrimSpace(command)
	// Replace newlines with spaces for single-line display.
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

// SummariseOutput returns a brief summary of stdout/stderr for the audit log.
// It takes the first N bytes of each stream.
func SummariseOutput(stdout, stderr string, maxBytes int) string {
	var parts []string
	if s := strings.TrimSpace(stdout); s != "" {
		if len(s) > maxBytes {
			s = s[:maxBytes] + "…"
		}
		parts = append(parts, "stdout: "+s)
	}
	if s := strings.TrimSpace(stderr); s != "" {
		if len(s) > maxBytes {
			s = s[:maxBytes] + "…"
		}
		parts = append(parts, "stderr: "+s)
	}
	if len(parts) == 0 {
		return "(no output)"
	}
	return strings.Join(parts, " | ")
}

// ─── Event ID generator ───────────────────────────────────────────────────────

var eventSeq uint64

func newEventID() string {
	n := atomic.AddUint64(&eventSeq, 1)
	return fmt.Sprintf("evt_%d_%06d", time.Now().UnixNano()/1e6, n%1_000_000)
}
