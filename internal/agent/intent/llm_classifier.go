package intent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	// Do not hard-code a single working-directory dependent path.
	// Allow injecting prompt path via env, otherwise try common locations.
	candidates := []string{}
	if fromEnv := strings.TrimSpace(os.Getenv("VCLAW_SOUL_MD_PATH")); fromEnv != "" {
		candidates = append(candidates, fromEnv)
	}
	candidates = append(candidates, filepath.Join("configs", "SOUL.md"))
	if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "configs", "SOUL.md"))
	}

	var lastErr error
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			lastErr = err
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			lastErr = fmt.Errorf("SOUL.md at %q is empty", path)
			continue
		}
		// Prefer returning from "LUẬT SINH TỒN" section if present, but do not depend on emoji.
		if idx := strings.Index(content, "LUẬT SINH TỒN"); idx >= 0 {
			return strings.TrimSpace(content[idx:]), nil
		}
		return content, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("SOUL.md not found in candidate paths")
	}
	return "", fmt.Errorf("failed to load SOUL.md: %w", lastErr)
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
