package risk

// Level represents the risk level of an action.
// These values align with docs/03-contracts.md RiskLevel enum.
type Level string

const (
	SafeRead      Level = "safe_read"
	SafeCompute   Level = "safe_compute"
	SensitiveRead Level = "sensitive_read"
	ExternalWrite Level = "external_write"
	LocalWrite    Level = "local_write"
	CodeExecution Level = "code_execution"
	Destructive   Level = "destructive"
	Blocked       Level = "blocked"
)

// Decision represents the outcome of a risk assessment.
// These values align with docs/03-contracts.md RiskDecision.decision.
type Decision string

const (
	Allow            Decision = "allow"
	RequiresApproval Decision = "requires_approval"
	Block            Decision = "block"
)

// Assessment is the output of the risk classifier.
// It maps to the RiskDecision contract in docs/03-contracts.md.
type Assessment struct {
	ToolName         string   `json:"tool_name"`
	RiskLevel        Level    `json:"risk_level"`
	Decision         Decision `json:"decision"`
	RequiresApproval bool     `json:"requires_approval"`
	ReasonVi         string   `json:"reason_vi"` // Vietnamese explanation
}
