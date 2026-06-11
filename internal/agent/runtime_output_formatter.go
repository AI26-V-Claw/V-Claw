package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"vclaw/internal/contracts"
)

func renderAssistantMessage(message string, results []contracts.ToolResult) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	if looksLikeMachinePayload(message) {
		for i := len(results) - 1; i >= 0; i-- {
			if !results[i].Success {
				continue
			}
			if rendered := renderMachinePayload(results[i].ToolName, message); rendered != "" {
				return rendered
			}
		}
		if rendered := renderMachinePayload("", message); rendered != "" {
			return rendered
		}
		if !shouldFallbackToToolResultForMachineLikeMessage(message) {
			message = sanitizeDeliveryClaimsForResults(message, results)
			return formatOutboundText(message)
		}
		for i := len(results) - 1; i >= 0; i-- {
			if rendered := renderToolResultForUser(results[i]); rendered != "" {
				return rendered
			}
		}
	}
	message = sanitizeDeliveryClaimsForResults(message, results)
	return formatOutboundText(message)
}

func sanitizeDeliveryClaimsForResults(message string, results []contracts.ToolResult) string {
	if !hasSuccessfulToolResult(results, "gmail.sendDraft") {
		return message
	}
	replacer := strings.NewReplacer(
		"\u0111\u00e3 \u0111\u01b0\u1ee3c g\u1eedi th\u00e0nh c\u00f4ng", "\u0111\u00e3 \u0111\u01b0\u1ee3c chuy\u1ec3n cho Gmail \u0111\u1ec3 g\u1eedi",
		"\u0110\u00e3 \u0111\u01b0\u1ee3c g\u1eedi th\u00e0nh c\u00f4ng", "\u0110\u00e3 \u0111\u01b0\u1ee3c chuy\u1ec3n cho Gmail \u0111\u1ec3 g\u1eedi",
		"\u0111\u00e3 g\u1eedi th\u00e0nh c\u00f4ng", "\u0111\u00e3 chuy\u1ec3n email cho Gmail \u0111\u1ec3 g\u1eedi",
		"\u0110\u00e3 g\u1eedi th\u00e0nh c\u00f4ng", "\u0110\u00e3 chuy\u1ec3n email cho Gmail \u0111\u1ec3 g\u1eedi",
		"g\u1eedi th\u00e0nh c\u00f4ng", "chuy\u1ec3n cho Gmail \u0111\u1ec3 g\u1eedi",
	)
	return replacer.Replace(message)
}

func hasSuccessfulToolResult(results []contracts.ToolResult, toolName string) bool {
	for _, result := range results {
		if result.Success && strings.TrimSpace(result.ToolName) == toolName {
			return true
		}
	}
	return false
}

func renderToolResultForUser(result contracts.ToolResult) string {
	if !result.Success {
		return ""
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return ""
	}
	content, ok := data["contentForUser"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return ""
	}
	return renderToolFallback(result.ToolName, content)
}

func looksLikeMachinePayload(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return true
	}
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "event created:") ||
		strings.HasPrefix(lower, "event updated:") ||
		strings.Contains(lower, ": {") ||
		strings.Contains(lower, ": [")
}

func shouldFallbackToToolResultForMachineLikeMessage(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return true
	}
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "event created:") ||
		strings.HasPrefix(lower, "event updated:") ||
		strings.Contains(lower, ": {")
}

func renderMachinePayload(toolName string, content string) string {
	prefix, jsonText := splitMachinePayload(content)
	if jsonText == "" {
		return ""
	}
	var value any
	if err := json.Unmarshal([]byte(jsonText), &value); err != nil {
		return ""
	}
	lines := renderPayloadValue(toolName, prefix, value)
	if len(lines) == 0 {
		return ""
	}
	return formatOutboundText(strings.Join(lines, "\n"))
}

func splitMachinePayload(content string) (string, string) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", ""
	}
	firstObject := strings.Index(trimmed, "{")
	firstArray := strings.Index(trimmed, "[")
	index := firstJSONIndex(firstObject, firstArray)
	if index < 0 {
		return "", ""
	}
	prefix := ""
	if index > 0 {
		prefix = strings.TrimSpace(strings.TrimSuffix(trimmed[:index], ":"))
	}
	return prefix, strings.TrimSpace(trimmed[index:])
}

func firstJSONIndex(objectIndex int, arrayIndex int) int {
	if objectIndex < 0 {
		return arrayIndex
	}
	if arrayIndex < 0 {
		return objectIndex
	}
	if objectIndex < arrayIndex {
		return objectIndex
	}
	return arrayIndex
}

func renderPayloadValue(toolName string, prefix string, value any) []string {
	switch typed := value.(type) {
	case map[string]any:
		return renderPayloadMap(toolName, prefix, typed)
	case []any:
		return renderPayloadList(toolName, payloadTitle(toolName, prefix), typed)
	default:
		return nil
	}
}

func renderPayloadMap(toolName string, prefix string, payload map[string]any) []string {
	switch {
	case strings.HasPrefix(toolName, "gmail."):
		return renderGmailPayload(toolName, payload)
	case strings.HasPrefix(toolName, "calendar."):
		return renderCalendarPayload(toolName, prefix, payload)
	case strings.HasPrefix(toolName, "chat."):
		return renderChatPayload(toolName, payload)
	case strings.HasPrefix(toolName, "drive."):
		return renderDrivePayload(toolName, payload)
	case strings.HasPrefix(toolName, "docs."):
		return renderDocsPayload(toolName, payload)
	case strings.HasPrefix(toolName, "sheets."):
		return renderSheetsPayload(toolName, payload)
	case hasAnyPayloadKey(payload, "Draft", "Drafts"):
		return renderGmailPayload(toolName, payload)
	case hasAnyPayloadKey(payload, "Event", "Title", "StartTime", "EndTime"):
		return renderCalendarPayload(toolName, prefix, payload)
	case hasAnyPayloadKey(payload, "Space", "Spaces", "Message", "Messages", "Membership"):
		return renderChatPayload(toolName, payload)
	case hasAnyPayloadKey(payload, "Files", "File", "Permission"):
		return renderDrivePayload(toolName, payload)
	case hasAnyPayloadKey(payload, "Document", "BodyText"):
		return renderDocsPayload(toolName, payload)
	case hasAnyPayloadKey(payload, "SpreadsheetID", "SpreadsheetUrl", "Values", "UpdatedRange"):
		return renderSheetsPayload(toolName, payload)
	default:
		return renderGenericPayload(payloadTitle(toolName, prefix), payload)
	}
}

func renderGmailPayload(toolName string, payload map[string]any) []string {
	if draft, ok := payloadMap(payload, "Draft"); ok {
		title := "Đã tạo bản nháp email."
		switch toolName {
		case "gmail.updateDraft":
			title = "Đã cập nhật bản nháp email."
		case "gmail.replyDraft":
			title = "Đã tạo bản nháp trả lời email."
		case "gmail.forwardDraft":
			title = "Đã tạo bản nháp chuyển tiếp email."
		}
		return append([]string{title}, payloadBullets(draft, []fieldLabel{
			{"ID", "Draft ID"},
			{"MessageID", "Message ID"},
			{"ThreadID", "Thread ID"},
		})...)
	}
	if message, ok := payloadMap(payload, "Message"); ok {
		title := "Đã xử lý email."
		if toolName == "gmail.sendDraft" {
			title = "Email \u0111\u00e3 \u0111\u01b0\u1ee3c chuy\u1ec3n cho Gmail \u0111\u1ec3 g\u1eedi."
		}
		return append([]string{title}, payloadBullets(message, []fieldLabel{
			{"ID", "Message ID"},
			{"ThreadID", "Thread ID"},
			{"To", "Người nhận"},
			{"From", "Người gửi"},
			{"Subject", "Chủ đề"},
			{"Date", "Ngày"},
		})...)
	}
	if drafts, ok := payloadArray(payload, "Drafts"); ok {
		return renderPayloadList(toolName, "Danh sách bản nháp Gmail", drafts)
	}
	if messages, ok := payloadArray(payload, "Messages"); ok {
		return renderPayloadList(toolName, "Danh sách email", messages)
	}
	return nil
}

func renderCalendarPayload(toolName string, prefix string, payload map[string]any) []string {
	event := payload
	if nested, ok := payloadMap(payload, "Event"); ok {
		event = nested
	}
	title := "Kết quả Calendar."
	switch toolName {
	case "calendar.createEvent":
		title = "Đã tạo sự kiện Calendar."
	case "calendar.updateEvent":
		title = "Đã cập nhật sự kiện Calendar."
	case "calendar.deleteEvent":
		title = "Đã xóa sự kiện Calendar."
	default:
		lowerPrefix := strings.ToLower(prefix)
		if strings.Contains(lowerPrefix, "created") {
			title = "Đã tạo sự kiện Calendar."
		} else if strings.Contains(lowerPrefix, "updated") {
			title = "Đã cập nhật sự kiện Calendar."
		}
	}
	return append([]string{title}, payloadBullets(event, []fieldLabel{
		{"Title", "Tiêu đề"},
		{"ID", "Event ID"},
		{"StartTime", "Bắt đầu"},
		{"EndTime", "Kết thúc"},
		{"Location", "Địa điểm"},
		{"MeetLink", "Google Meet"},
		{"Attendees", "Người tham gia"},
	})...)
}

func renderChatPayload(toolName string, payload map[string]any) []string {
	if message, ok := payloadMap(payload, "Message"); ok {
		title := "Đã xử lý tin nhắn Google Chat."
		switch toolName {
		case "chat.sendMessage":
			title = "Đã gửi tin nhắn Google Chat."
		case "chat.updateMessage":
			title = "Đã cập nhật tin nhắn Google Chat."
		}
		return append([]string{title}, payloadBullets(message, []fieldLabel{
			{"Name", "Message"},
			{"Text", "Nội dung"},
			{"ThreadName", "Thread"},
			{"CreateTime", "Thời gian"},
		})...)
	}
	if space, ok := payloadMap(payload, "Space"); ok {
		return append([]string{"Đã xử lý Google Chat space."}, payloadBullets(space, []fieldLabel{
			{"Name", "Space"},
			{"DisplayName", "Tên"},
			{"SpaceType", "Loại"},
			{"SpaceURI", "URI"},
		})...)
	}
	if membership, ok := payloadMap(payload, "Membership"); ok {
		return append([]string{"Đã cập nhật thành viên Google Chat."}, payloadBullets(membership, []fieldLabel{
			{"Name", "Membership"},
			{"MemberName", "User"},
			{"Email", "Email"},
			{"DisplayName", "Tên"},
			{"Role", "Vai trò"},
		})...)
	}
	if messages, ok := payloadArray(payload, "Messages"); ok {
		return renderPayloadList(toolName, "Danh sách tin nhắn Google Chat", messages)
	}
	if spaces, ok := payloadArray(payload, "Spaces"); ok {
		return renderPayloadList(toolName, "Danh sách Google Chat space", spaces)
	}
	return renderGenericPayload(payloadTitle(toolName, ""), payload)
}

func renderDrivePayload(toolName string, payload map[string]any) []string {
	if files, ok := payloadArray(payload, "Files"); ok {
		title := "Danh sách tệp Google Drive"
		if toolName == "drive.listFiles" {
			title = "Danh sách Google Drive"
		}
		return renderPayloadList(toolName, title, files)
	}
	if file, ok := payloadMap(payload, "File"); ok {
		return append([]string{"Tệp Google Drive."}, driveFileBullets(file)...)
	}
	if permission, ok := payloadMap(payload, "Permission"); ok {
		return append([]string{"Đã cập nhật chia sẻ Google Drive."}, payloadBullets(permission, []fieldLabel{
			{"Type", "Loại"},
			{"Role", "Quyền"},
			{"EmailAddress", "Email"},
			{"ID", "Permission ID"},
		})...)
	}
	return renderGenericPayload(payloadTitle(toolName, ""), payload)
}

func driveFileBullets(file map[string]any) []string {
	return payloadBullets(file, []fieldLabel{
		{"Name", "Tên"},
		{"ID", "File ID"},
		{"MimeType", "Loại"},
		{"WebViewLink", "Link"},
		{"ModifiedTime", "Cập nhật"},
		{"Owners", "Chủ sở hữu"},
	})
}

func renderDocsPayload(toolName string, payload map[string]any) []string {
	if document, ok := payloadMap(payload, "Document"); ok {
		title := "Tài liệu Google Docs."
		if toolName == "docs.createDocument" {
			title = "Đã tạo Google Docs."
		}
		return append([]string{title}, payloadBullets(document, []fieldLabel{
			{"Title", "Tiêu đề"},
			{"ID", "Document ID"},
			{"Text", "Nội dung"},
		})...)
	}
	return renderGenericPayload(payloadTitle(toolName, ""), payload)
}

func renderSheetsPayload(toolName string, payload map[string]any) []string {
	title := "Kết quả Google Sheets"
	switch toolName {
	case "sheets.createSpreadsheet":
		title = "Đã tạo Google Sheets"
	case "sheets.updateValues":
		title = "Đã cập nhật Google Sheets"
	case "sheets.appendValues":
		title = "Đã thêm dữ liệu vào Google Sheets"
	}
	return append([]string{title + "."}, payloadBullets(payload, []fieldLabel{
		{"Title", "Tiêu đề"},
		{"SpreadsheetID", "Spreadsheet ID"},
		{"SpreadsheetURL", "Link"},
		{"Range", "Vùng dữ liệu"},
		{"UpdatedRange", "Vùng đã cập nhật"},
		{"UpdatedRows", "Số dòng"},
		{"UpdatedCells", "Số ô"},
	})...)
}

func renderPayloadList(_ string, title string, items []any) []string {
	if title == "" {
		title = "Kết quả"
	}
	if len(items) == 0 {
		return []string{title + ": không có dữ liệu phù hợp."}
	}
	lines := []string{title + ":"}
	for index, item := range items {
		if index >= 10 {
			lines = append(lines, fmt.Sprintf("- ...và %d mục khác", len(items)-index))
			break
		}
		if itemMap, ok := item.(map[string]any); ok {
			lines = append(lines, "- "+compactPayloadItem(itemMap))
			continue
		}
		lines = append(lines, "- "+fmt.Sprint(item))
	}
	return lines
}

func renderGenericPayload(title string, payload map[string]any) []string {
	if title == "" {
		title = "Kết quả"
	}
	lines := []string{title + ":"}
	for _, label := range genericFieldLabels() {
		if line := payloadBullet(payload, label); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > 1 {
		return lines
	}
	for key, value := range payload {
		if rendered := renderPayloadScalar(value); rendered != "" {
			lines = append(lines, "- "+humanizeKey(key)+": "+rendered)
		}
	}
	return lines
}

type fieldLabel struct {
	Key   string
	Label string
}

func payloadBullets(payload map[string]any, labels []fieldLabel) []string {
	lines := make([]string, 0, len(labels))
	for _, label := range labels {
		if line := payloadBullet(payload, label); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func payloadBullet(payload map[string]any, label fieldLabel) string {
	value, ok := payload[label.Key]
	if !ok {
		return ""
	}
	rendered := renderPayloadScalar(value)
	if rendered == "" {
		return ""
	}
	return "- " + label.Label + ": " + rendered
}

func renderPayloadScalar(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case bool:
		if typed {
			return "có"
		}
		return "không"
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", typed), "0"), ".")
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if itemMap, ok := item.(map[string]any); ok {
				parts = append(parts, compactPayloadItem(itemMap))
				continue
			}
			if text := renderPayloadScalar(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		return compactPayloadItem(typed)
	default:
		return fmt.Sprint(value)
	}
}

func compactPayloadItem(payload map[string]any) string {
	if text := compactDriveFileItem(payload); text != "" {
		return text
	}
	for _, key := range []string{"Title", "Subject", "DisplayName", "Email", "Name", "ID", "Text", "Snippet"} {
		if text := renderPayloadScalar(payload[key]); text != "" {
			return text
		}
	}
	parts := make([]string, 0, 3)
	for _, label := range genericFieldLabels() {
		if text := renderPayloadScalar(payload[label.Key]); text != "" {
			parts = append(parts, label.Label+": "+text)
		}
		if len(parts) >= 3 {
			break
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}
	return "một mục"
}

func compactDriveFileItem(payload map[string]any) string {
	name := renderPayloadScalar(payload["Name"])
	link := renderPayloadScalar(payload["WebViewLink"])
	modified := renderPayloadScalar(payload["ModifiedTime"])
	if name == "" || (link == "" && modified == "") {
		return ""
	}
	parts := []string{name}
	if link != "" {
		parts = append(parts, "Link: "+link)
	}
	if modified != "" {
		parts = append(parts, "Cập nhật: "+modified)
	}
	return strings.Join(parts, " - ")
}

func payloadMap(payload map[string]any, key string) (map[string]any, bool) {
	value, ok := payload[key]
	if !ok {
		return nil, false
	}
	typed, ok := value.(map[string]any)
	return typed, ok
}

func payloadArray(payload map[string]any, key string) ([]any, bool) {
	value, ok := payload[key]
	if !ok {
		return nil, false
	}
	typed, ok := value.([]any)
	return typed, ok
}

func hasAnyPayloadKey(payload map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := payload[key]; ok {
			return true
		}
	}
	return false
}

func payloadTitle(toolName string, prefix string) string {
	if strings.TrimSpace(prefix) != "" {
		return strings.TrimSpace(prefix)
	}
	switch {
	case strings.HasPrefix(toolName, "gmail."):
		return "Kết quả Gmail"
	case strings.HasPrefix(toolName, "calendar."):
		return "Kết quả Calendar"
	case strings.HasPrefix(toolName, "chat."):
		return "Kết quả Google Chat"
	case strings.HasPrefix(toolName, "people."):
		return "Kết quả danh bạ Workspace"
	case strings.HasPrefix(toolName, "drive."):
		return "Kết quả Google Drive"
	case strings.HasPrefix(toolName, "docs."):
		return "Kết quả Google Docs"
	case strings.HasPrefix(toolName, "sheets."):
		return "Kết quả Google Sheets"
	case strings.HasPrefix(toolName, "web."):
		return "Kết quả web"
	default:
		return "Kết quả"
	}
}

func genericFieldLabels() []fieldLabel {
	return []fieldLabel{
		{"Title", "Tiêu đề"},
		{"Subject", "Chủ đề"},
		{"ID", "ID"},
		{"Name", "Tên"},
		{"DisplayName", "Tên hiển thị"},
		{"Email", "Email"},
		{"From", "Người gửi"},
		{"To", "Người nhận"},
		{"Date", "Ngày"},
		{"Snippet", "Tóm tắt"},
		{"Text", "Nội dung"},
		{"NextPageToken", "Trang tiếp theo"},
	}
}

func humanizeKey(key string) string {
	switch key {
	case "ID":
		return "ID"
	case "MessageID":
		return "Message ID"
	case "ThreadID":
		return "Thread ID"
	case "NextPageToken":
		return "Trang tiếp theo"
	default:
		return key
	}
}
