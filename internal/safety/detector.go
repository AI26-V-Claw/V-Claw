// Package safety provides the V-Claw danger detection layer.
//
// Role in the pipeline:
//
//	Tool Request → [DangerDetector] → []DangerReport
//	                                        ↓
//	                               PolicyChecker (uses reports to assign RiskLevel)
//	                                        ↓
//	                               HITL Gate / Executor
//
// Unlike the PolicyChecker (which returns one first-match decision), the
// DangerDetector scans for ALL dangerous patterns in a command or code block
// and returns a slice of DangerReport — one per matched threat.
// This allows the audit log and HITL proposal to surface every risk, not just
// the worst one.
//
// Supported threat categories (S1-T7):
//   - file_deletion      : rm, rmdir, shred, del, rd, os.remove …
//   - system_shutdown    : shutdown, reboot, halt, poweroff …
//   - registry_access    : reg.exe, regedit, HKLM, HKCU (Windows registry)
//   - service_control    : systemctl, service, sc.exe, net start/stop
//   - credential_access  : .env, id_rsa, .pem, credentials.json …
package safety

import "strings"

// ─── Threat category ──────────────────────────────────────────────────────────

// ThreatCategory classifies what type of dangerous action was detected.
type ThreatCategory string

const (
	// ThreatFileDeletion — command deletes or permanently destroys files.
	ThreatFileDeletion ThreatCategory = "file_deletion"

	// ThreatDataOverwrite — command overwrites existing file content.
	ThreatDataOverwrite ThreatCategory = "data_overwrite"

	// ThreatSystemShutdown — command shuts down, reboots, or halts the system.
	ThreatSystemShutdown ThreatCategory = "system_shutdown"

	// ThreatRegistryAccess — command reads or writes the Windows registry.
	ThreatRegistryAccess ThreatCategory = "registry_access"

	// ThreatServiceControl — command starts, stops, or modifies system services.
	ThreatServiceControl ThreatCategory = "service_control"

	// ThreatCredentialAccess — command accesses credential files or secret stores.
	ThreatCredentialAccess ThreatCategory = "credential_access"

	// ThreatNetworkAccess — command attempts outbound network communication.
	ThreatNetworkAccess ThreatCategory = "network_access"

	// ThreatPrivilegeEscalation — command attempts to gain elevated privileges.
	ThreatPrivilegeEscalation ThreatCategory = "privilege_escalation"

	// ThreatCodeExecution — command executes dynamic code (eval, exec, subprocess).
	ThreatCodeExecution ThreatCategory = "code_execution"

	// ThreatProcessKill — command terminates other processes.
	ThreatProcessKill ThreatCategory = "process_kill"
)

// ─── Severity ─────────────────────────────────────────────────────────────────

// Severity indicates how severe the detected threat is.
type Severity string

const (
	// SeverityHigh — must be blocked or require strict HITL.
	SeverityHigh Severity = "high"

	// SeverityMedium — requires user approval (needs_approval).
	SeverityMedium Severity = "medium"

	// SeverityLow — worth flagging but may be allowed with a light warning.
	SeverityLow Severity = "low"
)

// ─── DangerReport ─────────────────────────────────────────────────────────────

// DangerReport describes a single dangerous pattern found in a command or code.
// Multiple reports may be returned for a single input if it contains several
// dangerous patterns.
type DangerReport struct {
	// Category is the type of threat detected.
	Category ThreatCategory

	// Severity indicates how dangerous this specific finding is.
	Severity Severity

	// MatchedPattern is the exact substring that triggered this report.
	MatchedPattern string

	// ExplanationVI is a Vietnamese explanation of the threat.
	// Used in HITL proposals and audit logs.
	ExplanationVI string

	// AffectedPaths contains any file or resource paths extracted from the
	// context around the matched pattern. May be empty.
	AffectedPaths []string
}

// IsDangerous returns true when the report represents an actual threat.
// A zero-value DangerReport is not dangerous.
func (r DangerReport) IsDangerous() bool {
	return r.Category != ""
}

// ─── Detector interface ───────────────────────────────────────────────────────

// Detector scans input text for dangerous patterns and returns all findings.
// It does not make a policy decision; that is the role of the PolicyChecker.
type Detector interface {
	// ScanShell scans a shell command string for dangerous patterns.
	ScanShell(command string) []DangerReport

	// ScanPython scans Python source code or a script path for dangerous patterns.
	ScanPython(codeOrPath string) []DangerReport
}

// ─── DangerRule ───────────────────────────────────────────────────────────────

// DangerRule is a single entry in a detection rule set.
// Pattern is matched against the lowercased input using substring search.
type DangerRule struct {
	Pattern       string
	Category      ThreatCategory
	Severity      Severity
	ExplanationVI string
}

// matches returns true if rule.Pattern is found anywhere in the lowercased text.
func (r DangerRule) matches(text string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(r.Pattern))
}
