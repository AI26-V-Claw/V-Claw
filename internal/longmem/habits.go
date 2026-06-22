package longmem

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"vclaw/internal/providers"
)

const repeatedHabitThreshold = 5

var habitTimePattern = regexp.MustCompile(`\b([01]?\d|2[0-3])\s*((?::|h|gio)\s*([0-5]\d)?)?\s*(sang|chieu|toi|am|pm)?\b`)

type habitCandidate struct {
	key  string
	fact string
}

type HabitFact struct {
	CategorizedFact
	Count int
}

func extractRepeatedHabitFacts(transcript []providers.Message) []CategorizedFact {
	entries := extractRepeatedHabitEntries(transcript)
	out := make([]CategorizedFact, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.CategorizedFact)
	}
	return out
}

func extractRepeatedHabitEntries(transcript []providers.Message) []HabitFact {
	counts := map[string]int{}
	facts := map[string]string{}
	for _, message := range transcript {
		if message.Role != providers.MessageRoleUser {
			continue
		}
		candidate, ok := habitCandidateFromMessage(message.Content)
		if !ok {
			continue
		}
		counts[candidate.key]++
		facts[candidate.key] = candidate.fact
	}

	out := make([]HabitFact, 0, len(facts))
	for key, count := range counts {
		if count < repeatedHabitThreshold {
			continue
		}
		out = append(out, HabitFact{
			CategorizedFact: CategorizedFact{
				Category: userCategories[1],
				Fact:     facts[key],
			},
			Count: count,
		})
	}
	return out
}

func habitCandidateFromMessage(text string) (habitCandidate, bool) {
	original := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if original == "" || strings.HasPrefix(original, "/") {
		return habitCandidate{}, false
	}
	lower := foldVietnamese(strings.ToLower(original))
	if habitContainsAny(lower, "approve", "reject", "revise", "xac nhan", "dong y", "tu choi") {
		return habitCandidate{}, false
	}

	action, ok := habitAction(lower)
	if !ok {
		return habitCandidate{}, false
	}
	target, ok := habitTarget(lower)
	if !ok {
		return habitCandidate{}, false
	}
	when := habitTime(lower)

	key := action + "|" + target + "|" + when
	fact := fmt.Sprintf("Người dùng thường muốn %s %s", action, target)
	if when != "" {
		fact += " lúc " + when
	}
	fact += "."
	return habitCandidate{key: key, fact: fact}, true
}

func habitAction(text string) (string, bool) {
	switch {
	case habitContainsAny(text, "tom tat", "summary", "summarize"):
		return "tóm tắt", true
	case habitContainsAny(text, "liet ke", "list"):
		return "liệt kê", true
	case habitContainsAny(text, "doc ", " doc", "read"):
		return "đọc", true
	case habitContainsAny(text, "kiem tra", "check", "xem "):
		return "kiểm tra", true
	default:
		return "", false
	}
}

func habitTarget(text string) (string, bool) {
	switch {
	case habitContainsAny(text, "gmail", "email", "mail", "hop thu", "thu den"):
		return "email", true
	case habitContainsAny(text, "calendar", "lich", "su kien"):
		return "lịch", true
	case habitContainsAny(text, "google chat", "tin nhan", "chat"):
		return "tin nhắn", true
	case habitContainsAny(text, "drive", "tai lieu", "file"):
		return "tài liệu", true
	default:
		return "", false
	}
}

func habitTime(text string) string {
	matches := habitTimePattern.FindAllStringSubmatchIndex(text, -1)
	for _, match := range matches {
		raw := strings.TrimSpace(text[match[0]:match[1]])
		suffix := ""
		if match[8] >= 0 {
			suffix = strings.TrimSpace(text[match[8]:match[9]])
		}
		hasExplicitTimeCue := strings.Contains(raw, ":") || strings.Contains(raw, "h") || strings.Contains(raw, "gio") || suffix != ""
		if !hasExplicitTimeCue && !hasTimePrefix(text, match[0]) {
			continue
		}
		hour, err := strconv.Atoi(text[match[2]:match[3]])
		if err != nil {
			continue
		}
		minute := 0
		if match[6] >= 0 {
			minute, _ = strconv.Atoi(text[match[6]:match[7]])
		}
		switch suffix {
		case "pm", "chieu", "toi":
			if hour < 12 {
				hour += 12
			}
		case "am", "sang":
			if hour == 12 {
				hour = 0
			}
		}
		return fmt.Sprintf("%02d:%02d", hour, minute)
	}
	return ""
}

func hasTimePrefix(text string, start int) bool {
	prefix := strings.TrimSpace(text[:start])
	return strings.HasSuffix(prefix, "luc") || strings.HasSuffix(prefix, "vao") || strings.HasSuffix(prefix, "moi ngay")
}

func habitContainsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func foldVietnamese(text string) string {
	replacer := strings.NewReplacer(
		"à", "a", "á", "a", "ạ", "a", "ả", "a", "ã", "a", "â", "a", "ầ", "a", "ấ", "a", "ậ", "a", "ẩ", "a", "ẫ", "a", "ă", "a", "ằ", "a", "ắ", "a", "ặ", "a", "ẳ", "a", "ẵ", "a",
		"è", "e", "é", "e", "ẹ", "e", "ẻ", "e", "ẽ", "e", "ê", "e", "ề", "e", "ế", "e", "ệ", "e", "ể", "e", "ễ", "e",
		"ì", "i", "í", "i", "ị", "i", "ỉ", "i", "ĩ", "i",
		"ò", "o", "ó", "o", "ọ", "o", "ỏ", "o", "õ", "o", "ô", "o", "ồ", "o", "ố", "o", "ộ", "o", "ổ", "o", "ỗ", "o", "ơ", "o", "ờ", "o", "ớ", "o", "ợ", "o", "ở", "o", "ỡ", "o",
		"ù", "u", "ú", "u", "ụ", "u", "ủ", "u", "ũ", "u", "ư", "u", "ừ", "u", "ứ", "u", "ự", "u", "ử", "u", "ữ", "u",
		"ỳ", "y", "ý", "y", "ỵ", "y", "ỷ", "y", "ỹ", "y",
		"đ", "d",
	)
	return replacer.Replace(text)
}
