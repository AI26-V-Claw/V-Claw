package telegram

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"vclaw/internal/agent"
)

const usdToVnd = 25700.0
const maxTelegramStatusRunes = 3200
const maxStatusStepRunes = 220
const maxStatusListItems = 8

var statusEmailItemPattern = regexp.MustCompile(`(?m)^\d+\.\s+Từ:`)

func categoryIcon(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "gmail":
		return "📧"
	case "calendar":
		return "📅"
	case "drive":
		return "📁"
	case "docs":
		return "📄"
	case "search":
		return "🔍"
	default:
		return "💬"
	}
}

func smartTime(t, now time.Time) string {
	loc := hoChiMinhLocation()
	t = t.In(loc)
	now = now.In(loc)

	yesterday := now.AddDate(0, 0, -1)
	if sameLocalDay(t, now) {
		return t.Format("15:04")
	}
	if sameLocalDay(t, yesterday) {
		return "Hôm qua " + t.Format("15:04")
	}
	return t.Format("02/01 15:04")
}

func truncateRune(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= n {
		return string(runes)
	}
	return string(runes[:n-1]) + "…"
}

func FormatStatus(run *agent.RunState) string {
	if run == nil || strings.TrimSpace(run.RunID) == "" {
		return "Chưa có lệnh nào được thực thi."
	}

	now := time.Now()
	lines := []string{
		"📊 *Trạng thái lệnh gần nhất*",
		"────────────────────",
		fmt.Sprintf("🗓 Thời gian: %s", formatDateTime(run.CreatedAt)),
		fmt.Sprintf("⚡ Thời gian xử lý: %.1f giây", runDurationSeconds(run, now)),
		formatCostLine(run.CostUSD),
		"",
		"📝 *Yêu cầu*",
		escapeTelegramMarkdown(textOrFallback(run.OriginalGoal, "(không có dữ liệu)")),
		"",
		"📌 *Kết quả*",
	}

	if len(run.Steps) == 0 {
		lines = append(lines, "_(chưa có dữ liệu bước)_")
	} else {
		for _, step := range run.Steps {
			prefix := "✅"
			if !step.OK {
				prefix = "❌"
			}
			texts := summarizeStatusStepText(step.Text)
			if len(texts) == 0 {
				texts = []string{"(không có nội dung)"}
			}
				for i, text := range texts {
					text = truncateRune(text, maxStatusStepRunes)
					if i == 0 {
						lines = append(lines, fmt.Sprintf("%s %s", prefix, escapeTelegramMarkdown(text)))
						continue
					}
					lines = append(lines, "   "+escapeTelegramMarkdown(text))
			}
		}
	}

	lines = append(lines,
		"━━━━━━━━━━━━━━━━━━",
		statusLineForRun(run),
	)
	if isFailedRunStatus(string(run.Status)) && strings.TrimSpace(run.ErrorRef) != "" {
		lines = append(lines, fmt.Sprintf("🔍 Ref: %s", escapeTelegramMarkdown(strings.ToUpper(strings.TrimSpace(run.ErrorRef)))))
	}

	return trimTelegramStatusText(strings.Join(lines, "\n"))
}

func FormatHistory(runs []*agent.RunState, now time.Time) string {
	if len(runs) == 0 {
		return "Chưa có lịch sử nào."
	}

	lines := []string{
		"📋 *Lịch sử gần nhất*",
		"─────────────────────",
	}
	for i, run := range runs {
		if run == nil || strings.TrimSpace(run.RunID) == "" {
			continue
		}
		summary := truncateRune(textOrFallback(run.ShortLabel, run.OriginalGoal), 20)
		row := fmt.Sprintf("%s  %s  %s  %s  %s",
			padRightRunes(fmt.Sprintf("%d.", i+1), 3),
			padRightRunes(smartTime(run.CreatedAt, now), 15),
			categoryIcon(run.Category),
			padRightRunes(escapeTelegramMarkdown(summary), 21),
			historyStatusIcon(string(run.Status)),
		)
		lines = append(lines, strings.TrimRight(row, " "))
	}
	lines = append(lines, "", "_Gõ_ `/history <số>` _để xem chi tiết_")
	return strings.Join(lines, "\n")
}

func escapeTelegramMarkdown(text string) string {
	if text == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"`", "\\`",
	)
	return replacer.Replace(text)
}

func formatVnd(costUSD float64) string {
	return fmt.Sprintf("%d", int64(costUSD*usdToVnd+0.5))
}

func formatCostLine(costUSD float64) string {
	if costUSD <= 0 {
		return "💰 Chi phí: chưa ghi nhận"
	}
	return fmt.Sprintf("💰 Chi phí: $%.4f (~%s VNĐ)", costUSD, formatVnd(costUSD))
}

func formatDateTime(t time.Time) string {
	return t.In(hoChiMinhLocation()).Format("02/01/2006 15:04:05")
}

func textOrFallback(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(strings.ReplaceAll(value, "\n", " "))
}

func summarizeStatusStepText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if lines := summarizeFriendlyEmailListText(text); len(lines) > 0 {
		return lines
	}
	if lines := summarizeStructuredStatusText(text); len(lines) > 0 {
		return lines
	}
	return []string{text}
}

func summarizeFriendlyEmailListText(text string) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || !strings.Contains(trimmed, "Đây là danh sách email") {
		return nil
	}
	itemCount := len(statusEmailItemPattern.FindAllString(trimmed, -1))
	if itemCount == 0 {
		return nil
	}

	lines := strings.Split(trimmed, "\n")
	notable := make([]string, 0, 3)
	seen := map[string]bool{}
	attachmentCount := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "• Tệp đính kèm: Có") {
			attachmentCount++
			continue
		}
		if !strings.Contains(line, "Từ:") {
			continue
		}
		sender := statusSenderNameFromLine(line)
		if sender == "" || sender == "Bạn" || seen[sender] {
			continue
		}
		seen[sender] = true
		notable = append(notable, sender)
		if len(notable) == 3 {
			break
		}
	}

	summary := []string{fmt.Sprintf("Tìm thấy %d email phù hợp.", itemCount)}
	if len(notable) > 0 {
		summary = append(summary, "Người gửi đáng chú ý: "+strings.Join(notable, ", ")+".")
	}
	if attachmentCount > 0 {
		summary = append(summary, fmt.Sprintf("Có %d email có tệp đính kèm.", attachmentCount))
	} else {
		summary = append(summary, "Không thấy email nào có tệp đính kèm.")
	}
	return []string{strings.Join(summary, " ")}
}

func statusSenderNameFromLine(line string) string {
	start := strings.Index(line, "Từ:")
	if start < 0 {
		return ""
	}
	text := strings.TrimSpace(line[start+len("Từ:"):])
	if text == "" {
		return ""
	}
	if idx := strings.Index(text, " gửi tới "); idx >= 0 {
		text = text[:idx]
	}
	if idx := strings.Index(text, "("); idx >= 0 {
		text = text[:idx]
	}
	return strings.TrimSpace(text)
}

func summarizeStructuredStatusText(text string) []string {
	trimmed := strings.TrimSpace(text)
	start := strings.IndexAny(trimmed, "[{")
	if start < 0 {
		return nil
	}
	prefix := strings.TrimSpace(trimmed[:start])
	payloadText := strings.TrimSpace(trimmed[start:])
	var payload any
	if err := json.Unmarshal([]byte(payloadText), &payload); err != nil {
		return nil
	}
	lines := summarizeStructuredValue(payload)
	if len(lines) == 0 {
		return nil
	}
	if prefix != "" {
		lines[0] = prefix + ": " + lines[0]
	}
	return lines
}

func summarizeStructuredValue(value any) []string {
	switch typed := value.(type) {
	case []any:
		return summarizeStructuredList(typed)
	case map[string]any:
		return summarizeStructuredMap(typed)
	default:
		return nil
	}
}

func summarizeStructuredList(items []any) []string {
	if len(items) == 0 {
		return []string{"Không có dữ liệu."}
	}
	limit := len(items)
	if limit > maxStatusListItems {
		limit = maxStatusListItems
	}
	lines := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		item := items[i]
		summary := summarizeSingleRecord(item)
		if summary == "" {
			summary = fmt.Sprintf("Mục %d", i+1)
		}
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, summary))
	}
	if len(items) > limit {
		lines = append(lines, fmt.Sprintf("...và %d mục khác", len(items)-limit))
	}
	return lines
}

func summarizeStructuredMap(payload map[string]any) []string {
	for _, key := range []string{"Event", "Message", "Draft", "File"} {
		if nested, ok := payload[key].(map[string]any); ok {
			if summary := summarizeSingleRecord(nested); summary != "" {
				return []string{summary}
			}
		}
	}
	for _, key := range []string{"Events", "Messages", "Drafts", "Files"} {
		if list, ok := payload[key].([]any); ok {
			return summarizeStructuredList(list)
		}
	}
	if summary := summarizeSingleRecord(payload); summary != "" {
		return []string{summary}
	}
	return nil
}

func summarizeSingleRecord(value any) string {
	record, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, 4)
	if title := firstRecordValue(record, "title", "Title", "summary", "Subject", "subject", "Name", "name", "Text", "text"); title != "" {
		parts = append(parts, title)
	}
	if timeText := summarizeRecordTime(record); timeText != "" {
		parts = append(parts, "Thời gian: "+timeText)
	}
	if audience := summarizeRecordAudience(record); audience != "" {
		parts = append(parts, audience)
	}
	if link := firstRecordValue(record, "eventLink", "EventLink", "WebViewLink", "meetLink"); link != "" {
		parts = append(parts, "Link: "+link)
	}
	if location := firstRecordValue(record, "location", "Location"); location != "" {
		parts = append(parts, "Địa điểm: "+location)
	}
	if len(parts) > 0 {
		return strings.Join(parts, " | ")
	}
	return summarizeFallbackRecord(record)
}

func summarizeRecordTime(record map[string]any) string {
	start := firstRecordValue(record, "start", "StartTime", "startTime", "LocalDateTime", "ModifiedTime", "Date")
	end := firstRecordValue(record, "end", "EndTime", "endTime")
	switch {
	case start != "" && end != "":
		return start + " - " + end
	case start != "":
		return start
	case end != "":
		return end
	default:
		return ""
	}
}

func summarizeRecordAudience(record map[string]any) string {
	to := firstRecordValue(record, "To", "to")
	from := firstRecordValue(record, "From", "from")
	switch {
	case from != "" && to != "":
		return "Từ: " + from + " | Đến: " + to
	case to != "":
		return "Người nhận: " + to
	case from != "":
		return "Người gửi: " + from
	default:
		return ""
	}
}

func summarizeFallbackRecord(record map[string]any) string {
	keys := make([]string, 0, len(record))
	for key := range record {
		if shouldSkipStatusField(key, record[key]) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, 3)
	for _, key := range keys {
		value := firstRecordValue(record, key)
		if value == "" {
			continue
		}
		parts = append(parts, humanizeStatusField(key)+": "+value)
		if len(parts) == 3 {
			break
		}
	}
	return strings.Join(parts, " | ")
}

func firstRecordValue(record map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := record[key]
		if !ok {
			continue
		}
		switch value := raw.(type) {
		case string:
			value = strings.TrimSpace(value)
			if value != "" {
				return value
			}
		case float64:
			return fmt.Sprintf("%.0f", value)
		case bool:
			if value {
				return "Có"
			}
		}
	}
	return ""
}

func shouldSkipStatusField(key string, value any) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	switch lower {
	case "id", "threadid", "messageid", "historyid", "isrecurring":
		return true
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) == ""
	case bool:
		return !typed
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return value == nil
	}
}

func humanizeStatusField(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "title", "summary", "subject", "name":
		return "Tên"
	case "webviewlink", "eventlink", "meetlink":
		return "Link"
	case "start", "starttime", "localdatetime", "date", "modifiedtime":
		return "Thời gian"
	case "to":
		return "Người nhận"
	case "from":
		return "Người gửi"
	case "location":
		return "Địa điểm"
	default:
		return key
	}
}

func sameLocalDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func hoChiMinhLocation() *time.Location {
	if loc, err := time.LoadLocation("Asia/Ho_Chi_Minh"); err == nil {
		return loc
	}
	return time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60)
}

func padRightRunes(text string, width int) string {
	runes := []rune(text)
	if len(runes) >= width {
		return text
	}
	return text + strings.Repeat(" ", width-len(runes))
}

func statusTextForRun(run *agent.RunState) string {
	switch strings.ToLower(strings.TrimSpace(string(run.Status))) {
	case "completed":
		return "✅ Hoàn thành"
	case "failed", "blocked", "iteration_budget":
		return "❌ Thất bại"
	case "cancelled":
		return "🚫 Đã hủy"
	case "running":
		return "⏳ Đang xử lý"
	case "waiting_approval":
		return "⏳ Chờ xác nhận"
	case "waiting_clarification":
		return "⏳ Chờ thông tin"
	default:
		return "⏳ Đang xử lý"
	}
}

func statusLineForRun(run *agent.RunState) string {
	return "Trạng thái: " + statusTextForRun(run)
}

func historyStatusIcon(status string) string {
	if isFailedRunStatus(status) {
		return "❌"
	}
	return "✅"
}

func isFailedRunStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "blocked", "iteration_budget":
		return true
	default:
		return false
	}
}

func runDurationSeconds(run *agent.RunState, now time.Time) float64 {
	end := now
	if run.CompletedAt != nil {
		end = *run.CompletedAt
	}
	elapsed := end.Sub(run.CreatedAt)
	if elapsed < 0 {
		return 0
	}
	return elapsed.Seconds()
}

func trimTelegramStatusText(text string) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= maxTelegramStatusRunes {
		return text
	}
	suffix := "\n\n...[đã rút gọn]"
	suffixRunes := []rune(suffix)
	if len(suffixRunes) >= maxTelegramStatusRunes {
		return string(suffixRunes[:maxTelegramStatusRunes])
	}
	return strings.TrimSpace(string(runes[:maxTelegramStatusRunes-len(suffixRunes)])) + suffix
}
