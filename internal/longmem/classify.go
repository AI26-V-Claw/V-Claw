package longmem

import (
	"strings"

	"vclaw/internal/sessions"
)

const notesMaxTokens = 1500

// ClassifyResult holds facts extracted from a session summary by the LLM.
type ClassifyResult struct {
	UserFacts  []string // stable, long-term profile facts → USER.md
	NotesFacts []string // short-term / current context → NOTES.md
}

func classifySystemPrompt() string {
	return strings.TrimSpace(`Bạn là bộ phân loại bộ nhớ dài hạn cho AI agent.
Nhiệm vụ: đọc tóm tắt phiên làm việc và trích xuất các sự kiện đáng nhớ lâu dài.

PHÂN LOẠI:
USER_FACTS — thông tin ổn định, đúng mãi mãi về người dùng:
  tên, email, timezone, contacts thường xuyên (tên+email+vai trò), quy tắc agent phải luôn tuân theo.
NOTES_FACTS — thông tin hiện tại hoặc ngắn hạn, có thể lỗi thời sau vài tuần:
  project đang làm, contacts vừa gặp lần đầu, context session, ghi chú công việc tạm thời.

KHÔNG trích xuất:
- Credentials, password, token, API key bất kỳ loại nào.
- Nội dung cụ thể của email, lịch, tin nhắn (chỉ trích tên/email người liên quan nếu cần).
- Task đã hoàn thành không cần nhớ.
- Thông tin không rõ ràng hoặc suy đoán.

OUTPUT FORMAT — trả lời chính xác theo mẫu sau, không thêm gì khác:
## USER_FACTS
- <fact 1>

## NOTES_FACTS
- <fact 1>

Nếu không có sự kiện cho một loại, để section đó trống (giữ heading).
Trả lời bằng tiếng Việt.`)
}

func classifyUserPrompt(summary string) string {
	return "Tóm tắt phiên:\n" + summary
}

func parseClassifyResponse(text string) ClassifyResult {
	var result ClassifyResult
	text = strings.TrimSpace(text)
	userIdx := strings.Index(text, "## USER_FACTS")
	notesIdx := strings.Index(text, "## NOTES_FACTS")
	if userIdx < 0 && notesIdx < 0 {
		return result
	}
	var userSection, notesSection string
	if userIdx >= 0 && notesIdx > userIdx {
		userSection = text[userIdx+len("## USER_FACTS") : notesIdx]
		notesSection = text[notesIdx+len("## NOTES_FACTS"):]
	} else if notesIdx >= 0 && (userIdx < 0 || userIdx > notesIdx) {
		notesSection = text[notesIdx+len("## NOTES_FACTS"):]
		if userIdx >= 0 {
			userSection = text[userIdx+len("## USER_FACTS"):]
		}
	} else if userIdx >= 0 {
		userSection = text[userIdx+len("## USER_FACTS"):]
	}
	result.UserFacts = parseBullets(userSection)
	result.NotesFacts = parseBullets(notesSection)
	return result
}

func parseBullets(section string) []string {
	var out []string
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			fact := strings.TrimSpace(line[2:])
			if fact != "" {
				out = append(out, fact)
			}
		}
	}
	return out
}

func mergeUserFacts(existing string, newFacts []string) string {
	if strings.TrimSpace(existing) == "" {
		existing = userMDSkeleton()
	}
	for _, fact := range newFacts {
		if !strings.Contains(existing, fact) {
			existing = strings.TrimRight(existing, "\n") + "\n- " + fact + "\n"
		}
	}
	return existing
}

func userMDSkeleton() string {
	return strings.TrimSpace(`# Thông tin người dùng

## Thông tin cơ bản

## Sở thích làm việc

## Người quen thuộc

## Quy tắc làm việc
`) + "\n"
}

func appendNotesFacts(existing string, newFacts []string) string {
	if strings.TrimSpace(existing) == "" {
		existing = notesMDSkeleton()
	}
	for _, fact := range newFacts {
		if !strings.Contains(existing, fact) {
			existing = strings.TrimRight(existing, "\n") + "\n- " + fact + "\n"
		}
	}
	if sessions.EstimateTokens(existing) > notesMaxTokens {
		existing = trimNotesContent(existing, notesMaxTokens)
	}
	return existing
}

func notesMDSkeleton() string {
	return "# Ghi chú gần đây\n"
}

// trimNotesContent removes the oldest non-heading lines from the top of content
// until EstimateTokens(content) ≤ maxTokens. Heading lines (starting with #)
// are never removed.
func trimNotesContent(content string, maxTokens int) string {
	if sessions.EstimateTokens(content) <= maxTokens {
		return content
	}
	lines := strings.Split(content, "\n")
	for sessions.EstimateTokens(strings.Join(lines, "\n")) > maxTokens {
		removed := false
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				lines = append(lines[:i], lines[i+1:]...)
				removed = true
				break
			}
		}
		if !removed {
			break // only headings (or empty lines) remain; stop
		}
	}
	return strings.Join(lines, "\n")
}
