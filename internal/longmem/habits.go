package longmem

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"vclaw/internal/providers"
)

const repeatedHabitThreshold = 3

var (
	habitTimePattern      = regexp.MustCompile(`\b([01]?\d|2[0-3])\s*((?::|h|gio)\s*([0-5]\d)?)?\s*(sang|chieu|toi|am|pm)?\b`)
	habitStartTimePattern = regexp.MustCompile(`(?i)"start"\s*:\s*"([^"]+)"`)
)

type habitCandidate struct {
	key                string
	fact               string
	action             string
	target             string
	timeOfDay          string
	source             string
	status             string
	toolName           string
	sourceUserText     string
	completionUserText string
	eligible           bool
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
		if !ok || !candidate.eligible {
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
	if candidate, ok := habitCandidateFromApprovedContinuation(text); ok {
		return candidate, true
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

	candidate := newHabitCandidate(action, target, when)
	candidate.source = "user_message"
	candidate.status = "user_message"
	candidate.sourceUserText = original
	candidate.completionUserText = original
	candidate.eligible = habitIntentEligibleFromUserMessage(action)
	if !candidate.eligible {
		candidate.status = "proposed"
	}
	return candidate, true
}

func habitAction(text string) (string, bool) {
	switch {
	case habitContainsAny(text, "tom tat", "summary", "summarize"):
		return "summarize", true
	case habitContainsAny(text, "liet ke", "list"):
		return "inspect", true
	case habitContainsAny(text, "doc ", " doc", "read"):
		return "inspect", true
	case habitContainsAny(text, "kiem tra", "check", "xem "):
		return "inspect", true
	case habitContainsAny(text, "tao ", " tao", "create", "schedule", "dat lich", "len lich"):
		return "create", true
	case habitContainsAny(text, "cap nhat", "sua ", "update"):
		return "update", true
	case habitContainsAny(text, "gui ", "send"):
		return "send", true
	default:
		return "", false
	}
}

func habitTarget(text string) (string, bool) {
	switch {
	case habitContainsAny(text, "gmail", "email", "mail", "hop thu", "thu den"):
		return "email", true
	case habitContainsAny(text, "calendar", "lich", "su kien", "hop", "cuoc hop", "meeting"):
		return "calendar_event", true
	case habitContainsAny(text, "google chat", "tin nhan", "chat"):
		return "chat_message", true
	case habitContainsAny(text, "drive", "tai lieu", "file", "document", "docs"):
		return "drive_file", true
	default:
		return "", false
	}
}

func habitCandidateFromApprovedContinuation(text string) (habitCandidate, bool) {
	if !strings.Contains(text, "Completed tool:") || !strings.Contains(text, "Result:") {
		return habitCandidate{}, false
	}
	toolName := extractLineAfterLabel(text, "Completed tool:")
	if toolName == "" {
		return habitCandidate{}, false
	}
	action, target, ok := habitIntentFromToolName(toolName)
	if !ok {
		return habitCandidate{}, false
	}
	originalRequest := extractBlockBetween(text, "Original request:", "Completed tool:")
	resultText := extractTextAfterLabel(text, "Result:")
	when := habitTime(foldVietnamese(strings.ToLower(originalRequest)))
	if when == "" {
		when = habitTimeFromToolResult(resultText)
	}
	candidate := newHabitCandidate(action, target, when)
	candidate.source = "approved_tool"
	candidate.status = "approved_success"
	candidate.toolName = toolName
	candidate.sourceUserText = strings.Join(strings.Fields(originalRequest), " ")
	candidate.completionUserText = truncateHabitRawText(resultText)
	candidate.eligible = true
	return candidate, true
}

func habitIntentFromToolName(toolName string) (string, string, bool) {
	switch toolName {
	case "calendar.createEvent":
		return "create", "calendar_event", true
	case "calendar.updateEvent", "calendar.respondEvent":
		return "update", "calendar_event", true
	case "gmail.createDraft", "gmail.replyDraft", "gmail.forwardDraft":
		return "create", "email", true
	case "gmail.sendDraft":
		return "send", "email", true
	case "chat.sendMessage":
		return "send", "chat_message", true
	case "drive.createFile", "drive.uploadFile", "drive.createFolder":
		return "create", "drive_file", true
	case "drive.updateFileMetadata", "drive.moveFile", "docs.appendText", "docs.appendMarkdown", "docs.replaceText", "docs.insertText":
		return "update", "drive_file", true
	case "docs.createDocument":
		return "create", "document", true
	default:
		return "", "", false
	}
}

func newHabitCandidate(action, target, when string) habitCandidate {
	key := action + "|" + target + "|" + when
	fact := fmt.Sprintf("Người dùng thường muốn %s %s", habitActionDisplay(action), habitTargetDisplay(target))
	if when != "" {
		fact += " lúc " + when
	}
	fact += "."
	return habitCandidate{key: key, fact: fact, action: action, target: target, timeOfDay: when}
}

func habitIntentEligibleFromUserMessage(action string) bool {
	switch action {
	case "inspect", "check", "read", "list", "summarize":
		return true
	default:
		return false
	}
}

func habitActionDisplay(action string) string {
	switch action {
	case "inspect":
		return "xem/kiem tra"
	case "summarize":
		return "tóm tắt"
	case "list":
		return "liệt kê"
	case "read":
		return "đọc"
	case "check":
		return "kiểm tra"
	case "create":
		return "tạo"
	case "update":
		return "cập nhật"
	case "send":
		return "gửi"
	default:
		return action
	}
}

func habitTargetDisplay(target string) string {
	switch target {
	case "calendar_event":
		return "lịch/cuộc họp"
	case "chat_message":
		return "tin nhắn"
	case "drive_file":
		return "tài liệu"
	case "document":
		return "tài liệu"
	default:
		return target
	}
}

func habitTimeFromToolResult(text string) string {
	match := habitStartTimePattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339, match[1]); err == nil {
		return parsed.Format("15:04")
	}
	return ""
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

func extractLineAfterLabel(text, label string) string {
	rest := extractTextAfterLabel(text, label)
	if rest == "" {
		return ""
	}
	line := strings.SplitN(rest, "\n", 2)[0]
	return strings.TrimSpace(line)
}

func extractTextAfterLabel(text, label string) string {
	index := strings.Index(text, label)
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(text[index+len(label):])
}

func extractBlockBetween(text, startLabel, endLabel string) string {
	start := strings.Index(text, startLabel)
	if start < 0 {
		return ""
	}
	rest := text[start+len(startLabel):]
	end := strings.Index(rest, endLabel)
	if end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
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
