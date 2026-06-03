package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	TelegramBotToken      string
	AllowedTelegramUserID int64
	DataDir               string
	LogDir                string
	OpenAIAPIKey          string
	OpenAIBaseURL         string
	OpenAIModel           string
	GoogleCredentialsPath string
	GoogleTokenPath       string
	GoogleToolsEnabled    bool
}

func Load() (Config, error) {
	token := envFirst("TELEGRAM_BOT_TOKEN", "VCLAW_TELEGRAM_BOT_TOKEN")
	if token == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN or VCLAW_TELEGRAM_BOT_TOKEN is required")
	}

	allowedUserIDRaw := envFirst("ALLOWED_TELEGRAM_USER_ID", "VCLAW_TELEGRAM_ALLOWED_USER_IDS")
	if allowedUserIDRaw == "" {
		return Config{}, fmt.Errorf("ALLOWED_TELEGRAM_USER_ID or VCLAW_TELEGRAM_ALLOWED_USER_IDS is required")
	}
	allowedUserIDRaw = firstCSV(allowedUserIDRaw)

	allowedUserID, err := strconv.ParseInt(allowedUserIDRaw, 10, 64)
	if err != nil {
		return Config{}, fmt.Errorf("allowed telegram user id must be an integer: %w", err)
	}

	return Config{
		TelegramBotToken:      token,
		AllowedTelegramUserID: allowedUserID,
		DataDir:               envOrDefault("DATA_DIR", "./data"),
		LogDir:                envOrDefault("LOG_DIR", "./logs"),
		OpenAIAPIKey:          envFirst("OPENAI_API_KEY", "LLM_API_KEY"),
		OpenAIBaseURL:         envFirst("OPENAI_BASE_URL", "LLM_BASE_URL"),
		OpenAIModel:           envFirst("OPENAI_MODEL", "LLM_MODEL"),
		GoogleCredentialsPath: envOrDefault("VCLAW_GOOGLE_CREDENTIALS_PATH", "configs/google/credentials.json"),
		GoogleTokenPath:       envOrDefault("VCLAW_GOOGLE_TOKEN_PATH", "configs/google/token.json"),
		GoogleToolsEnabled:    envBool("VCLAW_AGENT_GOOGLE_TOOLS_ENABLED", false),
	}, nil
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envFirst(keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value
		}
	}
	return ""
}

func firstCSV(value string) string {
	first, _, _ := strings.Cut(value, ",")
	return strings.TrimSpace(first)
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes"
}
