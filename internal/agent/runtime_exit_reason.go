package agent

import (
	"encoding/json"
	"fmt"
	"strconv"

	"vclaw/internal/contracts"
)

// agentStatusForRunStatus maps an internal RuntimeRunStatus to the
// contract-level AgentStatus. This keeps the mapping in one place so
// handleContextError and future callers stay consistent.
func agentStatusForRunStatus(status RuntimeRunStatus) contracts.AgentStatus {
	switch status {
	case RuntimeRunStatusCompleted:
		return contracts.AgentStatusCompleted
	case RuntimeRunStatusCancelled:
		return contracts.AgentStatusCancelled
	case RuntimeRunStatusIterationBudget:
		return contracts.AgentStatusIterationBudgetExhausted
	case RuntimeRunStatusBlocked:
		return contracts.AgentStatusBlocked
	default:
		return contracts.AgentStatusFailed
	}
}

// ExitReason returns a short, user-facing explanation for a non-completed
// AgentResponse. The text is suitable for CLI output or Telegram messages.
func ExitReason(resp contracts.AgentResponse) string {
	switch resp.Status {
	case contracts.AgentStatusCompleted:
		return ""
	case contracts.AgentStatusCancelled:
		return "Đã hủy: run bị dừng bởi người dùng."
	case contracts.AgentStatusIterationBudgetExhausted:
		return iterationBudgetExitReason(resp)
	case contracts.AgentStatusBlocked:
		if resp.FailureReason != "" {
			return fmt.Sprintf("Bị chặn: %s", resp.FailureReason)
		}
		return "Run bị chặn và không thể tiếp tục."
	case contracts.AgentStatusFailed:
		return failedExitReason(resp)
	default:
		return ""
	}
}

func iterationBudgetExitReason(resp contracts.AgentResponse) string {
	if used, ok := responseInt(resp.Data["iteration_used"]); ok {
		if limit, ok2 := responseInt(resp.Data["iteration_limit"]); ok2 {
			return fmt.Sprintf("Đã dùng hết ngân sách xử lý (%d/%d bước). Hãy thu hẹp yêu cầu hoặc tiếp tục trong tin nhắn mới.", used, limit)
		}
	}
	return "Đã dùng hết ngân sách xử lý. Hãy thu hẹp yêu cầu hoặc tiếp tục trong tin nhắn mới."
}

func responseInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		parsed, err := strconv.Atoi(string(typed))
		return parsed, err == nil
	case string:
		parsed, err := strconv.Atoi(typed)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func failedExitReason(resp contracts.AgentResponse) string {
	if resp.Error == nil {
		if resp.FailureReason != "" {
			return fmt.Sprintf("Lỗi: %s", resp.FailureReason)
		}
		return "Không thể hoàn tất yêu cầu."
	}
	switch resp.Error.Code {
	case contracts.ErrorProviderTimeout:
		return "Lỗi: provider hết thời gian chờ (timeout)."
	case contracts.ErrorCancelled:
		return "Đã hủy: run bị dừng bởi người dùng."
	default:
		if resp.Error.Message != "" {
			return fmt.Sprintf("Lỗi: %s", resp.Error.Message)
		}
		return fmt.Sprintf("Lỗi: %s", resp.Error.Code)
	}
}
