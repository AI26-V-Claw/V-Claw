package intent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"vclaw/internal/providers"
)

// LLMClassifier uses an LLM provider to classify intents.
// This is the production implementation that replaces the heuristic classifier.
type LLMClassifier struct {
	provider providers.Provider
	config   ConfidenceConfig
	prompt   string // System prompt from SOUL.md
}

// NewLLMClassifier creates a new LLM-based intent classifier.
func NewLLMClassifier(provider providers.Provider, cfg ConfidenceConfig) (*LLMClassifier, error) {
	// Load system prompt from SOUL.md
	prompt, err := loadSystemPrompt()
	if err != nil {
		return nil, fmt.Errorf("failed to load system prompt: %w", err)
	}

	return &LLMClassifier{
		provider: provider,
		config:   cfg,
		prompt:   prompt,
	}, nil
}

// Classify uses the LLM to classify user intent.
func (c *LLMClassifier) Classify(ctx context.Context, userInput string) (*ClassificationOutput, error) {
	// Build the prompt
	userPrompt := fmt.Sprintf(`Phân loại ý định của người dùng sau đây:

"%s"

Trả về JSON theo đúng định dạng đã chỉ định.`, userInput)

	// Call LLM
	req := &providers.GenerateRequest{
		SystemPrompt:   c.prompt,
		UserPrompt:     userPrompt,
		Temperature:    0.3, // Low temperature for consistent classification
		MaxTokens:      2048,
		ResponseFormat: "json",
	}

	resp, err := c.provider.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("llm generation failed: %w", err)
	}

	// Parse JSON response
	var result Result
	if err := json.Unmarshal([]byte(resp.Text), &result); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w\nResponse: %s", err, resp.Text)
	}

	// Validate the result
	return Validate(&result, c.config), nil
}

// loadSystemPrompt loads the system prompt from configs/SOUL.md
func loadSystemPrompt() (string, error) {
	data, err := os.ReadFile("configs/SOUL.md")
	if err != nil {
		return "", fmt.Errorf("failed to read SOUL.md: %w", err)
	}

	// Extract the relevant sections for intent classification
	content := string(data)

	// Find the section starting from "LUẬT SINH TỒN"
	startIdx := strings.Index(content, "## 🔴 LUẬT SINH TỒN")
	if startIdx == -1 {
		return "", fmt.Errorf("SOUL.md missing required section: LUẬT SINH TỒN")
	}

	// Return everything from that point onwards
	return content[startIdx:], nil
}

// ClassifyWithMemoryIsolation classifies intent with memory isolation for dangerous actions.
// This implements the stateless memory requirement from the spec.
func (c *LLMClassifier) ClassifyWithMemoryIsolation(ctx context.Context, userInput string, recentHistory []string) (*ClassificationOutput, error) {
	// Build isolated prompt with memory warning
	isolationWarning := `⚠️ CẢNH BÁO CÁCH LY BỘ NHỚ (MEMORY ISOLATION WARNING) ⚠️

Bạn đang xử lý một yêu cầu có thể nguy hiểm.
Quy tắc bắt buộc:
1. CHỈ sử dụng các tham số được cung cấp TRỰC TIẾP trong câu thoại CUỐI CÙNG của người dùng.
2. KHÔNG được tự ý sao chép tham số từ hội thoại cũ hơn trừ khi người dùng chỉ thị rõ ràng.
3. Nếu thiếu tham số bắt buộc → trả về missing_params và needs_confirm = true.
4. Khi không chắc chắn → Hỏi lại, ĐỪNG đoán mò.

Lịch sử hội thoại gần đây (CHỈ để tham khảo ngữ cảnh, KHÔNG dùng làm tham số):
%s

Yêu cầu hiện tại:
"%s"

Trả về JSON theo đúng định dạng đã chỉ định.`

	historyStr := strings.Join(recentHistory, "\n")
	userPrompt := fmt.Sprintf(isolationWarning, historyStr, userInput)

	// Call LLM
	req := &providers.GenerateRequest{
		SystemPrompt:   c.prompt,
		UserPrompt:     userPrompt,
		Temperature:    0.3,
		MaxTokens:      2048,
		ResponseFormat: "json",
	}

	resp, err := c.provider.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("llm generation failed: %w", err)
	}

	// Parse JSON response
	var result Result
	if err := json.Unmarshal([]byte(resp.Text), &result); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w\nResponse: %s", err, resp.Text)
	}

	// Validate the result
	return Validate(&result, c.config), nil
}
