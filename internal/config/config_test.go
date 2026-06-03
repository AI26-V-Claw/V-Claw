package config

import "testing"

func TestLoadRequiresTelegramConfig(t *testing.T) {
	t.Setenv("VCLAW_TELEGRAM_ENABLED", "true")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("ALLOWED_TELEGRAM_USER_ID", "")

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
}

func TestLoadParsesSlackConfig(t *testing.T) {
	t.Setenv("VCLAW_SLACK_ENABLED", "true")
	t.Setenv("VCLAW_SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("VCLAW_SLACK_APP_TOKEN", "xapp-test")
	t.Setenv("VCLAW_SLACK_ALLOWED_CHANNEL_IDS", "C1, C2")
	t.Setenv("VCLAW_SLACK_ALLOWED_USER_IDS", "U1,U2")

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
	if len(cfg.SlackAllowedChannelIDs) != 2 || cfg.SlackAllowedChannelIDs[0] != "C1" || cfg.SlackAllowedChannelIDs[1] != "C2" {
		t.Fatalf("unexpected slack channel ids: %#v", cfg.SlackAllowedChannelIDs)
	}
	if len(cfg.SlackAllowedUserIDs) != 2 || cfg.SlackAllowedUserIDs[0] != "U1" || cfg.SlackAllowedUserIDs[1] != "U2" {
		t.Fatalf("unexpected slack user ids: %#v", cfg.SlackAllowedUserIDs)
	}
}
