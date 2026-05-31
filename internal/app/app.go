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
	auditLogger := audit.NewLogger(filepath.Join(cfg.LogDir, "audit.jsonl"))
	orchestrator := agent.NewOrchestrator(memoryStore, intentClassifier, auditLogger)
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
