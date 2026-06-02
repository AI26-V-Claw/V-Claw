package config

import "testing"

func TestLoadRequiresTelegramConfig(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("ALLOWED_TELEGRAM_USER_ID", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for missing required config")
	}
}

func TestLoadParsesTelegramConfig(t *testing.T) {
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
	if cfg.DataDir != "./data" {
		t.Fatalf("unexpected data dir: %q", cfg.DataDir)
	}
	if cfg.LogDir != "./logs" {
		t.Fatalf("unexpected log dir: %q", cfg.LogDir)
	}
}
