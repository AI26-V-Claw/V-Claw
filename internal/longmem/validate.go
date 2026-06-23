package longmem

import (
	"fmt"
	"regexp"
	"strings"
)

// secretPatterns matches content that looks like a credential, token, or secret.
// This enforces the data contract in docs/03-contracts.md §9.1 ("Không ghi
// credentials, token, password, hoặc secret bất kỳ loại nào") at the write
// boundary, independent of any approval. Approval gates intent; this gate
// protects the long-term memory store from being poisoned with secrets that the
// memory loader would later inject back into the prompt.
var secretPatterns = []*regexp.Regexp{
	// key=value / key: value style secret assignments, including names like
	// OPENAI_API_KEY or GOOGLE_PRIVATE_KEY.
	regexp.MustCompile(`(?i)\b[a-z0-9_]*(api[_-]?key|access[_-]?token|refresh[_-]?token|secret|password|passwd|client[_-]?secret|private[_-]?key)[a-z0-9_]*\s*[:=]`),
	regexp.MustCompile(`(?i)\bbearer\b\s*[:=]`),
	// Provider-style secret prefixes (OpenAI sk-..., Google AIza..., GitHub ghp_...)
	regexp.MustCompile(`\bsk-[A-Za-z0-9]{16,}\b`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{20,}\b`),
	regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}\b`),
	// Authorization: Bearer <token>
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._\-]{16,}\b`),
	// PEM private key blocks
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
}

// ValidateMemoryContent rejects content that appears to contain a credential,
// token, password, or other secret. It returns an error describing the
// violation so callers can surface it to the user. Empty/whitespace content is
// treated as a separate concern and passes this check.
func ValidateMemoryContent(content string) error {
	c := strings.TrimSpace(content)
	if c == "" {
		return nil
	}
	for _, re := range secretPatterns {
		if re.MatchString(c) {
			return fmt.Errorf("nội dung có dấu hiệu chứa credential/token/secret; không được ghi vào bộ nhớ dài hạn")
		}
	}
	return nil
}

