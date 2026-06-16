package longmem

import (
	"regexp"
	"strings"
)

var (
	// emailPattern matches "Name: email@x.com" associations.
	emailPattern = regexp.MustCompile(`(?i)([A-ZÀ-Ỹa-zà-ỹ][A-Za-zÀ-Ỹà-ỹ\s]{1,30}):\s*([a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,})`)

	// timezonePattern matches "timezone: Asia/Ho_Chi_Minh" or "múi giờ: GMT+7".
	timezonePattern = regexp.MustCompile(`(?i)(timezone|múi giờ|mui gio)[:\s]+([A-Za-z/_+\-0-9]{3,40})`)

	// namePattern matches "tên: Quang Ho" or "name: Quang Ho".
	namePattern = regexp.MustCompile(`(?i)(tên|name)[:\s]+([A-ZÀ-Ỹ][a-zA-ZÀ-Ỹà-ỹ\s]{1,40})`)

	// preferencePattern matches short preference/avoidance phrases.
	preferencePattern = regexp.MustCompile(`(?i)(thích|ưu tiên|prefer|không thích|tránh)[^\n.]{5,80}`)
)

// extractiveFallback extracts potentially useful facts from a summary using
// regex only — no LLM required. All results go to NOTES.md (not USER.md)
// because regex extraction is not reliable enough for the stable user profile.
// Returns nil if nothing extractable is found.
func extractiveFallback(summary string) []string {
	seen := map[string]bool{}
	var facts []string

	add := func(fact string) {
		fact = strings.TrimSpace(fact)
		if fact != "" && !seen[fact] {
			seen[fact] = true
			facts = append(facts, fact)
		}
	}

	for _, m := range emailPattern.FindAllStringSubmatch(summary, -1) {
		if len(m) >= 3 {
			add(strings.TrimSpace(m[1]) + ": " + strings.TrimSpace(m[2]))
		}
	}
	for _, m := range timezonePattern.FindAllStringSubmatch(summary, -1) {
		if len(m) >= 3 {
			add("Timezone: " + strings.TrimSpace(m[2]))
		}
	}
	for _, m := range namePattern.FindAllStringSubmatch(summary, -1) {
		if len(m) >= 3 {
			add("Tên: " + strings.TrimSpace(m[2]))
		}
	}
	for _, m := range preferencePattern.FindAllString(summary, -1) {
		add(strings.TrimSpace(m))
	}

	return facts
}
