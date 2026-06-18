package telegram

import (
	"fmt"
	"strings"
	"time"

	"vclaw/internal/agent"
)

const usdToVnd = 25700.0

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
	if sameDay(t, now) {
		return t.Format("15:04")
	}
	if sameDay(t, yesterday) {
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
			text := strings.TrimSpace(step.Text)
			if text == "" {
				text = "(không có nội dung)"
			}
			lines = append(lines, fmt.Sprintf("%s %s", prefix, escapeTelegramMarkdown(text)))
		}
	}

	lines = append(lines,
		"━━━━━━━━━━━━━━━━━━",
		fmt.Sprintf("Trạng thái: %s", statusTextForRun(run)),
	)
	if isFailedRunStatus(string(run.Status)) && strings.TrimSpace(run.ErrorRef) != "" {
		lines = append(lines, fmt.Sprintf("🔍 Ref: %s", escapeTelegramMarkdown(strings.ToUpper(strings.TrimSpace(run.ErrorRef)))))
	}

	return strings.Join(lines, "\n")
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

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
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
	case "failed", "blocked", "max_iterations":
		return "❌ Thất bại"
	default:
		return "⏳ Đang xử lý"
	}
}

func historyStatusIcon(status string) string {
	if isFailedRunStatus(status) {
		return "❌"
	}
	return "✅"
}

func isFailedRunStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "blocked", "max_iterations":
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
