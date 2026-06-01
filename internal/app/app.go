package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/nxhai/vclaw/internal/agent"
	"github.com/nxhai/vclaw/internal/audit"
	"github.com/nxhai/vclaw/internal/channels/telegram"
	"github.com/nxhai/vclaw/internal/config"
	"github.com/nxhai/vclaw/internal/intent"
	"github.com/nxhai/vclaw/internal/memory"
	"github.com/nxhai/vclaw/internal/providers"
)

type App struct {
	logger *slog.Logger
	bot    *telegram.Bot
}

func New() (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	memoryStore := memory.NewStore()
	intentClassifier := intent.NewClassifier()
	llmClient, err := providers.NewClient(providers.Config{
		Provider: chooseLLMProvider(cfg),
		APIKey:   chooseLLMAPIKey(cfg),
		BaseURL:  chooseLLMBaseURL(cfg),
		Model:    chooseLLMModel(cfg),
	})
	if err != nil {
		return nil, err
	}
	auditLogger := audit.NewLogger(filepath.Join(cfg.LogDir, "audit.jsonl"))
	orchestrator := agent.NewOrchestrator(memoryStore, intentClassifier, llmClient, auditLogger)
	bot := telegram.New(cfg.TelegramBotToken, cfg.AllowedTelegramUserID, cfg.DataDir, orchestrator, logger)

	return &App{logger: logger, bot: bot}, nil
}

func (a *App) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a.logger.Info("starting vclaw telegram bot")
	if err := a.bot.Run(ctx); err != nil && err != context.Canceled {
		return fmt.Errorf("telegram bot stopped: %w", err)
	}
	return nil
}

func chooseLLMProvider(cfg config.Config) string {
	if cfg.LLMProvider != "" {
		return cfg.LLMProvider
	}
	if cfg.LLMAPIKey != "" && cfg.LLMModel != "" {
		return "openai-compatible"
	}
	if cfg.AnthropicAPIKey != "" && cfg.AnthropicResponseModel != "" {
		return "anthropic"
	}
	return ""
}

func chooseLLMAPIKey(cfg config.Config) string {
	if cfg.LLMAPIKey != "" {
		return cfg.LLMAPIKey
	}
	return cfg.AnthropicAPIKey
}

func chooseLLMBaseURL(cfg config.Config) string {
	if cfg.LLMBaseURL != "" {
		return cfg.LLMBaseURL
	}
	if cfg.LLMProvider == "anthropic" || (cfg.LLMProvider == "" && cfg.AnthropicAPIKey != "" && cfg.AnthropicResponseModel != "") {
		return "https://api.anthropic.com"
	}
	return "https://api.openai.com/v1"
}

func chooseLLMModel(cfg config.Config) string {
	if cfg.LLMModel != "" {
		return cfg.LLMModel
	}
	return cfg.AnthropicResponseModel
}
