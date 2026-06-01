// Package policies implements the V-Claw policy layer.
//
// Every tool request from the Agent Planner must pass through the policy
// checker before reaching the sandbox executor. The checker classifies the
// request by risk level and returns a decision:
//
//   - allow          → execute immediately
//   - needs_approval → hold and surface a HITL proposal
//   - block          → reject, log, and never execute
//
// Architecture position:
//
//	Tool Request → [PolicyChecker] → allow/needs_approval/block
//	                                       ↓
//	                              HITL Gate (if needs_approval)
//	                                       ↓
//	                              Sandbox Executor
package policies

// ─── Decision ─────────────────────────────────────────────────────────────────

// Decision is the outcome of a policy check.
type Decision string

const (
	// DecisionAllow means the tool request may be executed immediately.
	DecisionAllow Decision = "allow"

	// DecisionNeedsApproval means the request must be held and presented to
	// the user for explicit approval before execution.
	DecisionNeedsApproval Decision = "needs_approval"

	// DecisionBlock means the request is rejected and must never be executed.
	DecisionBlock Decision = "block"
)

// ─── Risk Levels ─────────────────────────────────────────────────────────────

// RiskLevel describes how dangerous the requested action is.
// It is determined by the PolicyChecker independently of the final Decision
// so that audit logs capture the risk assessment regardless of outcome.
type RiskLevel string

const (
	// RiskSafeRead — read-only; e.g. list files, read CSV, view metadata.
	// Decision: allow.
	RiskSafeRead RiskLevel = "safe_read"

	// RiskSafeWrite — creates new files in workspace; e.g. write report,
	// create directory. Decision: allow (or light confirm depending on config).
	RiskSafeWrite RiskLevel = "safe_write"

	// RiskNeedsApproval — mutates or deletes existing data; e.g. delete file,
	// overwrite file, bulk rename. Decision: needs_approval.
	RiskNeedsApproval RiskLevel = "needs_approval"

	// RiskHighRisk — deep system commands or actions outside sandbox scope;
	// e.g. shutdown, service control, chmod. Decision: block.
	RiskHighRisk RiskLevel = "high_risk"

	// RiskExternalNetwork — sends or receives data over the network;
	// e.g. curl, wget, upload. Decision: needs_approval or block.
	RiskExternalNetwork RiskLevel = "external_network"

	// RiskCredentialAccess — attempts to read secrets, tokens, private keys;
	// e.g. .env, id_rsa, credentials.json. Decision: block.
	RiskCredentialAccess RiskLevel = "credential_access"
)

// ─── Tool Names ───────────────────────────────────────────────────────────────

// ToolName identifies which tool the agent is requesting.
type ToolName string

const (
	ToolRunPython ToolName = "run_python"
	ToolRunShell  ToolName = "run_shell"
	ToolFileOps   ToolName = "file_ops"
)

// ─── Policy Request ───────────────────────────────────────────────────────────

// Request is the input to the PolicyChecker.
// It mirrors the Tool Request schema from the V-Claw API contract (section 10).
type Request struct {
	// RequestID is the unique identifier assigned by the tool router.
	RequestID string

	// SessionID ties the request to an active user session.
	SessionID string

	// UserID is the authenticated user who triggered the request.
	UserID string

	// Tool identifies which sandbox tool is being invoked.
	Tool ToolName

	// Input holds the tool-specific payload for classification.
	// For run_shell: set Command.
	// For run_python: set Code and/or ScriptPath.
	// For file_ops:   set FilePath and FileOp.
	Input RequestInput

	// Meta carries user intent and source metadata for audit purposes.
	Meta RequestMeta
}

// RequestInput holds the classifiable payload extracted from the tool call.
type RequestInput struct {
	// Command is the shell expression for run_shell requests.
	Command string

	// Code is the inline Python source for run_python requests.
	Code string

	// ScriptPath is the relative path to a .py file for run_python requests.
	ScriptPath string

	// FilePath is the target path for file_ops requests.
	FilePath string

	// FileOp is the operation type for file_ops: "read", "write", "delete",
	// "copy", "move", "list".
	FileOp string
}

// RequestMeta carries contextual metadata used in audit logs.
type RequestMeta struct {
	// UserIntent is a short natural-language description of what was requested.
	UserIntent string

	// Source identifies who triggered the request: "agent" or "user_direct".
	Source string
}

// ─── Policy Result ────────────────────────────────────────────────────────────

// Result is the output of the PolicyChecker.
// It matches the Policy Result schema from the V-Claw API contract (section 10).
type Result struct {
	// RequestID echoes the originating request for correlation.
	RequestID string

	// Decision is the policy outcome: allow, needs_approval, or block.
	Decision Decision

	// RiskLevel is the assessed danger level of the request.
	RiskLevel RiskLevel

	// Reasons is a list of human-readable Vietnamese explanations for the
	// decision. Used in HITL proposals and audit logs.
	Reasons []string
}

// ─── Checker interface ────────────────────────────────────────────────────────

// Checker is the interface every policy engine must implement.
// Callers invoke Check before dispatching any tool request.
type Checker interface {
	// Check classifies req and returns the policy decision.
	// It never executes the request itself.
	Check(req Request) Result
}

// ─── Policy matrix entry ──────────────────────────────────────────────────────

// MatrixEntry maps a matched condition to its risk level, decision, and reason.
// Used internally by RuleBasedChecker to build the policy matrix.
type MatrixEntry struct {
	// Pattern is the token or substring to match against the request input.
	Pattern string

	// RiskLevel is the risk classification when this entry matches.
	RiskLevel RiskLevel

	// Decision is the policy outcome when this entry matches.
	Decision Decision

	// ReasonVI is the Vietnamese explanation included in the Result.
	ReasonVI string
}
