package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	TelegramBotToken         string
	AllowedTelegramUserID    int64
	DataDir                  string
	LogDir                   string
	LLMProvider              string
	LLMAPIKey                string
	LLMBaseURL               string
	LLMModel                 string
	AnthropicAPIKey          string
	AnthropicClassifierModel string
	AnthropicResponseModel   string
	UseLLMClassifier         bool
}

func Load() (Config, error) {
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}

	allowedUserIDRaw := strings.TrimSpace(os.Getenv("ALLOWED_TELEGRAM_USER_ID"))
	if allowedUserIDRaw == "" {
		return Config{}, fmt.Errorf("ALLOWED_TELEGRAM_USER_ID is required")
	}

	allowedUserID, err := strconv.ParseInt(allowedUserIDRaw, 10, 64)
	if err != nil {
		return Config{}, fmt.Errorf("ALLOWED_TELEGRAM_USER_ID must be an integer: %w", err)
	}

	return Config{
		TelegramBotToken:         token,
		AllowedTelegramUserID:    allowedUserID,
		DataDir:                  envOrDefault("DATA_DIR", "./data"),
		LogDir:                   envOrDefault("LOG_DIR", "./logs"),
		LLMProvider:              strings.ToLower(strings.TrimSpace(os.Getenv("LLM_PROVIDER"))),
		LLMAPIKey:                strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		LLMBaseURL:               envOrDefault("LLM_BASE_URL", ""),
		LLMModel:                 strings.TrimSpace(os.Getenv("LLM_MODEL")),
		AnthropicAPIKey:          strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")),
		AnthropicClassifierModel: strings.TrimSpace(os.Getenv("ANTHROPIC_CLASSIFIER_MODEL")),
		AnthropicResponseModel:   strings.TrimSpace(os.Getenv("ANTHROPIC_RESPONSE_MODEL")),
		UseLLMClassifier:         envOrDefault("USE_LLM_CLASSIFIER", "false") == "true",
	}, nil
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
