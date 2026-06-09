package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	TelegramEnabled          bool
	TelegramBotToken         string
	AllowedTelegramUserID    int64
	SlackEnabled             bool
	SlackBotToken            string
	SlackAppToken            string
	SlackOwnerUserID         string
	SlackAllowedChannelIDs   []string
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
	OpenAIAPIKey             string
	OpenAIBaseURL            string
	OpenAIModel              string
	CompactorModel           string
	GoogleCredentialsPath    string
	GoogleTokenPath          string
	GoogleToolsEnabled       bool
}

func Load() (Config, error) {
	telegramEnabled := envBool("VCLAW_TELEGRAM_ENABLED", false)
	slackEnabled := envBool("VCLAW_SLACK_ENABLED", false)

	telegramToken := firstNonEmptyEnv("TELEGRAM_BOT_TOKEN", "VCLAW_TELEGRAM_BOT_TOKEN")
	allowedUserIDRaw := firstNonEmptyEnv("ALLOWED_TELEGRAM_USER_ID", "VCLAW_TELEGRAM_ALLOWED_USER_ID", "VCLAW_TELEGRAM_ALLOWED_USER_IDS")
	allowedUserID := int64(0)
	if strings.TrimSpace(allowedUserIDRaw) != "" {
		allowedUserID, _ = strconv.ParseInt(firstCSVValue(allowedUserIDRaw), 10, 64)
	}
	if telegramEnabled {
		if telegramToken == "" {
			return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required when VCLAW_TELEGRAM_ENABLED=true")
		}
		if strings.TrimSpace(allowedUserIDRaw) == "" {
			return Config{}, fmt.Errorf("ALLOWED_TELEGRAM_USER_ID is required when VCLAW_TELEGRAM_ENABLED=true")
		}
		var err error
		allowedUserID, err = strconv.ParseInt(firstCSVValue(allowedUserIDRaw), 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("ALLOWED_TELEGRAM_USER_ID must be an integer: %w", err)
		}
	}

	slackBotToken := firstNonEmptyEnv("VCLAW_SLACK_BOT_TOKEN")
	slackAppToken := firstNonEmptyEnv("VCLAW_SLACK_APP_TOKEN")
	slackOwnerUserID := firstCSVValue(firstNonEmptyEnv("VCLAW_SLACK_OWNER_USER_ID", "VCLAW_SLACK_ALLOWED_USER_ID", "VCLAW_SLACK_ALLOWED_USER_IDS"))
	slackAllowedChannelIDs := splitCSV(firstNonEmptyEnv("VCLAW_SLACK_ALLOWED_CHANNEL_IDS"))
	if slackEnabled {
		if strings.TrimSpace(slackBotToken) == "" {
			return Config{}, fmt.Errorf("VCLAW_SLACK_BOT_TOKEN is required when VCLAW_SLACK_ENABLED=true")
		}
		if strings.TrimSpace(slackAppToken) == "" {
			return Config{}, fmt.Errorf("VCLAW_SLACK_APP_TOKEN is required when VCLAW_SLACK_ENABLED=true")
		}
		if strings.TrimSpace(slackOwnerUserID) == "" {
			return Config{}, fmt.Errorf("VCLAW_SLACK_OWNER_USER_ID is required when VCLAW_SLACK_ENABLED=true")
		}
	}

	return Config{
		TelegramEnabled:          telegramEnabled,
		TelegramBotToken:         telegramToken,
		AllowedTelegramUserID:    allowedUserID,
		SlackEnabled:             slackEnabled,
		SlackBotToken:            slackBotToken,
		SlackAppToken:            slackAppToken,
		SlackOwnerUserID:         slackOwnerUserID,
		SlackAllowedChannelIDs:   slackAllowedChannelIDs,
		DataDir:                  envOrDefault("DATA_DIR", "./data"),
		LogDir:                   envOrDefault("LOG_DIR", "./logs"),
		LLMProvider:              strings.ToLower(strings.TrimSpace(os.Getenv("LLM_PROVIDER"))),
		LLMAPIKey:                strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		LLMBaseURL:               envOrDefault("LLM_BASE_URL", ""),
		LLMModel:                 strings.TrimSpace(os.Getenv("LLM_MODEL")),
		AnthropicAPIKey:          strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")),
		AnthropicClassifierModel: strings.TrimSpace(os.Getenv("ANTHROPIC_CLASSIFIER_MODEL")),
		AnthropicResponseModel:   strings.TrimSpace(os.Getenv("ANTHROPIC_RESPONSE_MODEL")),
		UseLLMClassifier:         envBool("USE_LLM_CLASSIFIER", false),
		OpenAIAPIKey:             envFirst("OPENAI_API_KEY", "LLM_API_KEY"),
		OpenAIBaseURL:            envFirst("OPENAI_BASE_URL", "LLM_BASE_URL"),
		OpenAIModel:              envFirst("OPENAI_MODEL", "LLM_MODEL"),
		CompactorModel:           strings.TrimSpace(os.Getenv("VCLAW_COMPACTOR_MODEL")),
		GoogleCredentialsPath:    envOrDefault("VCLAW_GOOGLE_CREDENTIALS_PATH", "configs/google/credentials.json"),
		GoogleTokenPath:          envOrDefault("VCLAW_GOOGLE_TOKEN_PATH", "configs/google/token.json"),
		GoogleToolsEnabled:       envBool("VCLAW_AGENT_GOOGLE_TOOLS_ENABLED", false),
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
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyEnv(keys ...string) string {
	return envFirst(keys...)
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
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func splitCSV(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if entry := strings.TrimSpace(part); entry != "" {
			result = append(result, entry)
		}
	}
	return result
}

func firstCSVValue(value string) string {
	parts := splitCSV(value)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
