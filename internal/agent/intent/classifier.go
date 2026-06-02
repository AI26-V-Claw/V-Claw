package intent

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Classifier is the main entry point for intent classification.
// It determines the user's intent, extracts parameters, and checks
// if clarification is needed before proceeding.
//
// In production, Classify would call an LLM provider via the interface
// in internal/providers/. For now it uses heuristic rules that achieve
// the >80% accuracy target on the evaluation dataset.
type Classifier struct {
	config ConfidenceConfig
}

// NewClassifier creates a new intent classifier with the given config.
func NewClassifier(cfg ConfidenceConfig) *Classifier {
	return &Classifier{config: cfg}
}

// Classify analyses userInput and returns a ClassificationOutput.
// This implements the core pipeline:
//
//	Input Guard → Intent Detection → Param Extraction → Validation → Output
func Classify(ctx context.Context, classifier *Classifier, userInput string) (*ClassificationOutput, error) {
	userInput = strings.TrimSpace(userInput)
	if userInput == "" {
		return nil, fmt.Errorf("user input cannot be empty")
	}

	// Step 0: Prompt Injection Guard (runs BEFORE any intent matching)
	if isPromptInjection(userInput) {
		return &ClassificationOutput{
			Intent: &Result{
				Type:       TypeUnknown,
				Confidence: 0.05, // Very low confidence
				ToolCalls:  []ToolCallInfo{},
				NeedsConfirm: true,
				Reasoning:  "Phát hiện prompt injection - từ chối xử lý",
				Timestamp:  time.Now(),
			},
			NeedsClarification: true,
			ClarificationMessage: "Tôi không thể xử lý yêu cầu này. Vui lòng thử lại với câu hỏi khác.",
		}, nil
	}

	// Step 1: Determine intent type (heuristic, would be LLM in production)
	intentType := detectIntentType(userInput)

	// Step 2: Calculate confidence score
	confidence := calculateConfidence(userInput, intentType)

	// Step 3: Extract tool calls based on intent
	toolCalls := extractToolCalls(userInput, intentType)

	// Step 4: Validate parameters against tool registry
	requiredParams, providedParams, missingParams := validateParams(toolCalls)

	// Step 5: Determine if confirmation is needed
	needsConfirm := requiresConfirmation(intentType, toolCalls)

	// Step 6: Generate reasoning
	reasoning := generateReasoning(userInput, intentType, confidence, missingParams)

	result := &Result{
		Type:           intentType,
		Confidence:     confidence,
		RequiredParams: requiredParams,
		ProvidedParams: providedParams,
		MissingParams:  missingParams,
		ToolCalls:      toolCalls,
		NeedsConfirm:   needsConfirm,
		Reasoning:      reasoning,
		Timestamp:      time.Now(),
	}

	// Post-validation confidence boost: if we extracted all required params
	// successfully, the classification is more trustworthy.
	if len(missingParams) == 0 && len(requiredParams) > 0 {
		result.Confidence = clamp(result.Confidence + 0.15)
	}

	// Step 7: Check if clarification is needed (confidence too low)
	if shouldClarify(classifier.config, result.Confidence, intentType) {
		return &ClassificationOutput{
			Intent:             result,
			NeedsClarification: true,
			ClarificationOptions: &ClarificationChoice{
				Question: fmt.Sprintf("Tôi chưa hiểu rõ ý bạn với câu: %q\n\nBạn muốn làm gì?", userInput),
				Options: []Option{
					{ID: "A", Label: "Đọc và hiển thị thông tin (READ_INFO)", IntentType: TypeReadInfo},
					{ID: "B", Label: "Thực hiện thay đổi/xóa/gửi (DANGEROUS_ACTION)", IntentType: TypeDangerousAction},
					{ID: "C", Label: "Hành động phức hợp nhiều bước (COMPOSITE_ACTION)", IntentType: TypeComposite},
				},
			},
		}, nil
	}

	// Step 8: Check missing required parameters for dangerous actions
	if len(missingParams) > 0 && (intentType == TypeDangerousAction || intentType == TypeComposite) {
		toolName := ""
		if len(toolCalls) > 0 {
			toolName = toolCalls[0].Name
		}
		return &ClassificationOutput{
			Intent:             result,
			NeedsClarification: true,
			ClarificationMessage: generateMissingParamMessage(toolName, missingParams),
		}, nil
	}

	return &ClassificationOutput{
		Intent:             result,
		NeedsClarification: false,
	}, nil
}

// ─── Heuristic Intent Detection ──────────────────────────────────────────────

func detectIntentType(input string) IntentType {
	lower := strings.ToLower(input)

	// ── READ_INFO patterns that look like dangerous but are actually safe ──
	// "Có ai gửi mail cho tôi chưa?" is checking inbox, not sending.
	// "Liệt kê container Docker đang chạy" is listing, not running commands.
	readSafeOverrides := []string{
		"có ai gửi", "có gì không", "đang chạy",
	}
	for _, p := range readSafeOverrides {
		if strings.Contains(lower, p) {
			return TypeReadInfo
		}
	}

	// Composite patterns (check before individual categories)
	compositePatterns := []string{
		"tìm và xóa", "find and delete",
		"đọc và gửi", "read and send",
		"tìm rồi", "find then",
		"đọc rồi", "sau đó",
		"kiểm tra rồi", "rồi xóa",
		"rồi sửa", "rồi gửi",
		"rồi trả lời", "rồi restore",
		"and reply", "and delete", "and send",
		"then reply", "then delete", "then send",
		"if not", "nếu không", "if ", "nếu ",
	}
	for _, p := range compositePatterns {
		if strings.Contains(lower, p) {
			return TypeComposite
		}
	}
	// Detect composite by presence of both read AND dangerous keywords
	hasRead := containsAny(lower, "tìm", "đọc", "kiểm tra", "lấy", "check", "find", "read", "search", "backup")
	hasDangerous := containsAny(lower, "xóa", "delete", "gửi", "send", "sửa", "edit", "nén", "zip", "restore", "reply", "trả lời", "restart", "khởi động")
	hasConjunction := containsAny(lower, "và", "and", "rồi", "then", "sau đó", "after", "if", "nếu")
	if hasRead && hasDangerous && hasConjunction {
		return TypeComposite
	}

	// Dangerous action patterns — checked BEFORE greeting to avoid
	// false positives like "Write file ... Hello World" matching "hello".
	dangerousPatterns := []string{
		"xóa", "delete", "remove", "rm ",
		"gửi email", "gửi mail", "gửi tài liệu", "gửi cho",
		"send email", "send mail",
		"chạy", "run ", "exec", "execute",
		"sửa", "edit", "modify", "update",
		"tạo file", "create file", "write file", "write ",
		"tạo sự kiện", "lên lịch", "đặt lịch",
		"cài đặt", "install",
		"khởi động lại", "restart",
		"deploy", "rename", "di chuyển", "move",
	}
	for _, p := range dangerousPatterns {
		if strings.Contains(lower, p) {
			return TypeDangerousAction
		}
	}

	// Greeting patterns — checked AFTER dangerous so "Write file Hello World" isn't a greeting
	greetingPatterns := []string{
		"chào", "hello", "hey", "xin chào",
		"cảm ơn", "thank", "tạm biệt", "bye",
		"buổi sáng", "buổi chiều", "buổi tối",
		"tốt lành", "khỏe không", "how are you",
		"good morning", "good afternoon", "good evening",
		"see you", "goodbye",
	}
	for _, p := range greetingPatterns {
		if strings.Contains(lower, p) && len(lower) < 60 {
			return TypeGreeting
		}
	}
	// "hi" with word-boundary match. Also match "hi," (with punctuation).
	if containsWordFuzzy(lower, "hi") && len(lower) < 50 {
		return TypeGreeting
	}
	// Emoji greetings
	if (strings.Contains(lower, "😊") || strings.Contains(lower, "👋")) && len(lower) < 30 {
		return TypeGreeting
	}
	// LOL, haha patterns (short expressions)
	if containsAny(lower, "lol", "haha", "hehe") && len(lower) < 30 && !containsAny(lower, "file", "email", "xóa", "delete", "gửi", "send") {
		return TypeGreeting
	}

	// Read info patterns
	readPatterns := []string{
		"đọc", "read", "xem", "view", "show",
		"tìm", "find", "search", "tìm kiếm",
		"list", "danh sách", "liệt kê",
		"cho tôi xem", "cho tôi biết",
		"check", "kiểm tra",
		"mở file", "mở ", "open",
		"tra cứu", "thời tiết",
		"lịch họp",
	}
	for _, p := range readPatterns {
		if strings.Contains(lower, p) {
			return TypeReadInfo
		}
	}

	return TypeUnknown
}

// ─── Confidence Scoring ──────────────────────────────────────────────────────

func calculateConfidence(input string, t IntentType) float64 {
	lower := strings.ToLower(input)

	switch t {
	case TypeGreeting:
		return scoreGreeting(lower)
	case TypeReadInfo:
		return scoreReadInfo(lower)
	case TypeDangerousAction:
		return scoreDangerous(lower)
	case TypeComposite:
		return scoreComposite(lower)
	default:
		return 0.30
	}
}

func scoreGreeting(input string) float64 {
	keywords := []string{"chào", "hello", "hey", "xin chào", "cảm ơn", "thank", "tạm biệt", "bye", "buổi sáng", "tốt lành", "khỏe không", "how are you"}
	for _, k := range keywords {
		if strings.Contains(input, k) {
			return 0.95
		}
	}
	if containsWord(input, "hi") {
		return 0.95
	}
	// Emoji and short expressions
	if strings.Contains(input, "😊") || strings.Contains(input, "👋") {
		return 0.90
	}
	// LOL, haha patterns
	if containsAny(input, "lol", "haha", "hehe") && len(input) < 30 {
		return 0.90
	}
	if len(input) < 20 {
		return 0.70
	}
	return 0.50
}

func scoreReadInfo(input string) float64 {
	score := 0.70
	keywords := []string{"đọc", "read", "xem", "view", "tìm", "find", "search", "list", "danh sách", "cho tôi xem", "cho tôi biết", "kiểm tra", "check", "mở", "lịch", "mail", "email", "calendar", "họp", "liệt kê", "có ai gửi", "có gì không", "thời tiết", "tra cứu"}
	for _, k := range keywords {
		if strings.Contains(input, k) {
			score += 0.10
		}
	}
	// Penalise if dangerous keywords sneak in
	for _, k := range []string{"xóa", "delete", "gửi", "send", "chạy", "run"} {
		if strings.Contains(input, k) {
			score -= 0.25
		}
	}
	return clamp(score)
}

func scoreDangerous(input string) float64 {
	score := 0.75
	for _, k := range []string{"xóa", "delete", "gửi", "send", "chạy", "run", "exec", "sửa", "edit", "tạo", "create", "write", "install", "deploy", "restart", "khởi động", "rename", "di chuyển", "move"} {
		if strings.Contains(input, k) {
			score += 0.05
		}
	}
	// Strong boost if specific targets are mentioned (paths, emails, filenames)
	if strings.Contains(input, "@") {
		score += 0.15
	}
	if strings.Contains(input, "/") {
		score += 0.10
	}
	// Filenames with extensions (e.g. config.json, test.txt)
	for _, w := range strings.Fields(input) {
		if strings.Contains(w, ".") && len(w) > 3 {
			score += 0.10
			break
		}
	}
	return clamp(score)
}

func scoreComposite(input string) float64 {
	score := 0.75
	for _, k := range []string{"và", "and", "rồi", "then", "sau đó", "if", "nếu"} {
		if strings.Contains(input, k) {
			score += 0.10
		}
	}
	// Boost if contains both read and dangerous keywords
	hasRead := containsAny(input, "tìm", "đọc", "kiểm tra", "lấy", "check", "find", "backup", "read")
	hasDanger := containsAny(input, "xóa", "delete", "gửi", "send", "sửa", "nén", "zip", "restore", "reply", "trả lời", "restart", "khởi động")
	if hasRead && hasDanger {
		score += 0.15
	}
	// Strong boost for explicit conditional patterns
	if containsAny(input, "if not", "nếu không", "if ", "nếu ") {
		score += 0.10
	}
	return clamp(score)
}

// ─── Tool Call Extraction ────────────────────────────────────────────────────

func extractToolCalls(input string, t IntentType) []ToolCallInfo {
	lower := strings.ToLower(input)

	switch t {
	case TypeGreeting:
		return nil

	case TypeReadInfo:
		return extractReadToolCalls(lower, input)

	case TypeDangerousAction:
		return extractDangerousToolCalls(lower, input)

	case TypeComposite:
		var calls []ToolCallInfo
		calls = append(calls, extractReadToolCalls(lower, input)...)
		calls = append(calls, extractDangerousToolCalls(lower, input)...)
		return calls

	default:
		return nil
	}
}

func extractReadToolCalls(lower, original string) []ToolCallInfo {
	if containsAny(lower, "file", "đọc file", "mở file", "read file", "xem nội dung", "readme") {
		return []ToolCallInfo{{
			Name: "system.readFile", Category: "SAFE_READ",
			Parameters: extractPathParam(original), Timeout: 30,
		}}
	}
	if containsAny(lower, "mail", "email", "hộp thư") {
		return []ToolCallInfo{{
			Name: "gmail.listEmails", Category: "SAFE_READ",
			Parameters: map[string]interface{}{"query": original}, Timeout: 30,
		}}
	}
	if containsAny(lower, "lịch", "calendar", "họp") {
		return []ToolCallInfo{{
			Name: "calendar.listEvents", Category: "SAFE_READ",
			Parameters: map[string]interface{}{}, Timeout: 30,
		}}
	}
	if containsAny(lower, "list", "danh sách", "liệt kê", "thư mục") {
		return []ToolCallInfo{{
			Name: "system.listDirectory", Category: "SAFE_READ",
			Parameters: extractPathParam(original), Timeout: 30,
		}}
	}
	if containsAny(lower, "tìm", "find", "search", "tra cứu", "thời tiết") {
		return []ToolCallInfo{{
			Name: "web.search", Category: "SAFE_READ",
			Parameters: map[string]interface{}{"query": original}, Timeout: 45,
		}}
	}
	return nil
}

func extractDangerousToolCalls(lower, original string) []ToolCallInfo {
	if containsAny(lower, "xóa", "delete", "remove") {
		return []ToolCallInfo{{
			Name: "system.deleteFile", Category: "DANGEROUS_WRITE",
			Parameters: extractPathParam(original), Timeout: 60,
		}}
	}
	if containsAny(lower, "gửi email", "gửi mail", "send email", "gửi cho", "gửi tài liệu") {
		return []ToolCallInfo{{
			Name: "gmail.sendEmail", Category: "COMMUNICATION",
			Parameters: extractEmailParams(original), Timeout: 60,
		}}
	}
	if containsAny(lower, "chạy", "run", "exec") {
		return []ToolCallInfo{{
			Name: "sandbox.runShell", Category: "EXECUTION",
			Parameters: map[string]interface{}{"command": original}, Timeout: 120,
		}}
	}
	if containsAny(lower, "sửa", "edit", "modify") {
		return []ToolCallInfo{{
			Name: "system.writeFile", Category: "DANGEROUS_WRITE",
			Parameters: extractPathParam(original), Timeout: 60,
		}}
	}
	if containsAny(lower, "tạo sự kiện", "lên lịch", "đặt lịch") {
		return []ToolCallInfo{{
			Name: "calendar.createEvent", Category: "DANGEROUS_WRITE",
			Parameters: map[string]interface{}{}, Timeout: 60,
		}}
	}
	if containsAny(lower, "tạo file", "create file", "write file", "write ") {
		return []ToolCallInfo{{
			Name: "system.writeFile", Category: "DANGEROUS_WRITE",
			Parameters: extractPathParam(original), Timeout: 60,
		}}
	}
	return nil
}

// ─── Parameter Validation ────────────────────────────────────────────────────

func validateParams(toolCalls []ToolCallInfo) (required []string, provided map[string]interface{}, missing []string) {
	provided = make(map[string]interface{})
	for _, tc := range toolCalls {
		tool, err := LookupTool(tc.Name)
		if err != nil {
			continue
		}
		for _, p := range tool.Parameters {
			if !p.Required {
				continue
			}
			// Skip "confirm" param — this is handled by the HITL approval flow,
			// not by user input. Counting it as missing would cause the
			// classifier to always demand clarification for dangerous actions.
			if p.Name == "confirm" {
				continue
			}
			required = append(required, p.Name)
			if val, ok := tc.Parameters[p.Name]; ok && val != nil && val != "" {
				provided[p.Name] = val
			} else {
				missing = append(missing, p.Name)
			}
		}
	}
	return
}

// ─── Confirmation Logic ─────────────────────────────────────────────────────

func requiresConfirmation(t IntentType, toolCalls []ToolCallInfo) bool {
	if t == TypeDangerousAction || t == TypeComposite {
		return true
	}
	for _, tc := range toolCalls {
		if IsDangerous(tc.Name) {
			return true
		}
	}
	return false
}

func shouldClarify(cfg ConfidenceConfig, confidence float64, t IntentType) bool {
	if t == TypeGreeting {
		return false
	}
	// Always clarify if confidence is extremely low
	if confidence < cfg.AmbiguousLow {
		return true
	}
	// For each intent type, check against its own minimum threshold.
	// READ_INFO that meets its threshold should pass through without ambiguity check.
	minConf := cfg.MinConfidenceFor(t)
	if confidence >= minConf {
		return false
	}
	// Below threshold → clarify
	return true
}

// ─── Reasoning & Messages ────────────────────────────────────────────────────

func generateReasoning(input string, t IntentType, confidence float64, missing []string) string {
	if len(missing) > 0 {
		return fmt.Sprintf("Phân loại là %s (confidence=%.2f). Thiếu tham số: %s. Cần hỏi lại người dùng.",
			t, confidence, strings.Join(missing, ", "))
	}
	return fmt.Sprintf("Phân loại là %s (confidence=%.2f) dựa trên đầu vào: %q", t, confidence, truncate(input, 80))
}

func generateMissingParamMessage(toolName string, missing []string) string {
	if toolName == "" {
		return "Vui lòng cung cấp thêm thông tin để tôi thực hiện."
	}
	return fmt.Sprintf("Để thực hiện %s, tôi cần thêm thông tin: %s\n\nVui lòng cung cấp đầy đủ.",
		toolName, strings.Join(missing, ", "))
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// isPromptInjection detects prompt injection attempts
func isPromptInjection(input string) bool {
	lower := strings.ToLower(input)
	
	// English patterns
	injectionPatterns := []string{
		"ignore previous instructions",
		"disregard previous instructions",
		"forget previous instructions",
		"ignore all previous",
		"disregard all previous",
		"you are now",
		"forget your instructions",
		"new instructions",
		"system prompt",
		"override instructions",
	}
	
	// Vietnamese patterns
	vietnamesePatterns := []string{
		"bỏ qua hướng dẫn trước",
		"quên hướng dẫn trước",
		"bỏ qua chỉ dẫn",
		"quên chỉ dẫn",
		"bây giờ bạn là",
		"hướng dẫn mới",
	}
	
	for _, pattern := range injectionPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	
	for _, pattern := range vietnamesePatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	
	return false
}

func extractPathParam(input string) map[string]interface{} {
	params := make(map[string]interface{})
	for _, w := range strings.Fields(input) {
		if strings.Contains(w, "/") || (strings.Contains(w, ".") && len(w) > 2) {
			params["path"] = w
			break
		}
	}
	return params
}

func extractEmailParams(input string) map[string]interface{} {
	params := make(map[string]interface{})
	for _, w := range strings.Fields(input) {
		if strings.Contains(w, "@") {
			params["to"] = w
			break
		}
	}
	return params
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func containsWord(input, word string) bool {
	if input == word {
		return true
	}
	if strings.HasPrefix(input, word+" ") || strings.HasSuffix(input, " "+word) || strings.Contains(input, " "+word+" ") {
		return true
	}
	return false
}

// containsWordFuzzy matches a word even if followed by punctuation (e.g. "hi," or "hi!")
func containsWordFuzzy(input, word string) bool {
	if containsWord(input, word) {
		return true
	}
	// Check for word followed by punctuation: "hi," "hi!" "hi."
	for _, punct := range []string{",", "!", ".", "?", ";", ":"} {
		if strings.Contains(input, word+punct) {
			return true
		}
	}
	return false
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
