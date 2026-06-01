package intent

import (
	"fmt"
	"strings"
)

// Validate checks an intent Result against the Tool Registry and
// returns a ClassificationOutput that enforces the safety rules:
//
//  1. DANGEROUS_ACTION with missing params → NeedsClarification = true
//  2. Low confidence → NeedsClarification = true
//  3. GREETING / safe READ_INFO → pass through
//
// This is the "gatekeeper" function described in the implementation plan.
func Validate(result *Result, cfg ConfidenceConfig) *ClassificationOutput {
	// Rule 1: Greetings always pass
	if result.Type == TypeGreeting {
		return &ClassificationOutput{
			Intent:             result,
			NeedsClarification: false,
		}
	}

	// Rule 2: Unknown intent → always clarify
	if result.Type == TypeUnknown {
		return &ClassificationOutput{
			Intent:             result,
			NeedsClarification: true,
			ClarificationMessage: "Tôi chưa hiểu rõ ý bạn. Bạn có thể diễn đạt lại được không?",
		}
	}

	// Rule 3: Confidence below absolute minimum → clarify
	if result.Confidence < cfg.AmbiguousLow {
		return &ClassificationOutput{
			Intent:             result,
			NeedsClarification: true,
			ClarificationMessage: "Tôi chưa hiểu rõ ý bạn. Bạn có thể diễn đạt lại được không?",
		}
	}

	// Rule 4: For DANGEROUS_ACTION / COMPOSITE, check missing params
	if result.Type == TypeDangerousAction || result.Type == TypeComposite {
		if len(result.MissingParams) > 0 {
			return &ClassificationOutput{
				Intent:             result,
				NeedsClarification: true,
				ClarificationMessage: buildClarificationMsg(result),
			}
		}

		// Rule 5: Even if params are present, confidence must meet threshold
		minConf := cfg.MinConfidenceFor(result.Type)
		if result.Confidence < minConf {
			return &ClassificationOutput{
				Intent:             result,
				NeedsClarification: true,
				ClarificationMessage: fmt.Sprintf(
					"Tôi nghĩ bạn muốn thực hiện một hành động thay đổi dữ liệu. Bạn có thể xác nhận lại không?\n\nĐộ tin cậy hiện tại: %.0f%% (yêu cầu tối thiểu: %.0f%%)",
					result.Confidence*100, minConf*100,
				),
			}
		}
	}

	// Rule 6: READ_INFO with confidence below threshold
	if result.Type == TypeReadInfo {
		minConf := cfg.MinConfidenceFor(result.Type)
		if result.Confidence < minConf {
			return &ClassificationOutput{
				Intent:             result,
				NeedsClarification: true,
				ClarificationMessage: "Bạn có thể nói rõ hơn bạn muốn tra cứu thông tin gì không?",
			}
		}
	}

	return &ClassificationOutput{
		Intent:             result,
		NeedsClarification: false,
	}
}

// buildClarificationMsg creates a user-friendly Vietnamese message
// asking for the specific missing parameters.
func buildClarificationMsg(r *Result) string {
	if len(r.ToolCalls) == 0 {
		return "Vui lòng cung cấp thêm thông tin để tôi thực hiện."
	}

	toolName := r.ToolCalls[0].Name

	// Map technical param names to Vietnamese
	paramNameMap := map[string]string{
		"path":    "đường dẫn file",
		"confirm": "xác nhận",
		"to":      "địa chỉ email người nhận",
		"subject": "tiêu đề email",
		"body":    "nội dung email",
		"command": "lệnh cần chạy",
		"content": "nội dung cần ghi",
		"code":    "mã nguồn cần chạy",
		"query":   "từ khóa tìm kiếm",
		"title":   "tiêu đề sự kiện",
		"start":   "thời gian bắt đầu",
	}

	var friendlyMissing []string
	for _, p := range r.MissingParams {
		if vi, ok := paramNameMap[p]; ok {
			friendlyMissing = append(friendlyMissing, vi)
		} else {
			friendlyMissing = append(friendlyMissing, p)
		}
	}

	return fmt.Sprintf(
		"Để thực hiện thao tác \"%s\", tôi cần bạn cung cấp thêm: %s.\n\nVui lòng cho tôi biết cụ thể.",
		toolName, strings.Join(friendlyMissing, ", "),
	)
}
