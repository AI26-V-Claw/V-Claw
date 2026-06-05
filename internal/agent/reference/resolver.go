package reference

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"vclaw/internal/providers"
	"vclaw/internal/sessions"
)

type Type string

const (
	TypeNone              Type = "none"
	TypeCalendarEvent     Type = "calendar_event"
	TypeGmailEmail        Type = "gmail_email"
	TypeChatSpace         Type = "chat_space"
	TypeChatMessage       Type = "chat_message"
	TypeConversationTopic Type = "conversation_topic"
	TypeUnknown           Type = "unknown"
)

type Source string

const (
	SourceNone             Source = "none"
	SourceLastActionResult Source = "last_action_result"
	SourceRecentHistory    Source = "recent_history"
	SourceMemorySummary    Source = "memory_summary"
)

type Input struct {
	CurrentMessage string
	RecentHistory  []string
	Memory         sessions.SessionMemory
	Now            time.Time
}

type Resolution struct {
	HasReference          bool           `json:"hasReference"`
	ReferenceType         Type           `json:"referenceType"`
	ReferenceID           string         `json:"referenceId,omitempty"`
	Source                Source         `json:"source,omitempty"`
	Confidence            float64        `json:"confidence"`
	NeedsClarification    bool           `json:"needsClarification"`
	ClarificationQuestion string         `json:"clarificationQuestion,omitempty"`
	ResolvedContext       map[string]any `json:"resolvedContext,omitempty"`
	Reasoning             string         `json:"reasoning,omitempty"`
}

type Resolver interface {
	Resolve(ctx context.Context, input Input) (*Resolution, error)
}

type HeuristicResolver struct{}

func NewHeuristicResolver() *HeuristicResolver {
	return &HeuristicResolver{}
}

func (r *HeuristicResolver) Resolve(_ context.Context, input Input) (*Resolution, error) {
	text := foldVietnameseSearchText(strings.ToLower(strings.TrimSpace(input.CurrentMessage)))
	if text == "" {
		return noReference(), nil
	}

	if hasDraftReferenceCue(text) {
		return resolveDraftFromActionResults(input), nil
	}
	if containsAny(text, "email này", "email nay", "mail này", "mail nay", "email vừa rồi", "email vua roi", "mail vừa rồi", "mail vua roi") {
		return resolveFromActionResults(input, TypeGmailEmail, "gmail.", "gmail email"), nil
	}
	if containsAny(text,
		"lich nay", "lich vua roi", "lich vua tao", "lich vua moi tao",
		"su kien nay", "su kien vua roi", "su kien vua tao", "su kien vua moi tao",
		"event nay", "event vua roi", "event vua tao",
		"cuoc hop tren", "cuoc hop o tren", "cuoc hop vua liet ke", "cuoc hop vua roi", "meeting above", "meeting vua roi",
	) {
		return resolveFromActionResults(input, TypeCalendarEvent, "calendar.", "calendar event"), nil
	}
	if containsAny(text, "space này", "space nay", "nhóm chat này", "nhom chat nay", "chat này", "chat nay") {
		return resolveFromActionResults(input, TypeChatSpace, "chat.", "chat space"), nil
	}
	if containsAny(text, "tin nhắn này", "tin nhan nay", "message này", "message nay", "tin nhắn vừa rồi", "tin nhan vua roi") {
		return resolveFromActionResults(input, TypeChatMessage, "chat.", "chat message"), nil
	}
	if containsAny(text, "nội dung mình vừa nói", "noi dung minh vua noi", "nội dung vừa nói", "noi dung vua noi", "chủ đề đó", "chu de do", "chủ đề này", "chu de nay", "note lại", "ghi chú lại", "tom tat", "tóm tắt") {
		return resolveConversationTopic(input), nil
	}

	return noReference(), nil
}

type LLMResolver struct {
	provider providers.Provider
	model    string
}

func NewLLMResolver(provider providers.Provider, model string) *LLMResolver {
	return &LLMResolver{provider: provider, model: strings.TrimSpace(model)}
}

func (r *LLMResolver) Resolve(ctx context.Context, input Input) (*Resolution, error) {
	if r == nil || r.provider == nil {
		return nil, fmt.Errorf("reference resolver provider is required")
	}
	resp, err := r.provider.Generate(ctx, &providers.GenerateRequest{
		SystemPrompt:   BuildSystemPrompt(),
		UserPrompt:     BuildUserPrompt(input),
		Temperature:    0.1,
		MaxTokens:      1200,
		ResponseFormat: "json",
		Model:          r.model,
	})
	if err != nil {
		return nil, fmt.Errorf("reference resolution failed: %w", err)
	}
	var resolution Resolution
	if err := json.Unmarshal([]byte(extractJSONObject(resp.Text)), &resolution); err != nil {
		return nil, fmt.Errorf("parse reference resolver response: %w", err)
	}
	return normalizeResolution(&resolution), nil
}

type FallbackResolver struct {
	primary  Resolver
	fallback Resolver
}

func NewFallbackResolver(primary Resolver, fallback Resolver) *FallbackResolver {
	return &FallbackResolver{primary: primary, fallback: fallback}
}

func (r *FallbackResolver) Resolve(ctx context.Context, input Input) (*Resolution, error) {
	var primaryResolution *Resolution
	var primaryErr error
	if r != nil && r.primary != nil {
		primaryResolution, primaryErr = r.primary.Resolve(ctx, input)
		if primaryErr == nil {
			primaryResolution = normalizeResolution(primaryResolution)
			if usableResolution(primaryResolution) {
				if r.fallback != nil {
					fallbackResolution, fallbackErr := r.fallback.Resolve(ctx, input)
					if fallbackErr == nil {
						fallbackResolution = normalizeResolution(fallbackResolution)
						if shouldPreferStrongLexicalFallback(input.CurrentMessage, primaryResolution, fallbackResolution) {
							return fallbackResolution, nil
						}
					}
				}
				return primaryResolution, nil
			}
		}
	}
	if r != nil && r.fallback != nil {
		fallbackResolution, fallbackErr := r.fallback.Resolve(ctx, input)
		if fallbackErr == nil {
			fallbackResolution = normalizeResolution(fallbackResolution)
			if usableResolution(fallbackResolution) {
				return fallbackResolution, nil
			}
			if primaryErr != nil || primaryResolution == nil {
				return fallbackResolution, nil
			}
			return primaryResolution, nil
		}
		if primaryErr != nil {
			return nil, fallbackErr
		}
	}
	if primaryErr == nil && primaryResolution != nil {
		return primaryResolution, nil
	}
	if primaryErr != nil {
		return nil, primaryErr
	}
	return noReference(), nil
}

func BuildSystemPrompt() string {
	return strings.TrimSpace(`<reference_resolver_system_prompt>
  <persona>
    <identity>Bạn là Reference Resolver của V-Claw, một local-first personal AI agent assistant.</identity>
    <mission>Nhận diện xem tin nhắn hiện tại có tham chiếu tới đối tượng hoặc chủ đề đã xuất hiện trong ngữ cảnh ngắn hạn hay không.</mission>
    <language>Luôn viết reasoning và clarification bằng tiếng Việt ngắn gọn.</language>
  </persona>

  <rules>
    <rule>Chỉ resolve tham chiếu, không lập kế hoạch, không gọi tool, không xác nhận hành động đã hoàn tất.</rule>
    <rule>Nhận diện các cụm như "lịch này", "cuộc họp trên", "cuộc họp vừa liệt kê", "email vừa rồi", "bản nháp vừa tạo", "draft vừa rồi", "tin nhắn này", "chủ đề đó", "nội dung mình vừa nói".</rule>
    <rule>Nếu tin nhắn nhắc "draft" hoặc "bản nháp", reference_type phải là gmail_email, không phải calendar_event, kể cả khi có cụm "vừa tạo".</rule>
    <rule>Nếu tham chiếu trỏ rõ tới last_action_result hoặc recent_history, trả confidence cao.</rule>
    <rule>Nếu có nhiều đối tượng phù hợp hoặc thiếu ngữ cảnh, đặt needsClarification=true và hỏi một câu tiếng Việt ngắn.</rule>
    <rule>Không dùng memory để tự lấy tham số cho write/destructive action. Memory chỉ giúp hiểu user đang nói tới đối tượng nào.</rule>
    <rule>Nếu tin nhắn là yêu cầu mới không có đại từ/tham chiếu, trả hasReference=false.</rule>
    <rule>Nếu đại từ chỉ trỏ tới đối tượng vừa được đề cập trong cùng câu/tin nhắn (forward reference, ví dụ: "tạo sự kiện X... mời tham dự sự kiện này"), không coi là tham chiếu tới đối tượng cũ — trả hasReference=false.</rule>
  </rules>

  <tools_instruction>
    <reference_type name="calendar_event">Google Calendar event đã đọc/tạo/sửa/xóa gần đây.</reference_type>
    <reference_type name="gmail_email">Email hoặc draft Gmail gần đây.</reference_type>
    <reference_type name="chat_space">Google Chat space gần đây.</reference_type>
    <reference_type name="chat_message">Google Chat message gần đây.</reference_type>
    <reference_type name="conversation_topic">Chủ đề hoặc nội dung trao đổi trong hội thoại gần đây.</reference_type>
  </tools_instruction>

  <response_format>
    <format>Chỉ trả về một JSON object hợp lệ. Không Markdown, không giải thích ngoài JSON.</format>
    <schema>
      {
        "hasReference": false,
        "referenceType": "none|calendar_event|gmail_email|chat_space|chat_message|conversation_topic|unknown",
        "referenceId": "",
        "source": "none|last_action_result|recent_history|memory_summary",
        "confidence": 0.0,
        "needsClarification": false,
        "clarificationQuestion": "",
        "resolvedContext": {},
        "reasoning": "Giải thích ngắn bằng tiếng Việt"
      }
    </schema>
  </response_format>

  <constraints>
    <constraint>Không đưa secret, token, chat_id, user_id, hoặc dữ liệu nhạy cảm không cần thiết vào reasoning.</constraint>
    <constraint>Không tự chọn đối tượng nếu confidence dưới 0.60.</constraint>
    <constraint>Không biến tham chiếu mơ hồ thành hành động write.</constraint>
  </constraints>
</reference_resolver_system_prompt>`)
}

func BuildUserPrompt(input Input) string {
	return strings.TrimSpace(fmt.Sprintf(`<reference_resolution_request>
  <current_time>%s</current_time>
  <current_user_message>%s</current_user_message>
  <recent_history>%s</recent_history>
  <memory_summary>%s</memory_summary>
  <last_action_results>%s</last_action_results>
</reference_resolution_request>`,
		input.Now.Format(time.RFC3339),
		xmlEscape(input.CurrentMessage),
		xmlEscape(strings.Join(input.RecentHistory, "\n")),
		xmlEscape(input.Memory.Summary),
		xmlEscape(formatActionResults(input.Memory.LastActionResults)),
	))
}

func noReference() *Resolution {
	return &Resolution{
		HasReference:  false,
		ReferenceType: TypeNone,
		Source:        SourceNone,
		Confidence:    1,
	}
}

func resolveFromActionResults(input Input, refType Type, toolPrefix string, label string) *Resolution {
	matches := make([]sessions.ActionResult, 0, 2)
	for i := len(input.Memory.LastActionResults) - 1; i >= 0; i-- {
		result := input.Memory.LastActionResults[i]
		if strings.HasPrefix(strings.ToLower(result.ToolName), toolPrefix) && strings.TrimSpace(result.Content) != "" {
			matches = append(matches, result)
			if len(matches) >= 2 {
				break
			}
		}
	}
	if len(matches) == 1 {
		return actionResultResolution(refType, matches[0])
	}
	if len(matches) > 1 {
		return &Resolution{
			HasReference:          true,
			ReferenceType:         refType,
			Source:                SourceLastActionResult,
			Confidence:            0.45,
			NeedsClarification:    true,
			ClarificationQuestion: "Bạn muốn nói tới " + label + " nào gần đây?",
			Reasoning:             "Có nhiều kết quả gần đây cùng loại nên cần hỏi lại.",
		}
	}

	historyContext := newestHistoryContaining(input.RecentHistory, historyNeedles(refType, toolPrefix)...)
	if historyContext != "" {
		return &Resolution{
			HasReference:    true,
			ReferenceType:   refType,
			Source:          SourceRecentHistory,
			Confidence:      0.72,
			ResolvedContext: map[string]any{"text": historyContext},
			Reasoning:       "Tin nhắn có tham chiếu và recent_history có ngữ cảnh phù hợp.",
		}
	}

	return &Resolution{
		HasReference:          true,
		ReferenceType:         refType,
		Source:                SourceNone,
		Confidence:            0.35,
		NeedsClarification:    true,
		ClarificationQuestion: "Bạn muốn nói tới mục nào gần đây?",
		Reasoning:             "Tin nhắn có tham chiếu nhưng không tìm thấy đối tượng gần đây.",
	}
}

func resolveDraftFromActionResults(input Input) *Resolution {
	matches := make([]sessions.ActionResult, 0, 2)
	for i := len(input.Memory.LastActionResults) - 1; i >= 0; i-- {
		result := input.Memory.LastActionResults[i]
		if isDraftActionResult(result) && strings.TrimSpace(result.Content) != "" {
			matches = append(matches, result)
			if len(matches) >= 2 {
				break
			}
		}
	}
	if len(matches) == 1 {
		return actionResultResolution(TypeGmailEmail, matches[0])
	}
	if len(matches) > 1 && hasLatestDraftReferenceCue(input.CurrentMessage) {
		return actionResultResolution(TypeGmailEmail, matches[0])
	}
	if len(matches) > 1 {
		return &Resolution{
			HasReference:          true,
			ReferenceType:         TypeGmailEmail,
			Source:                SourceLastActionResult,
			Confidence:            0.45,
			NeedsClarification:    true,
			ClarificationQuestion: "Bạn muốn gửi bản nháp Gmail nào gần đây?",
			Reasoning:             "Có nhiều bản nháp Gmail gần đây nên cần hỏi lại.",
		}
	}

	historyContext := newestHistoryContaining(input.RecentHistory, "draft", "ban nhap", "gmail.createdraft")
	if historyContext != "" {
		return &Resolution{
			HasReference:    true,
			ReferenceType:   TypeGmailEmail,
			Source:          SourceRecentHistory,
			Confidence:      0.72,
			ResolvedContext: map[string]any{"text": historyContext},
			Reasoning:       "Tin nhắn nhắc bản nháp và recent_history có ngữ cảnh Gmail draft.",
		}
	}

	return &Resolution{
		HasReference:          true,
		ReferenceType:         TypeGmailEmail,
		Source:                SourceNone,
		Confidence:            0.35,
		NeedsClarification:    true,
		ClarificationQuestion: "Bạn muốn gửi bản nháp Gmail nào gần đây?",
		Reasoning:             "Tin nhắn nhắc bản nháp nhưng không tìm thấy draft gần đây.",
	}
}

func isDraftActionResult(result sessions.ActionResult) bool {
	toolName := strings.ToLower(strings.TrimSpace(result.ToolName))
	content := strings.ToLower(strings.TrimSpace(result.Content))
	return strings.Contains(toolName, "draft") ||
		strings.Contains(content, `"draft"`) ||
		strings.Contains(content, "draft id") ||
		strings.Contains(foldVietnameseSearchText(content), "ban nhap")
}

func hasLatestDraftReferenceCue(text string) bool {
	lower := foldVietnameseSearchText(strings.ToLower(strings.TrimSpace(text)))
	if lower == "" {
		return false
	}
	if !strings.Contains(lower, "draft") && !strings.Contains(lower, "ban nhap") {
		return false
	}
	return containsAny(lower,
		"nay",
		"do",
		"vua tao",
		"vua roi",
		"vua tao ra",
		"da tao",
		"ban tao",
		"ban da tao",
		"ban vua tao",
		"o tren",
		"gan nhat",
		"moi nhat",
	)
}

func resolveConversationTopic(input Input) *Resolution {
	contextText := strings.TrimSpace(input.Memory.Summary)
	source := SourceMemorySummary
	if contextText == "" {
		contextText = strings.TrimSpace(strings.Join(input.RecentHistory, "\n"))
		source = SourceRecentHistory
	}
	if contextText == "" {
		return &Resolution{
			HasReference:          true,
			ReferenceType:         TypeConversationTopic,
			Source:                SourceNone,
			Confidence:            0.3,
			NeedsClarification:    true,
			ClarificationQuestion: "Bạn muốn tôi note lại chủ đề nào?",
			Reasoning:             "Có tham chiếu tới chủ đề nhưng chưa có ngữ cảnh đủ rõ.",
		}
	}
	return &Resolution{
		HasReference:    true,
		ReferenceType:   TypeConversationTopic,
		Source:          source,
		Confidence:      0.78,
		ResolvedContext: map[string]any{"text": contextText},
		Reasoning:       "Tin nhắn tham chiếu tới chủ đề trong hội thoại gần đây.",
	}
}

func actionResultResolution(refType Type, result sessions.ActionResult) *Resolution {
	context := map[string]any{
		"toolName":  result.ToolName,
		"content":   result.Content,
		"createdAt": result.CreatedAt.Format(time.RFC3339),
	}
	if id := extractActionResultID(refType, result); id != "" {
		context["id"] = id
		if refType == TypeGmailEmail && strings.Contains(strings.ToLower(result.ToolName), "draft") {
			context["draftId"] = id
			context["actionHint"] = "If the user asks to send this existing draft, use gmail.sendDraft with draftId. Do not use MessageID or ThreadID as draftId. Do not ask for to, subject, or body again unless the user wants to edit the draft."
		}
	}
	return &Resolution{
		HasReference:    true,
		ReferenceType:   refType,
		ReferenceID:     stringValue(context["id"]),
		Source:          SourceLastActionResult,
		Confidence:      0.88,
		ResolvedContext: context,
		Reasoning:       "Tham chiếu khớp với action result gần nhất.",
	}
}

func normalizeResolution(resolution *Resolution) *Resolution {
	if resolution == nil {
		return noReference()
	}
	switch resolution.ReferenceType {
	case TypeNone, TypeCalendarEvent, TypeGmailEmail, TypeChatSpace, TypeChatMessage, TypeConversationTopic, TypeUnknown:
	default:
		resolution.ReferenceType = TypeUnknown
	}
	switch resolution.Source {
	case SourceNone, SourceLastActionResult, SourceRecentHistory, SourceMemorySummary:
	default:
		resolution.Source = SourceNone
	}
	if resolution.ReferenceType == TypeNone {
		resolution.HasReference = false
		resolution.NeedsClarification = false
	}
	if resolution.Confidence < 0 {
		resolution.Confidence = 0
	}
	if resolution.Confidence > 1 {
		resolution.Confidence = 1
	}
	if resolution.HasReference && resolution.Confidence < 0.6 && strings.TrimSpace(resolution.ClarificationQuestion) == "" {
		resolution.NeedsClarification = true
		resolution.ClarificationQuestion = "Bạn muốn nói tới mục nào trong cuộc trò chuyện trước đó?"
	}
	return resolution
}

func usableResolution(resolution *Resolution) bool {
	if resolution == nil {
		return false
	}
	return resolution.HasReference &&
		resolution.ReferenceType != TypeNone &&
		!resolution.NeedsClarification &&
		resolution.Confidence >= 0.6
}

func shouldPreferStrongLexicalFallback(text string, primary *Resolution, fallback *Resolution) bool {
	if !usableResolution(primary) || !usableResolution(fallback) {
		return false
	}
	if primary.ReferenceType == fallback.ReferenceType {
		return false
	}
	return hasStrongReferenceCueForType(text, fallback.ReferenceType)
}

func hasStrongReferenceCueForType(text string, refType Type) bool {
	lower := foldVietnameseSearchText(strings.ToLower(strings.TrimSpace(text)))
	if lower == "" {
		return false
	}
	switch refType {
	case TypeGmailEmail:
		return containsAny(lower, "draft", "ban nhap", "email", "mail", "gmail")
	case TypeCalendarEvent:
		return containsAny(lower, "lich", "calendar", "su kien", "event", "cuoc hop", "meeting")
	case TypeChatSpace, TypeChatMessage:
		return containsAny(lower, "chat", "space", "nhom chat", "tin nhan", "message")
	case TypeConversationTopic:
		return containsAny(lower, "chu de", "noi dung", "note", "ghi chu", "tom tat")
	default:
		return false
	}
}

func hasDraftReferenceCue(text string) bool {
	lower := foldVietnameseSearchText(strings.ToLower(strings.TrimSpace(text)))
	if lower == "" || !containsAny(lower, "draft", "ban nhap") {
		return false
	}
	return containsAny(lower,
		"nay", "do", "vua roi", "vua tao", "da tao", "ban tao", "ban da tao", "ban vua tao",
		"gui", "send", "email", "mail",
	)
}

func formatActionResults(results []sessions.ActionResult) string {
	lines := make([]string, 0, len(results))
	for _, result := range results {
		if strings.TrimSpace(result.Content) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s at %s: %s", result.ToolName, result.CreatedAt.Format(time.RFC3339), result.Content))
	}
	return strings.Join(lines, "\n")
}

func historyNeedles(refType Type, toolPrefix string) []string {
	needles := []string{toolPrefix}
	switch refType {
	case TypeCalendarEvent:
		needles = append(needles, "event created", "event updated", "event deleted", "calendar", "sự kiện", "su kien")
	case TypeGmailEmail:
		needles = append(needles, "email", "gmail", "mail", "draft")
	case TypeChatSpace, TypeChatMessage:
		needles = append(needles, "chat", "message", "space", "tin nhắn", "tin nhan")
	}
	return needles
}

func newestHistoryContaining(history []string, needles ...string) string {
	normalized := make([]string, 0, len(needles))
	for _, needle := range needles {
		needle = strings.ToLower(strings.TrimSpace(needle))
		if needle != "" {
			normalized = append(normalized, needle)
		}
	}
	for i := len(history) - 1; i >= 0; i-- {
		item := strings.TrimSpace(history[i])
		lower := strings.ToLower(item)
		for _, needle := range normalized {
			if strings.Contains(lower, needle) {
				return item
			}
		}
	}
	return ""
}

func extractJSONObject(text string) string {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end >= start {
		return trimmed[start : end+1]
	}
	return trimmed
}

func xmlEscape(value string) string {
	return html.EscapeString(strings.TrimSpace(value))
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func foldVietnameseSearchText(text string) string {
	replacer := strings.NewReplacer(
		"\u00e0", "a", "\u00e1", "a", "\u1ea1", "a", "\u1ea3", "a", "\u00e3", "a",
		"\u00e2", "a", "\u1ea7", "a", "\u1ea5", "a", "\u1ead", "a", "\u1ea9", "a", "\u1eab", "a",
		"\u0103", "a", "\u1eb1", "a", "\u1eaf", "a", "\u1eb7", "a", "\u1eb3", "a", "\u1eb5", "a",
		"\u00e8", "e", "\u00e9", "e", "\u1eb9", "e", "\u1ebb", "e", "\u1ebd", "e",
		"\u00ea", "e", "\u1ec1", "e", "\u1ebf", "e", "\u1ec7", "e", "\u1ec3", "e", "\u1ec5", "e",
		"\u00ec", "i", "\u00ed", "i", "\u1ecb", "i", "\u1ec9", "i", "\u0129", "i",
		"\u00f2", "o", "\u00f3", "o", "\u1ecd", "o", "\u1ecf", "o", "\u00f5", "o",
		"\u00f4", "o", "\u1ed3", "o", "\u1ed1", "o", "\u1ed9", "o", "\u1ed5", "o", "\u1ed7", "o",
		"\u01a1", "o", "\u1edd", "o", "\u1edb", "o", "\u1ee3", "o", "\u1edf", "o", "\u1ee1", "o",
		"\u00f9", "u", "\u00fa", "u", "\u1ee5", "u", "\u1ee7", "u", "\u0169", "u",
		"\u01b0", "u", "\u1eeb", "u", "\u1ee9", "u", "\u1ef1", "u", "\u1eed", "u", "\u1eef", "u",
		"\u1ef3", "y", "\u00fd", "y", "\u1ef5", "y", "\u1ef7", "y", "\u1ef9", "y",
		"\u0111", "d",
	)
	return replacer.Replace(text)
}

var likelyIDPattern = regexp.MustCompile(`(?i)"?(id|eventId|messageId|threadId)"?\s*[:=]\s*"?([A-Za-z0-9_.:/-]+)"?`)
var draftIDPattern = regexp.MustCompile(`(?i)\bdraft\s*id\b\s*[:=]\s*"?([A-Za-z0-9_.:/-]+)"?`)

func extractActionResultID(refType Type, result sessions.ActionResult) string {
	if refType == TypeGmailEmail && strings.Contains(strings.ToLower(result.ToolName), "draft") {
		if id := extractDraftID(result.Content); id != "" {
			return id
		}
	}
	return extractLikelyID(result.Content)
}

func extractDraftID(text string) string {
	if id := extractJSONDraftID(text); id != "" {
		return id
	}
	match := draftIDPattern.FindStringSubmatch(text)
	if len(match) >= 2 {
		return strings.Trim(match[1], `"'`)
	}
	return ""
}

func extractJSONDraftID(text string) string {
	jsonText := extractJSONObject(text)
	if jsonText == "" || !strings.HasPrefix(strings.TrimSpace(jsonText), "{") {
		return ""
	}
	var value any
	if err := json.Unmarshal([]byte(jsonText), &value); err != nil {
		return ""
	}
	return findJSONDraftID(value)
}

func findJSONDraftID(value any) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if draft, ok := lookupJSONField(object, "Draft"); ok {
		if id := jsonStringField(draft, "ID", "id", "draftId", "DraftID"); id != "" {
			return id
		}
	}
	if id := jsonStringField(object, "draftId", "DraftID"); id != "" {
		return id
	}
	return ""
}

func lookupJSONField(object map[string]any, name string) (any, bool) {
	for key, value := range object {
		if strings.EqualFold(strings.TrimSpace(key), name) {
			return value, true
		}
	}
	return nil, false
}

func jsonStringField(value any, names ...string) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	for _, name := range names {
		if value, ok := lookupJSONField(object, name); ok {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func extractLikelyID(text string) string {
	match := likelyIDPattern.FindStringSubmatch(text)
	if len(match) < 3 {
		return ""
	}
	return strings.Trim(match[2], `"'`)
}

func stringValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}
