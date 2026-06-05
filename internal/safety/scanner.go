package safety

import (
	"fmt"
	"strings"
)

// ─── MultiScanner ─────────────────────────────────────────────────────────────

// MultiScanner implements the Detector interface by scanning input against all
// rules in shellDangerRules and pythonDangerRules. Unlike the PolicyChecker
// (first-match), MultiScanner returns ALL matching rules so the HITL proposal
// and audit log can surface every detected threat.
//
// Usage:
//
//	scanner := safety.DefaultScanner
//	reports := scanner.ScanShell("rm -rf /workspace && curl http://evil.com")
//	// returns two DangerReports: file_deletion + network_access
type MultiScanner struct{}

// DefaultScanner is a ready-to-use MultiScanner.
var DefaultScanner Detector = &MultiScanner{}

// ScanShell implements Detector. It returns all dangerous patterns found in
// the shell command. An empty slice means no threats were detected.
func (s *MultiScanner) ScanShell(command string) []DangerReport {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	return scan(command, shellDangerRules)
}

// ScanPython implements Detector. It returns all dangerous patterns found in
// the Python source code or script path.
func (s *MultiScanner) ScanPython(codeOrPath string) []DangerReport {
	if strings.TrimSpace(codeOrPath) == "" {
		return nil
	}
	return scan(codeOrPath, pythonDangerRules)
}

// scan applies rules against text and collects all matching DangerReports.
// Duplicate patterns within the same category are deduplicated.
func scan(text string, rules []DangerRule) []DangerReport {
	seen := make(map[string]bool) // key: category+pattern to avoid duplicates
	var reports []DangerReport

	for _, rule := range rules {
		if !rule.matches(text) {
			continue
		}
		key := string(rule.Category) + "|" + strings.ToLower(rule.Pattern)
		if seen[key] {
			continue
		}
		seen[key] = true
		reports = append(reports, DangerReport{
			Category:       rule.Category,
			Severity:       rule.Severity,
			MatchedPattern: rule.Pattern,
			ExplanationVI:  rule.ExplanationVI,
		})
	}
	return reports
}

// ─── Summary helpers ──────────────────────────────────────────────────────────

// HighestSeverity returns the most severe Severity found in reports.
// Returns "" when reports is empty.
func HighestSeverity(reports []DangerReport) Severity {
	for _, r := range reports {
		if r.Severity == SeverityHigh {
			return SeverityHigh
		}
	}
	for _, r := range reports {
		if r.Severity == SeverityMedium {
			return SeverityMedium
		}
	}
	if len(reports) > 0 {
		return SeverityLow
	}
	return ""
}

// Categories returns a deduplicated list of ThreatCategory values found in
// the reports.
func Categories(reports []DangerReport) []ThreatCategory {
	seen := make(map[ThreatCategory]bool)
	var cats []ThreatCategory
	for _, r := range reports {
		if !seen[r.Category] {
			seen[r.Category] = true
			cats = append(cats, r.Category)
		}
	}
	return cats
}

// HasCategory returns true if any report in the slice belongs to cat.
func HasCategory(reports []DangerReport, cat ThreatCategory) bool {
	for _, r := range reports {
		if r.Category == cat {
			return true
		}
	}
	return false
}

// SummariseVI returns a concise Vietnamese summary of all detected threats,
// suitable for the first line of a HITL proposal.
//
// Example output:
//
//	"Phát hiện 2 mối nguy: xóa file (medium), truy cập mạng (high)."
func SummariseVI(reports []DangerReport) string {
	if len(reports) == 0 {
		return "Không phát hiện mối nguy nào."
	}

	// Collect unique (category, severity) pairs.
	type entry struct {
		cat ThreatCategory
		sev Severity
	}
	seen := make(map[ThreatCategory]bool)
	var pairs []entry
	for _, r := range reports {
		if !seen[r.Category] {
			seen[r.Category] = true
			pairs = append(pairs, entry{r.Category, r.Severity})
		}
	}

	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, fmt.Sprintf("%s (%s)", categoryVI(p.cat), p.sev))
	}

	return fmt.Sprintf("Phát hiện %d mối nguy: %s.", len(pairs), strings.Join(parts, ", "))
}

// categoryVI maps ThreatCategory to a short Vietnamese label.
func categoryVI(c ThreatCategory) string {
	switch c {
	case ThreatFileDeletion:
		return "xóa file"
	case ThreatDataOverwrite:
		return "ghi đè dữ liệu"
	case ThreatSystemShutdown:
		return "tắt/khởi động lại hệ thống"
	case ThreatRegistryAccess:
		return "truy cập registry"
	case ThreatServiceControl:
		return "điều khiển service"
	case ThreatCredentialAccess:
		return "truy cập credential"
	case ThreatNetworkAccess:
		return "truy cập mạng"
	case ThreatPrivilegeEscalation:
		return "leo thang đặc quyền"
	case ThreatCodeExecution:
		return "thực thi code động"
	case ThreatProcessKill:
		return "kết thúc tiến trình"
	default:
		return string(c)
	}
}
