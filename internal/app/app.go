package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"vclaw/internal/agent"
	"vclaw/internal/channels/telegram"
	"vclaw/internal/config"
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

	runtime, err := NewAgentRuntime(context.Background(), AgentRuntimeConfig{
		OpenAIAPIKey:          cfg.OpenAIAPIKey,
		OpenAIModel:           cfg.OpenAIModel,
		OpenAIBaseURL:         cfg.OpenAIBaseURL,
		Logger:                logger,
		EnableGoogleTools:     cfg.GoogleToolsEnabled,
		GoogleCredentialsPath: cfg.GoogleCredentialsPath,
		GoogleTokenPath:       cfg.GoogleTokenPath,
	})
	if err != nil {
		return nil, err
	}
	bot := telegram.New(cfg.TelegramBotToken, cfg.AllowedTelegramUserID, cfg.DataDir, agent.NewRuntimeMessenger(runtime), logger)

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
