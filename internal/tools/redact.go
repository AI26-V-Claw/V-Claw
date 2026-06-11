package tools

import (
	"regexp"
	"strings"
)

// Sensitive patterns to mask in any tool output regardless of risk level.
// Each entry is compiled once at package init.
var sensitivePatterns = []*regexp.Regexp{
	// HTTP Authorization Bearer tokens
	regexp.MustCompile(`(?i)(Authorization:\s*Bearer\s+)[A-Za-z0-9\-._~+/]+=*`),
	// Generic "token", "secret", "password", "api_key", "apikey" JSON fields
	regexp.MustCompile(`(?i)("(?:token|secret|password|api_key|apikey|access_token|refresh_token|client_secret)"\s*:\s*")[^"]{4,}(")`),
	// AWS-style access keys (AKIA...)
	regexp.MustCompile(`(AKIA[0-9A-Z]{16})`),
	// Generic long hex/base64 that looks like a credential (≥32 chars, no spaces)
	// Only match when preceded by key-like context (=, :, ": ) to avoid false positives.
	regexp.MustCompile(`(?i)(?:secret|token|key|password)\s*[=:]\s*([A-Za-z0-9/+]{32,}={0,2})`),
}

const redactedPlaceholder = "[REDACTED]"

// RedactResult returns a copy of result with sensitive content masked.
//
// Two redaction strategies are applied:
//  1. Risk-based: if riskLevel is RiskLevelSensitiveRead, ContentForLLM is
//     replaced with a short summary. ContentForUser is never modified.
//  2. Pattern-based: regex patterns for credentials/tokens are masked in
//     ContentForLLM for ALL risk levels.
//
// RedactResult is a pure function; the original result is not modified.
func RedactResult(result ToolResult, riskLevel RiskLevel) ToolResult {
	out := result // shallow copy of value type

	// Pattern-based masking always applies to ContentForLLM.
	out.ContentForLLM = maskSensitivePatterns(result.ContentForLLM)
	if out.ContentForLLM != result.ContentForLLM {
		out.Metadata = cloneMetadata(result.Metadata)
		out.Metadata["_redacted"] = true
	}

	// Risk-based: for sensitive_read tools, suppress the LLM body and flag it.
	if riskLevel == RiskLevelSensitiveRead && result.Success {
		out.ContentForLLM = summarizeSensitiveContent(out.ContentForLLM)
		out.Metadata = cloneMetadata(result.Metadata)
		out.Metadata["_redacted"] = true
	}

	return out
}

// maskSensitivePatterns applies regex-based masking to s for all sensitive patterns.
func maskSensitivePatterns(s string) string {
	for _, re := range sensitivePatterns {
		s = re.ReplaceAllStringFunc(s, func(match string) string {
			// Preserve the leading key/label (first submatch group) and mask the value.
			subs := re.FindStringSubmatch(match)
			if len(subs) == 3 {
				// Pattern: (prefix)(value)(suffix) — mask middle group
				return subs[1] + redactedPlaceholder + subs[2]
			}
			if len(subs) == 2 {
				// Pattern: (prefix)(value) — mask last group
				return subs[1] + redactedPlaceholder
			}
			// Whole match has no groups — mask entirely
			return redactedPlaceholder
		})
	}
	return s
}

// summarizeSensitiveContent replaces the body of content with a redaction notice,
// preserving any leading header line (first line) for traceability.
func summarizeSensitiveContent(content string) string {
	lines := strings.SplitN(content, "\n", 2)
	header := strings.TrimSpace(lines[0])
	if header == "" {
		return "[sensitive content redacted from LLM context]"
	}
	return header + "\n[sensitive content redacted from LLM context]"
}

// cloneMetadata returns a shallow copy of m (or a new map if m is nil).
func cloneMetadata(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}
