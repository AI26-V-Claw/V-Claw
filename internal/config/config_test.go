package config

import "testing"

func TestLoadRequiresTelegramConfig(t *testing.T) {
	t.Setenv("VCLAW_TELEGRAM_ENABLED", "true")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("VCLAW_TELEGRAM_BOT_TOKEN", "")
	t.Setenv("ALLOWED_TELEGRAM_USER_ID", "")
	t.Setenv("VCLAW_TELEGRAM_ALLOWED_USER_IDS", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for missing required config")
	}
}

func TestLoadParsesTelegramConfig(t *testing.T) {
	t.Setenv("VCLAW_TELEGRAM_ENABLED", "true")
	t.Setenv("TELEGRAM_BOT_TOKEN", "token")
	t.Setenv("ALLOWED_TELEGRAM_USER_ID", "123")
	t.Setenv("DATA_DIR", "")
	t.Setenv("LOG_DIR", "")
	t.Setenv("OPENAI_API_KEY", "openai-key")
	t.Setenv("OPENAI_MODEL", "gpt-test")
	t.Setenv("OPENAI_BASE_URL", "https://example.invalid/v1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.TelegramBotToken != "token" {
		t.Fatalf("unexpected token: %q", cfg.TelegramBotToken)
	}
	if cfg.AllowedTelegramUserID != 123 {
		t.Fatalf("unexpected user id: %d", cfg.AllowedTelegramUserID)
	}
	if !cfg.TelegramEnabled {
		t.Fatal("expected telegram to be enabled")
	}
	if cfg.DataDir != "./data" {
		t.Fatalf("unexpected data dir: %q", cfg.DataDir)
	}
	if cfg.LogDir != "./logs" {
		t.Fatalf("unexpected log dir: %q", cfg.LogDir)
	}
	if cfg.OpenAIAPIKey != "openai-key" {
		t.Fatalf("unexpected openai key: %q", cfg.OpenAIAPIKey)
	}
	if cfg.OpenAIModel != "gpt-test" {
		t.Fatalf("unexpected openai model: %q", cfg.OpenAIModel)
	}
	if cfg.OpenAIBaseURL != "https://example.invalid/v1" {
		t.Fatalf("unexpected openai base url: %q", cfg.OpenAIBaseURL)
	}
}

func TestLoadAcceptsVClawTelegramEnvAliases(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("ALLOWED_TELEGRAM_USER_ID", "")
	t.Setenv("VCLAW_TELEGRAM_BOT_TOKEN", "token")
	t.Setenv("VCLAW_TELEGRAM_ALLOWED_USER_IDS", "123,456")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.TelegramBotToken != "token" {
		t.Fatalf("unexpected token: %q", cfg.TelegramBotToken)
	}
	if cfg.AllowedTelegramUserID != 123 {
		t.Fatalf("unexpected user id: %d", cfg.AllowedTelegramUserID)
	}
}

func TestLoadParsesSlackConfig(t *testing.T) {
	t.Setenv("VCLAW_SLACK_ENABLED", "true")
	t.Setenv("VCLAW_SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("VCLAW_SLACK_APP_TOKEN", "xapp-test")
	t.Setenv("VCLAW_SLACK_OWNER_USER_ID", "U1")
	t.Setenv("VCLAW_SLACK_ALLOWED_CHANNEL_IDS", "C1, C2")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if !cfg.SlackEnabled {
		t.Fatal("expected slack to be enabled")
	}
	if cfg.SlackBotToken != "xoxb-test" {
		t.Fatalf("unexpected slack bot token: %q", cfg.SlackBotToken)
	}
	if cfg.SlackAppToken != "xapp-test" {
		t.Fatalf("unexpected slack app token: %q", cfg.SlackAppToken)
	}
	if cfg.SlackOwnerUserID != "U1" {
		t.Fatalf("unexpected slack owner user id: %q", cfg.SlackOwnerUserID)
	}
	if len(cfg.SlackAllowedChannelIDs) != 2 || cfg.SlackAllowedChannelIDs[0] != "C1" || cfg.SlackAllowedChannelIDs[1] != "C2" {
		t.Fatalf("unexpected slack channel ids: %#v", cfg.SlackAllowedChannelIDs)
	}
}

func TestLoadParsesLegacyLLMAliases(t *testing.T) {
	t.Setenv("VCLAW_TELEGRAM_ENABLED", "true")
	t.Setenv("TELEGRAM_BOT_TOKEN", "token")
	t.Setenv("ALLOWED_TELEGRAM_USER_ID", "123")
	t.Setenv("LLM_API_KEY", "legacy-key")
	t.Setenv("LLM_BASE_URL", "https://legacy.invalid/v1")
	t.Setenv("LLM_MODEL", "legacy-model")
	t.Setenv("VCLAW_GOOGLE_CREDENTIALS_PATH", "configs/google/credentials.json")
	t.Setenv("VCLAW_GOOGLE_TOKEN_PATH", "configs/google/token.json")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.LLMAPIKey != "legacy-key" {
		t.Fatalf("unexpected llm api key: %q", cfg.LLMAPIKey)
	}
	if cfg.LLMBaseURL != "https://legacy.invalid/v1" {
		t.Fatalf("unexpected llm base url: %q", cfg.LLMBaseURL)
	}
	if cfg.LLMModel != "legacy-model" {
		t.Fatalf("unexpected llm model: %q", cfg.LLMModel)
	}
	if cfg.GoogleCredentialsPath != "configs/google/credentials.json" {
		t.Fatalf("unexpected google credentials path: %q", cfg.GoogleCredentialsPath)
	}
	if cfg.GoogleTokenPath != "configs/google/token.json" {
		t.Fatalf("unexpected google token path: %q", cfg.GoogleTokenPath)
	}
}
